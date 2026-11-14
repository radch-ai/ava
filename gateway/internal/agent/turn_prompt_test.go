package agent_test

// Turn persona pins: the lane fetches the agent's config each turn
// and leads the model call with the built system prompt, and a failed
// fetch fails open — the turn still runs, persona-less.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
)

// openAIRequest is the request body shape the persona test reads.
type openAIRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// recordingLLM streams one answer as a single-delta SSE turn and
// records the request body the model call carried.
type recordingLLM struct {
	*httptest.Server
	mu   sync.Mutex
	body []byte
}

func (s *recordingLLM) lastRequest() openAIRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var req openAIRequest
	_ = json.Unmarshal(s.body, &req)
	return req
}

// newRecordingLLM builds the fake the persona tests assert against.
func newRecordingLLM(t *testing.T, reply string) *recordingLLM {
	t.Helper()
	s := &recordingLLM{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.body = b
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk(reply)))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(s.Close)
	return s
}

// fixedConfig serves one config to every lane.
func fixedConfig(cfg *apitypes.AgentConfig) agent.AgentConfigSource {
	return func(ctx context.Context, agentID string) (*apitypes.AgentConfig, error) {
		return cfg, nil
	}
}

func TestTurnLeadsModelCallWithPersona(t *testing.T) {
	cfg := &apitypes.AgentConfig{
		AgentEditableFields: apitypes.AgentEditableFields{
			Soul:                  &apitypes.SoulConfig{Personality: "You are warm.", Identity: "A research companion."},
			OperatingInstructions: textPtr("house rules"),
			UserContext:           textPtr("Lives in Lyon."),
			ToolsContext:          textPtr("Prefer search."),
		},
	}
	llm := newRecordingLLM(t, "hello from the model")

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel:   fixedResolver(llm.URL),
			GetAgentConfig: fixedConfig(cfg),
		})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-persona", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	_, p := readUntilMessage(t, c)
	require.Equal(t, "hello from the model", p.Content)

	req := llm.lastRequest()
	require.NotEmpty(t, req.Messages)
	require.Equal(t, "system", req.Messages[0].Role)
	require.Equal(t, agent.BuildSystemPrompt(cfg), req.Messages[0].Content)
	require.GreaterOrEqual(t, len(req.Messages), 2)
	require.Equal(t, "user", req.Messages[1].Role)
}

func TestTurnConfigFetchFailureStillAnswers(t *testing.T) {
	llm := newRecordingLLM(t, "still answering")

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			GetAgentConfig: func(ctx context.Context, agentID string) (*apitypes.AgentConfig, error) {
				return nil, errors.New("db down")
			},
		})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-persona-less", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	_, p := readUntilMessage(t, c)
	require.Equal(t, "still answering", p.Content)

	// Persona-less: no system message leads the turn.
	for _, m := range llm.lastRequest().Messages {
		require.NotEqual(t, "system", m.Role)
	}
}

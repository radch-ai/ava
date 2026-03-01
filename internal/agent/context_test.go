package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty string", "", 0},
		{"single char", "a", 1},
		{"four chars", "abcd", 1},
		{"five chars", "abcde", 2},
		{"eight chars", "abcdefgh", 2},
		{"long string", "This is a test message with many characters", 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateTokens(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContextAssembler_AssembleContext_EmptyHistory(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", []string{"Hello"})

	assert.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, RoleUser, messages[0].Role)
}

func TestContextAssembler_AssembleContext_ExistingHistory(t *testing.T) {
	store := NewMockHistoryStore()
	store.SetHistory("session-1", []AgentMessage{
		NewTextMessage(RoleUser, "Previous message"),
		NewTextMessage(RoleAssistant, "Previous response"),
	})

	assembler := NewContextAssembler(store, 4000)

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", []string{"New message"})

	assert.NoError(t, err)
	assert.Len(t, messages, 3)
	assert.Equal(t, RoleUser, messages[0].Role)
	assert.Equal(t, RoleAssistant, messages[1].Role)
	assert.Equal(t, RoleUser, messages[2].Role)
}

func TestContextAssembler_AssembleContext_NoCompactionAtThreshold(t *testing.T) {
	store := NewMockHistoryStore()

	contextLimit := 1000
	threshold := int(float64(contextLimit) * compactionThreshold)

	var history []AgentMessage
	textLen := 0
	for textLen < threshold-10 {
		msg := NewTextMessage(RoleUser, "short")
		history = append(history, msg)
		textLen += EstimateTokens("short")
	}
	store.SetHistory("session-1", history)

	assembler := NewContextAssembler(store, contextLimit)

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", []string{})

	assert.NoError(t, err)
	assert.Len(t, messages, len(history), "no compaction should occur at or below threshold")
}

func TestContextAssembler_AssembleContext_CompactionOverThreshold(t *testing.T) {
	store := NewMockHistoryStore()

	contextLimit := 1000

	var history []AgentMessage
	for i := 0; i < 100; i++ {
		history = append(history,
			NewTextMessage(RoleUser, "This is a longer test message for testing compaction"),
			NewTextMessage(RoleAssistant, "This is a longer response for testing compaction"),
		)
	}
	store.SetHistory("session-1", history)

	assembler := NewContextAssembler(store, contextLimit)

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", []string{})

	assert.NoError(t, err)
	assert.Less(t, len(messages), len(history), "compaction should reduce message count")
	assert.Contains(t, messages[0].Content[0].(TextContent).Text, "[Previous conversation summary]")
}

func TestContextAssembler_AssembleContext_VeryLargeHistory(t *testing.T) {
	store := NewMockHistoryStore()

	contextLimit := 2000

	var history []AgentMessage
	for i := 0; i < 500; i++ {
		history = append(history,
			NewTextMessage(RoleUser, "User message number in this long conversation history test"),
			NewTextMessage(RoleAssistant, "Assistant response number in this long conversation history test"),
		)
	}
	store.SetHistory("session-1", history)

	assembler := NewContextAssembler(store, contextLimit)

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", []string{})

	assert.NoError(t, err)
	assert.Less(t, len(messages), 100, "compaction should significantly reduce message count")
}

func TestContextAssembler_CompactHistory_NoCompactionNeeded(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	history := make([]AgentMessage, 10)
	for i := range history {
		history[i] = NewTextMessage(RoleUser, "message")
	}

	result := assembler.CompactHistory(history)

	assert.Len(t, result, 10)
	assert.Equal(t, history, result)
}

func TestContextAssembler_CompactHistory_ExactlyAtThreshold(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	history := make([]AgentMessage, recentTurnsToKeep)
	for i := range history {
		history[i] = NewTextMessage(RoleUser, "message")
	}

	result := assembler.CompactHistory(history)

	assert.Len(t, result, recentTurnsToKeep)
}

func TestContextAssembler_CompactHistory_JustOverThreshold(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	history := make([]AgentMessage, recentTurnsToKeep+5)
	for i := range history {
		history[i] = NewTextMessage(RoleUser, "message")
	}

	result := assembler.CompactHistory(history)

	assert.Equal(t, 1+recentTurnsToKeep, len(result), "should have 1 summary + recentTurnsToKeep messages")
}

func TestContextAssembler_CompactHistory_SummaryContent(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	var history []AgentMessage
	for i := 0; i < 30; i++ {
		history = append(history, NewTextMessage(RoleUser, "user message"))
		history = append(history, NewTextMessage(RoleAssistant, "assistant response"))
	}

	result := assembler.CompactHistory(history)

	summary := result[0].Content[0].(TextContent).Text
	assert.Contains(t, summary, "[Previous conversation summary]")
	assert.Contains(t, summary, "user messages")
	assert.Contains(t, summary, "assistant responses")
}

func TestContextAssembler_SummarizeMessages(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	messages := []AgentMessage{
		NewTextMessage(RoleUser, "First user message"),
		NewTextMessage(RoleAssistant, "First response"),
		NewTextMessage(RoleUser, "Second user message"),
		NewTextMessage(RoleAssistant, "Second response"),
		NewTextMessage(RoleUser, "Last user message with a longer text to check truncation"),
	}

	summary := assembler.summarizeMessages(messages)

	assert.Equal(t, RoleAssistant, summary.Role)
	text := summary.Content[0].(TextContent).Text
	assert.Contains(t, text, "3 user messages")
	assert.Contains(t, text, "2 assistant responses")
	assert.Contains(t, text, "Last user topic")
}

func TestContextAssembler_SummarizeMessages_AllAssistantMessages(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	messages := []AgentMessage{
		NewTextMessage(RoleAssistant, "Response 1"),
		NewTextMessage(RoleAssistant, "Response 2"),
	}

	summary := assembler.summarizeMessages(messages)

	text := summary.Content[0].(TextContent).Text
	assert.Contains(t, text, "0 user messages")
	assert.Contains(t, text, "2 assistant responses")
}

func TestContextAssembler_SummarizeMessages_AllUserMessages(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	messages := []AgentMessage{
		NewTextMessage(RoleUser, "User 1"),
		NewTextMessage(RoleUser, "User 2"),
		NewTextMessage(RoleUser, "User 3"),
	}

	summary := assembler.summarizeMessages(messages)

	text := summary.Content[0].(TextContent).Text
	assert.Contains(t, text, "3 user messages")
	assert.Contains(t, text, "0 assistant responses")
}

func TestContextAssembler_SummarizeMessages_EmptyMessages(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	messages := []AgentMessage{}

	summary := assembler.summarizeMessages(messages)

	text := summary.Content[0].(TextContent).Text
	assert.Contains(t, text, "0 user messages")
	assert.Contains(t, text, "0 assistant responses")
}

func TestContextAssembler_SummarizeMessages_LongLastUserMessage(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	longText := ""
	for i := 0; i < 200; i++ {
		longText += "x"
	}

	messages := []AgentMessage{
		NewTextMessage(RoleUser, longText),
	}

	summary := assembler.summarizeMessages(messages)

	text := summary.Content[0].(TextContent).Text
	assert.Contains(t, text, "Last user topic:")
	assert.Contains(t, text, "...")
}

func TestContextAssembler_AssembleContext_StoreError(t *testing.T) {
	store := &ErrorHistoryStore{loadErr: assert.AnError}
	assembler := NewContextAssembler(store, 4000)

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", []string{"Hello"})

	assert.Nil(t, messages)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load history")
}

func TestContextAssembler_AssembleContext_SaveError(t *testing.T) {
	store := &ErrorHistoryStore{
		history: []AgentMessage{},
		saveErr: assert.AnError,
	}
	assembler := NewContextAssembler(store, 100)

	var newMessages []string
	for i := 0; i < 50; i++ {
		newMessages = append(newMessages, "This is a test message to trigger compaction")
	}

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", newMessages)

	assert.Nil(t, messages)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save compacted history")
}

type ErrorHistoryStore struct {
	history []AgentMessage
	loadErr error
	saveErr error
}

func (e *ErrorHistoryStore) LoadHistory(ctx context.Context, sessionID string) ([]AgentMessage, error) {
	if e.loadErr != nil {
		return nil, e.loadErr
	}
	return e.history, nil
}

func (e *ErrorHistoryStore) SaveHistory(ctx context.Context, sessionID string, history []AgentMessage) error {
	return e.saveErr
}

func TestContextAssembler_EstimateTokensForMessages(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	messages := []AgentMessage{
		NewTextMessage(RoleUser, "Hello"),      // 2 tokens
		NewTextMessage(RoleAssistant, "World"), // 2 tokens
	}

	tokens := assembler.estimateTokensForMessages(messages)

	assert.Equal(t, 4, tokens)
}

func TestContextAssembler_EstimateTokensForMessages_MixedContent(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	messages := []AgentMessage{
		{
			Role: RoleUser,
			Content: []ContentBlock{
				TextContent{Text: "Hello"},
				ImageContent{Data: "base64imagedata"},
			},
		},
	}

	tokens := assembler.estimateTokensForMessages(messages)

	assert.Equal(t, 2, tokens)
}

func TestContextAssembler_AssembleContext_MultipleNewMessages(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", []string{"Msg1", "Msg2", "Msg3"})

	assert.NoError(t, err)
	assert.Len(t, messages, 3)
	assert.Equal(t, RoleUser, messages[0].Role)
	assert.Equal(t, RoleUser, messages[1].Role)
	assert.Equal(t, RoleUser, messages[2].Role)
}

func TestContextAssembler_AssembleContext_NoNewMessages(t *testing.T) {
	store := NewMockHistoryStore()
	store.SetHistory("session-1", []AgentMessage{
		NewTextMessage(RoleUser, "Existing"),
	})

	assembler := NewContextAssembler(store, 4000)

	ctx := context.Background()
	messages, err := assembler.AssembleContext(ctx, "session-1", []string{})

	assert.NoError(t, err)
	assert.Len(t, messages, 1)
}

func TestContextAssembler_CompactHistory_PreservesRecentMessages(t *testing.T) {
	store := NewMockHistoryStore()
	assembler := NewContextAssembler(store, 4000)

	var history []AgentMessage
	for i := 0; i < 50; i++ {
		history = append(history, NewTextMessage(RoleUser, "message"))
	}

	result := assembler.CompactHistory(history)

	assert.Equal(t, 1+recentTurnsToKeep, len(result))

	for i := 1; i < len(result); i++ {
		assert.Equal(t, history[len(history)-recentTurnsToKeep+i-1], result[i])
	}
}

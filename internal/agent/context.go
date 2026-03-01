package agent

import (
	"context"
	"fmt"
)

const (
	compactionThreshold = 0.7
	recentTurnsToKeep   = 20
)

type HistoryStore interface {
	LoadHistory(ctx context.Context, sessionID string) ([]AgentMessage, error)
	SaveHistory(ctx context.Context, sessionID string, history []AgentMessage) error
}

type ContextAssembler struct {
	store        HistoryStore
	contextLimit int
}

func NewContextAssembler(store HistoryStore, contextLimit int) *ContextAssembler {
	return &ContextAssembler{
		store:        store,
		contextLimit: contextLimit,
	}
}

func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return (len(text) + 3) / 4
}

func (ca *ContextAssembler) AssembleContext(ctx context.Context, sessionID string, newMessages []string) ([]AgentMessage, error) {
	history, err := ca.store.LoadHistory(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load history: %w", err)
	}

	for _, msg := range newMessages {
		history = append(history, NewTextMessage(RoleUser, msg))
	}

	totalTokens := ca.estimateTokensForMessages(history)
	if totalTokens > int(float64(ca.contextLimit)*compactionThreshold) {
		history = ca.CompactHistory(history)
		if err := ca.store.SaveHistory(ctx, sessionID, history); err != nil {
			return nil, fmt.Errorf("failed to save compacted history: %w", err)
		}
	}

	return history, nil
}

func (ca *ContextAssembler) CompactHistory(history []AgentMessage) []AgentMessage {
	if len(history) <= recentTurnsToKeep {
		return history
	}

	splitPoint := len(history) - recentTurnsToKeep
	if splitPoint < 0 {
		splitPoint = 0
	}

	oldMessages := history[:splitPoint]
	recentMessages := history[splitPoint:]

	summary := ca.summarizeMessages(oldMessages)

	result := []AgentMessage{summary}
	result = append(result, recentMessages...)

	return result
}

func (ca *ContextAssembler) estimateTokensForMessages(messages []AgentMessage) int {
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			if tc, ok := block.(TextContent); ok {
				total += EstimateTokens(tc.Text)
			}
		}
	}
	return total
}

func (ca *ContextAssembler) summarizeMessages(messages []AgentMessage) AgentMessage {
	summary := "[Previous conversation summary]\n"

	userCount := 0
	assistantCount := 0
	for _, msg := range messages {
		switch msg.Role {
		case RoleUser:
			userCount++
		case RoleAssistant:
			assistantCount++
		}
	}

	summary += fmt.Sprintf("The conversation had %d user messages and %d assistant responses.", userCount, assistantCount)

	if len(messages) > 0 {
		lastUserIdx := -1
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == RoleUser {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx >= 0 {
			for _, block := range messages[lastUserIdx].Content {
				if tc, ok := block.(TextContent); ok {
					if len(tc.Text) > 100 {
						summary += fmt.Sprintf(" Last user topic: %s...", tc.Text[:100])
					} else {
						summary += fmt.Sprintf(" Last user topic: %s", tc.Text)
					}
					break
				}
			}
		}
	}

	return AgentMessage{
		Role:    RoleAssistant,
		Content: []ContentBlock{TextContent{Text: summary}},
	}
}

type MockHistoryStore struct {
	history map[string][]AgentMessage
}

func NewMockHistoryStore() *MockHistoryStore {
	return &MockHistoryStore{
		history: make(map[string][]AgentMessage),
	}
}

func (m *MockHistoryStore) LoadHistory(ctx context.Context, sessionID string) ([]AgentMessage, error) {
	if h, ok := m.history[sessionID]; ok {
		return h, nil
	}
	return []AgentMessage{}, nil
}

func (m *MockHistoryStore) SaveHistory(ctx context.Context, sessionID string, history []AgentMessage) error {
	m.history[sessionID] = history
	return nil
}

func (m *MockHistoryStore) SetHistory(sessionID string, history []AgentMessage) {
	m.history[sessionID] = history
}

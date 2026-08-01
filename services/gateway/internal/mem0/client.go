package mem0

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// Client is the interface for Mem0 API operations.
type Client interface {
	Add(ctx context.Context, userID, content, runID string) error
	Search(ctx context.Context, userID, query string) ([]string, error)
	SearchInSession(ctx context.Context, userID, runID, query string) ([]string, error)
	CreateSession(ctx context.Context, userID string) (string, error)
	DeleteSession(ctx context.Context, userID, runID string) error
}

// Memory represents a stored memory.
type Memory struct {
	ID      string
	UserID  string
	RunID   string
	Content string
}

// MockClient is an in-memory mock Mem0 client for testing.
type MockClient struct {
	mu       sync.RWMutex
	memories map[string]*Memory
	byUser   map[string][]*Memory
	byRun    map[string][]*Memory
}

// NewMockClient creates a new mock Mem0 client.
func NewMockClient() *MockClient {
	return &MockClient{
		memories: make(map[string]*Memory),
		byUser:   make(map[string][]*Memory),
		byRun:    make(map[string][]*Memory),
	}
}

func (m *MockClient) Add(ctx context.Context, userID, content, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	memory := &Memory{
		ID:      uuid.New().String(),
		UserID:  userID,
		RunID:   runID,
		Content: content,
	}

	m.memories[memory.ID] = memory
	m.byUser[userID] = append(m.byUser[userID], memory)

	if runID != "" {
		m.byRun[runID] = append(m.byRun[runID], memory)
	}

	return nil
}

func (m *MockClient) Search(ctx context.Context, userID, query string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userMemories, exists := m.byUser[userID]
	if !exists {
		return []string{}, nil
	}

	var results []string
	queryLower := strings.ToLower(query)

	for _, mem := range userMemories {
		if mem.RunID != "" {
			continue
		}
		if strings.Contains(strings.ToLower(mem.Content), queryLower) {
			results = append(results, mem.Content)
		}
	}

	return results, nil
}

func (m *MockClient) SearchInSession(ctx context.Context, userID, runID, query string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runMemories, exists := m.byRun[runID]
	if !exists {
		return []string{}, nil
	}

	var results []string
	queryLower := strings.ToLower(query)

	for _, mem := range runMemories {
		if mem.UserID != userID {
			continue
		}
		if strings.Contains(strings.ToLower(mem.Content), queryLower) {
			results = append(results, mem.Content)
		}
	}

	return results, nil
}

func (m *MockClient) CreateSession(ctx context.Context, userID string) (string, error) {
	return uuid.New().String(), nil
}

func (m *MockClient) DeleteSession(ctx context.Context, userID, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	runMemories, exists := m.byRun[runID]
	if !exists {
		return nil
	}

	for _, mem := range runMemories {
		delete(m.memories, mem.ID)
	}

	delete(m.byRun, runID)

	var updated []*Memory
	for _, mem := range m.byUser[userID] {
		if mem.RunID != runID {
			updated = append(updated, mem)
		}
	}
	m.byUser[userID] = updated

	return nil
}

// APIClient implements Client using the real Mem0 API.
// This is a placeholder for production implementation.
type APIClient struct {
	baseURL string
	apiKey  string
}

func NewAPIClient(baseURL, apiKey string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
	}
}

func (a *APIClient) Add(ctx context.Context, userID, content, runID string) error {
	return fmt.Errorf("not implemented: use MockClient for testing")
}

func (a *APIClient) Search(ctx context.Context, userID, query string) ([]string, error) {
	return nil, fmt.Errorf("not implemented: use MockClient for testing")
}

func (a *APIClient) SearchInSession(ctx context.Context, userID, runID, query string) ([]string, error) {
	return nil, fmt.Errorf("not implemented: use MockClient for testing")
}

func (a *APIClient) CreateSession(ctx context.Context, userID string) (string, error) {
	return "", fmt.Errorf("not implemented: use MockClient for testing")
}

func (a *APIClient) DeleteSession(ctx context.Context, userID, runID string) error {
	return fmt.Errorf("not implemented: use MockClient for testing")
}

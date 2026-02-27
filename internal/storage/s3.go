package storage

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
)

// Client is the interface for S3-compatible storage operations.
type Client interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Append(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
}

// TranscriptPath returns the S3 path for a transcript file.
func TranscriptPath(userID, agentID, sessionID string) string {
	return path.Join(userID, agentID, "transcripts", sessionID+".jsonl")
}

// MockS3Client is an in-memory mock S3 client for testing.
type MockS3Client struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMockS3Client creates a new mock S3 client.
func NewMockS3Client() *MockS3Client {
	return &MockS3Client{
		data: make(map[string][]byte),
	}
}

// Put stores data at the given key.
func (m *MockS3Client) Put(ctx context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), data...)
	return nil
}

// Get retrieves data from the given key.
func (m *MockS3Client) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// Append appends data to the given key.
func (m *MockS3Client) Append(ctx context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.data[key]
	if !exists {
		m.data[key] = append([]byte(nil), data...)
		return nil
	}

	combined := append(existing, data...)
	m.data[key] = combined
	return nil
}

// Delete removes data at the given key.
func (m *MockS3Client) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// AWSS3Client implements Client using AWS SDK.
// This is a placeholder for production implementation.
type AWSS3Client struct {
	bucket string
	region string
}

// NewAWSS3Client creates a new AWS S3 client.
// TODO: Implement with actual AWS SDK
func NewAWSS3Client(bucket, region string) *AWSS3Client {
	return &AWSS3Client{
		bucket: bucket,
		region: region,
	}
}

func (a *AWSS3Client) Put(ctx context.Context, key string, data []byte) error {
	return fmt.Errorf("not implemented: use MockS3Client for testing")
}

func (a *AWSS3Client) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented: use MockS3Client for testing")
}

func (a *AWSS3Client) Append(ctx context.Context, key string, data []byte) error {
	existing, err := a.Get(ctx, key)
	if err != nil && strings.Contains(err.Error(), "not found") {
		return a.Put(ctx, key, data)
	}
	if err != nil {
		return err
	}

	combined := append(existing, data...)
	return a.Put(ctx, key, combined)
}

func (a *AWSS3Client) Delete(ctx context.Context, key string) error {
	return fmt.Errorf("not implemented: use MockS3Client for testing")
}

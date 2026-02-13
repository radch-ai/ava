// Package usage provides usage tracking with multi-layer fallback.
package usage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/nevaria-ai/gateway/internal/llm-proxy"
	"github.com/rs/zerolog/log"
)

// DiskQueue provides disk-based fallback for usage events
type DiskQueue struct {
	path   string
	mu     sync.Mutex
	closed bool
}

// NewDiskQueue creates a new disk queue
func NewDiskQueue(path string) *DiskQueue {
	// Ensure directory exists
	if err := os.MkdirAll(path, 0755); err != nil {
		log.Error().Err(err).Str("path", path).Msg("Failed to create disk queue directory")
		return nil
	}

	queuePath := filepath.Join(path, "usage-events.jsonl")

	return &DiskQueue{
		path: queuePath,
	}
}

// Push adds an event to the disk queue
func (q *DiskQueue) Push(event proxy.UsageEvent) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueClosed
	}

	// Open file in append mode
	file, err := os.OpenFile(q.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	// Write event as JSONL line
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}

	return nil
}

// Pop removes and returns the next event from the queue
func (q *DiskQueue) Pop() (*proxy.UsageEvent, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, ErrQueueClosed
	}

	// Read first line from file
	file, err := os.Open(q.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrQueueEmpty
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	// Read first line
	var event proxy.UsageEvent
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&event); err != nil {
		return nil, err
	}

	// TODO: Implement proper queue rotation
	// For now, we just read the first line

	return &event, nil
}

// Close closes the disk queue
func (q *DiskQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.closed = true
	return nil
}

// Errors
var (
	ErrQueueClosed = &QueueError{Op: "close", Err: errors.New("queue is closed")}
	ErrQueueEmpty  = &QueueError{Op: "pop", Err: errors.New("queue is empty")}
)

// QueueError represents an error in queue operations
type QueueError struct {
	Op  string
	Err error
}

func (e *QueueError) Error() string {
	if e.Err != nil {
		return e.Op + ": " + e.Err.Error()
	}
	return e.Op + " failed"
}

func (e *QueueError) Unwrap() error {
	return e.Err
}

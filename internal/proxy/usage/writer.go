// Package usage provides usage tracking with multi-layer fallback.
package usage

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nevaria-ai/gateway/internal/proxy"
	"github.com/rs/zerolog/log"
)

const (
	defaultBatchSize     = 1000
	defaultFlushInterval = 30 * time.Second
	defaultMaxBufferSize = 10000
)

// UsageWriter tracks usage events with buffered batch writes
type UsageWriter struct {
	eventChan  chan proxy.UsageEvent
	buffer     []proxy.UsageEvent
	bufferMu   sync.Mutex
	ticker     *time.Ticker
	db         any // *sql.DB - using any to avoid import cycles
	redis      any // *redis.Client - using any to avoid import cycles
	diskQueue  *DiskQueue
	dbHealthy  atomic.Bool
	batchSize  int
	flushTimer *time.Timer
	flushMu    sync.Mutex
}

// NewUsageWriter creates a new usage writer with fallback
func NewUsageWriter(db any, redis any, diskQueuePath string) *UsageWriter {
	return &UsageWriter{
		eventChan: make(chan proxy.UsageEvent, defaultMaxBufferSize),
		buffer:    make([]proxy.UsageEvent, 0, defaultBatchSize),
		ticker:    time.NewTicker(defaultFlushInterval),
		db:        db,
		redis:     redis,
		diskQueue: NewDiskQueue(diskQueuePath),
		batchSize: defaultBatchSize,
		dbHealthy: atomic.Bool{},
	}
}

// Track queues a usage event for processing
func (w *UsageWriter) Track(event proxy.UsageEvent) error {
	select {
	case w.eventChan <- event:
		return nil
	default:
		// Channel full, try immediate fallback
		return w.writeWithFallback(event)
	}
}

// writeWithFallback writes an event with multi-layer fallback
func (w *UsageWriter) writeWithFallback(event proxy.UsageEvent) error {
	// Try Redis queue first
	if w.redis != nil {
		err := w.pushToRedis(event)
		if err == nil {
			return nil
		}
		log.Warn().Err(err).Msg("Redis queue failed, trying disk")
	}

	// Fall back to disk queue
	if err := w.diskQueue.Push(event); err != nil {
		log.Error().Err(err).Msg("All usage tracking methods failed - data at risk")
		return err
	}

	return nil
}

// pushToRedis pushes an event to Redis queue
func (w *UsageWriter) pushToRedis(event proxy.UsageEvent) error {
	// TODO: Implement Redis LPUSH
	// For now, return error to trigger fallback
	return ErrRedisUnavailable
}

// flush writes buffered events to database
func (w *UsageWriter) flush() error {
	w.bufferMu.Lock()
	if len(w.buffer) == 0 {
		w.bufferMu.Unlock()
		return nil
	}

	// Copy buffer and clear
	toFlush := make([]proxy.UsageEvent, len(w.buffer))
	copy(toFlush, w.buffer)
	w.buffer = w.buffer[:0]
	w.bufferMu.Unlock()

	// Try database insert
	if err := w.dbInsert(toFlush); err != nil {
		w.dbHealthy.Store(false)
		log.Warn().Err(err).Msg("DB insert failed, using fallback")
		// Move buffer to fallback
		for _, e := range toFlush {
			w.writeWithFallback(e)
		}
		return err
	}

	w.dbHealthy.Store(true)
	return nil
}

// dbInsert inserts events into the database
func (w *UsageWriter) dbInsert(events []proxy.UsageEvent) error {
	// TODO: Implement actual database insert
	// For now, just log
	log.Debug().Int("count", len(events)).Msg("Would insert usage events")
	return nil
}

// scheduleFlush schedules a flush if not already pending
func (w *UsageWriter) scheduleFlush() {
	w.flushMu.Lock()
	defer w.flushMu.Unlock()

	if w.flushTimer == nil {
		w.flushTimer = time.AfterFunc(0, func() {
			w.flush()
			w.flushMu.Lock()
			w.flushTimer = nil
			w.flushMu.Unlock()
		})
	}
}

// Start begins processing usage events
func (w *UsageWriter) Start(ctx context.Context) {
	for {
		select {
		case event := <-w.eventChan:
			w.bufferMu.Lock()
			w.buffer = append(w.buffer, event)
			shouldFlush := len(w.buffer) >= w.batchSize
			w.bufferMu.Unlock()

			if shouldFlush {
				w.flush()
			}

		case <-w.ticker.C:
			w.flush()

		case <-ctx.Done():
			w.flush() // Final flush
			w.ticker.Stop()
			return
		}
	}
}

// TrackUsage is a convenience function to track usage with the global writer
func TrackUsage(ctx context.Context, event proxy.UsageEvent) error {
	// For now, just log the event
	log.Info().
		Str("user_id", event.UserID).
		Str("model", event.Model).
		Int("tokens_in", event.TokensInput).
		Int("tokens_out", event.TokensOutput).
		Float64("cost", event.CostTotal).
		Msg("Usage tracked")
	return nil
}

// Errors
var (
	ErrRedisUnavailable   = &UsageError{Op: "redis_push", Err: ErrRedisNotConfigured}
	ErrRedisNotConfigured = &UsageError{Op: "redis", Err: &FallbackError{Message: "redis not configured"}}
)

// UsageError represents an error in usage tracking
type UsageError struct {
	Op  string
	Err error
}

func (e *UsageError) Error() string {
	if e.Err != nil {
		return e.Op + ": " + e.Err.Error()
	}
	return e.Op + " failed"
}

func (e *UsageError) Unwrap() error {
	return e.Err
}

// FallbackError represents a fallback to a lower-tier storage
type FallbackError struct {
	Message string
}

func (e *FallbackError) Error() string {
	return "fallback: " + e.Message
}

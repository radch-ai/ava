// Package usage provides usage tracking with Redis fallback.
package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/nevaria-ai/gateway/internal/llm-proxy"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	defaultBatchSize     = 1000
	defaultFlushInterval = 30 * time.Second
	defaultMaxBufferSize = 10000
)

// UsageWriter tracks usage events with buffered batch writes
type UsageWriter struct {
	eventChan chan proxy.UsageEvent
	buffer    []proxy.UsageEvent
	bufferMu  sync.Mutex
	ticker    *time.Ticker
	db        any // *sql.DB - using any to avoid import cycles
	redis     any // *redis.Client - using any to avoid import cycles
	dbHealthy atomic.Bool
	batchSize int
}

// NewUsageWriter creates a new usage writer with Redis fallback
func NewUsageWriter(db any, redis any) *UsageWriter {
	return &UsageWriter{
		eventChan: make(chan proxy.UsageEvent, defaultMaxBufferSize),
		buffer:    make([]proxy.UsageEvent, 0, defaultBatchSize),
		ticker:    time.NewTicker(defaultFlushInterval),
		db:        db,
		redis:     redis,
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
		// Channel full, try immediate fallback to Redis
		return w.pushToRedis(event)
	}
}

// pushToRedis pushes an event to Redis queue as fallback
func (w *UsageWriter) pushToRedis(event proxy.UsageEvent) error {
	if w.redis == nil {
		return ErrRedisNotConfigured
	}

	// Type assert to redis.Client
	client, ok := w.redis.(*redis.Client)
	if !ok {
		return ErrRedisNotConfigured
	}

	// Serialize event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return &UsageError{Op: "redis_push", Err: err}
	}

	// Push to Redis list with key format "usage:<userID>"
	key := "usage:" + event.UserID
	ctx := context.Background()

	if err := client.LPush(ctx, key, data).Err(); err != nil {
		return &UsageError{Op: "redis_push", Err: err}
	}

	return nil
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
		log.Warn().Err(err).Msg("DB insert failed, using Redis fallback")
		// Move buffer to Redis
		for _, e := range toFlush {
			_ = w.pushToRedis(e)
		}
		return err
	}

	w.dbHealthy.Store(true)
	return nil
}

// dbInsert inserts events into the database
func (w *UsageWriter) dbInsert(events []proxy.UsageEvent) error {
	// Type assert db to sql.DB
	db, ok := w.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("database is not *sql.DB, got %T", w.db)
	}

	// Begin transaction for atomic insert
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Prepare insert statement
	stmt, err := tx.Prepare(`
		INSERT INTO usage_events (
			user_id, session_id, model, provider,
			tokens_input, tokens_output, cost_total,
			charged_from, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	// Execute insert for each event
	for _, event := range events {
		_, err := stmt.Exec(
			event.UserID,
			event.SessionID,
			event.Model,
			event.Provider,
			event.TokensInput,
			event.TokensOutput,
			event.CostTotal,
			event.ChargedFrom,
			event.Timestamp,
		)
		if err != nil {
			// Log error but continue with other events
			log.Error().Err(err).
				Str("user_id", event.UserID).
				Str("model", event.Model).
				Msg("failed to insert usage event")
			continue
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Log successful batch insert
	log.Debug().Int("count", len(events)).Msg("Inserted usage events into database")

	return nil
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
				_ = w.flush()
			}

		case <-w.ticker.C:
			_ = w.flush()

		case <-ctx.Done():
			_ = w.flush() // Final flush
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

// FallbackError represents a fallback to Redis
type FallbackError struct {
	Message string
}

func (e *FallbackError) Error() string {
	return "fallback: " + e.Message
}

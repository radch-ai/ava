package api

import (
	"sync"
	"time"
)

// Throttle absorbs toggle bursts: one accepted write per key per
// window. Human-paced toggles never share a window; machine fire on
// one target does and draws a 429.
type Throttle struct {
	mu     sync.Mutex
	last   map[string]time.Time
	window time.Duration
}

// NewThrottle builds a throttle; a zero window admits everything.
func NewThrottle(window time.Duration) *Throttle {
	return &Throttle{last: map[string]time.Time{}, window: window}
}

// Allow records the write and reports whether the key may proceed.
func (t *Throttle) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.window <= 0 {
		return true
	}
	now := time.Now()
	if prev, ok := t.last[key]; ok && now.Sub(prev) < t.window {
		return false
	}
	t.last[key] = now
	return true
}

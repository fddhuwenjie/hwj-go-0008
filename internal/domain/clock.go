package domain

import (
	"sync"
	"time"
)

// Clock returns the current time. It is an abstraction so that tests can
// deterministically control time (critical for testing lock expiry and audit
// timestamps) while production code uses the wall clock.
type Clock interface {
	Now() time.Time
}

// SystemClock returns the real wall-clock time.
type SystemClock struct{}

// Now implements Clock.
func (SystemClock) Now() time.Time { return time.Now() }

// ManualClock is a controllable Clock for tests and deterministic scenarios.
// It is safe for concurrent use: its own mutex is independent of any service
// mutex, so it can be advanced while a service operation holds its lock without
// risk of deadlock.
type ManualClock struct {
	mu  sync.RWMutex
	now time.Time
}

// NewManualClock returns a ManualClock set to t.
func NewManualClock(t time.Time) *ManualClock {
	return &ManualClock{now: t}
}

// Now implements Clock.
func (c *ManualClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

// Set replaces the clock's current time.
func (c *ManualClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the clock forward by d.
func (c *ManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

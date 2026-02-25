package agent

import (
	"context"
	"sync"
	"time"
)

// Throttle implements a sliding-window rate limiter shared across all Gemini
// agents. It tracks request timestamps within a rolling window and blocks
// callers when the limit is reached until a slot becomes available.
type Throttle struct {
	mu       sync.Mutex
	rpm      int
	window   time.Duration // defaults to 1 minute; exposed for testing
	requests []time.Time
}

// NewThrottle creates a new Throttle allowing rpm requests per minute.
// Returns nil if rpm <= 0, making nil a safe no-op value.
func NewThrottle(rpm int) *Throttle {
	if rpm <= 0 {
		return nil
	}
	return &Throttle{
		rpm:    rpm,
		window: time.Minute,
	}
}

// Wait blocks until a request slot is available within the sliding window,
// or until ctx is cancelled. A nil Throttle is a no-op and returns nil
// immediately.
func (t *Throttle) Wait(ctx context.Context) error {
	if t == nil {
		return nil
	}

	for {
		wait := t.nextWait()
		if wait == 0 {
			t.record()
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			// Re-check in case another goroutine consumed the slot.
			continue
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

// nextWait returns how long the caller must wait before a slot opens.
// Returns 0 if a slot is immediately available.
func (t *Throttle) nextWait() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	// Prune expired entries.
	i := 0
	for i < len(t.requests) && t.requests[i].Before(cutoff) {
		i++
	}
	t.requests = t.requests[i:]

	if len(t.requests) < t.rpm {
		return 0
	}

	// The oldest request in the window determines when the next slot opens.
	return t.requests[0].Add(t.window).Sub(now)
}

// record appends the current timestamp to the request log.
func (t *Throttle) record() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests = append(t.requests, time.Now())
}

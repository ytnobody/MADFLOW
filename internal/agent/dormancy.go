package agent

import (
	"context"
	"log"
	"sync"
	"time"
)

// DefaultProbeInterval is the initial interval between rate-limit recovery probes.
const DefaultProbeInterval = 15 * time.Minute

// MaxProbeInterval is the upper bound for exponential backoff.
const MaxProbeInterval = 60 * time.Minute

// ProbeFunc tests whether the rate limit has been lifted.
// It should return nil if the limit is cleared.
type ProbeFunc func(ctx context.Context) error

// Dormancy manages rate-limit-induced sleep shared across all agents.
// When any agent detects a token limit error, all agents stop sending
// requests until a periodic probe confirms the limit has been lifted.
type Dormancy struct {
	mu            sync.Mutex
	sleeping      bool
	wakeCh        chan struct{}
	ProbeInterval time.Duration // configurable for testing
}

// NewDormancy creates a new Dormancy instance.
// An optional probeInterval can be provided; if zero or omitted, DefaultProbeInterval is used.
func NewDormancy(probeInterval ...time.Duration) *Dormancy {
	interval := DefaultProbeInterval
	if len(probeInterval) > 0 && probeInterval[0] > 0 {
		interval = probeInterval[0]
	}
	return &Dormancy{
		wakeCh:        make(chan struct{}),
		ProbeInterval: interval,
	}
}

// Enter puts all agents into dormant mode.
// If already sleeping, this is a no-op.
// A background goroutine probes periodically using probeFn.
func (d *Dormancy) Enter(ctx context.Context, probeFn ProbeFunc) {
	d.mu.Lock()
	if d.sleeping {
		d.mu.Unlock()
		return
	}
	d.sleeping = true
	d.wakeCh = make(chan struct{})
	d.mu.Unlock()

	log.Println("[dormancy] entering sleep mode (rate limit detected)")

	go d.probeLoop(ctx, probeFn)
}

// probeLoop calls probeFn with exponential backoff until it succeeds without a rate limit error.
func (d *Dormancy) probeLoop(ctx context.Context, probeFn ProbeFunc) {
	interval := d.ProbeInterval
	if interval <= 0 {
		interval = DefaultProbeInterval
	}

	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			log.Printf("[dormancy] probing rate limit status (interval=%s)...", interval)
			err := probeFn(ctx)
			if err == nil || !IsRateLimitError(err) {
				d.wake()
				return
			}
			log.Printf("[dormancy] still rate limited, next probe in %s", interval*2)
			interval *= 2
			if interval > MaxProbeInterval {
				interval = MaxProbeInterval
			}
		}
	}
}

// wake lifts dormancy and unblocks all waiting agents.
func (d *Dormancy) wake() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sleeping {
		d.sleeping = false
		close(d.wakeCh)
		log.Println("[dormancy] rate limit lifted, resuming all agents")
	}
}

// Sleeping returns whether dormancy is currently active.
func (d *Dormancy) Sleeping() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sleeping
}

// Wait blocks until dormancy ends or ctx is cancelled.
// Returns immediately if not sleeping.
func (d *Dormancy) Wait(ctx context.Context) error {
	d.mu.Lock()
	if !d.sleeping {
		d.mu.Unlock()
		return nil
	}
	ch := d.wakeCh
	d.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

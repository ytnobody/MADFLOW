package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestDormancyWaitWhenAwake(t *testing.T) {
	d := NewDormancy()

	ctx := context.Background()
	if err := d.Wait(ctx); err != nil {
		t.Fatalf("Wait should return nil when not sleeping: %v", err)
	}
}

func TestDormancySleeping(t *testing.T) {
	d := NewDormancy()

	if d.Sleeping() {
		t.Error("should not be sleeping initially")
	}

	// Enter dormancy with a probe that always fails
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.ProbeInterval = 1 * time.Hour // won't fire during test
	d.Enter(ctx, func(ctx context.Context) error {
		return fmt.Errorf("rate limit exceeded")
	})

	if !d.Sleeping() {
		t.Error("should be sleeping after Enter")
	}
}

func TestDormancyEnterIdempotent(t *testing.T) {
	d := NewDormancy()
	d.ProbeInterval = 1 * time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	probeFn := func(ctx context.Context) error {
		return fmt.Errorf("rate limit exceeded")
	}

	d.Enter(ctx, probeFn)
	d.Enter(ctx, probeFn) // second call should be no-op

	if !d.Sleeping() {
		t.Error("should be sleeping")
	}
}

func TestDormancyProbeWakes(t *testing.T) {
	d := NewDormancy()
	d.ProbeInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls int32

	// Probe fails first time, succeeds second time
	d.Enter(ctx, func(ctx context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n <= 1 {
			return fmt.Errorf("rate limit exceeded")
		}
		return nil
	})

	// Wait should unblock when probe succeeds
	done := make(chan struct{})
	go func() {
		d.Wait(ctx)
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after probe succeeded")
	}

	if d.Sleeping() {
		t.Error("should not be sleeping after probe succeeded")
	}
}

func TestDormancyWaitBlocksUntilWake(t *testing.T) {
	d := NewDormancy()
	d.ProbeInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Probe succeeds immediately
	d.Enter(ctx, func(ctx context.Context) error {
		return nil
	})

	done := make(chan struct{})
	go func() {
		d.Wait(ctx)
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return")
	}
}

func TestDormancyWaitCancelledContext(t *testing.T) {
	d := NewDormancy()
	d.ProbeInterval = 1 * time.Hour

	ctx, cancel := context.WithCancel(context.Background())

	d.Enter(ctx, func(ctx context.Context) error {
		return fmt.Errorf("rate limit exceeded")
	})

	// Cancel context while waiting
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := d.Wait(ctx)
	if err == nil {
		t.Error("expected context error from Wait")
	}
}

func TestDormancyExponentialBackoff(t *testing.T) {
	d := NewDormancy()
	d.ProbeInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var probeTimes []time.Time

	// Probe fails 3 times, then succeeds
	d.Enter(ctx, func(ctx context.Context) error {
		probeTimes = append(probeTimes, time.Now())
		if len(probeTimes) < 4 {
			return fmt.Errorf("rate limit exceeded")
		}
		return nil
	})

	done := make(chan struct{})
	go func() {
		d.Wait(ctx)
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after backoff probes")
	}

	if len(probeTimes) != 4 {
		t.Fatalf("expected 4 probes, got %d", len(probeTimes))
	}

	// Verify intervals are increasing (with some tolerance for scheduling jitter)
	for i := 2; i < len(probeTimes); i++ {
		prev := probeTimes[i-1].Sub(probeTimes[i-2])
		curr := probeTimes[i].Sub(probeTimes[i-1])
		// Each interval should be roughly 2x the previous (allow 50% tolerance)
		if curr < prev {
			t.Errorf("interval %d (%s) should be >= interval %d (%s)", i, curr, i-1, prev)
		}
	}
}

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("some random error"), false},
		{fmt.Errorf("claude process failed: rate limit exceeded"), true},
		{fmt.Errorf("stderr: Rate Limit error"), true},
		{fmt.Errorf("token limit reached"), true},
		{fmt.Errorf("usage limit exceeded"), true},
		{fmt.Errorf("too many requests"), true},
		{fmt.Errorf("error code 429"), true},
		{fmt.Errorf("overloaded"), true},
	}

	for _, tt := range tests {
		got := IsRateLimitError(tt.err)
		if got != tt.want {
			t.Errorf("IsRateLimitError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

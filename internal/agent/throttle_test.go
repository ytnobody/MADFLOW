package agent

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestThrottleNilIsNoOp(t *testing.T) {
	var th *Throttle
	if err := th.Wait(context.Background()); err != nil {
		t.Fatalf("nil Throttle.Wait returned error: %v", err)
	}
}

func TestNewThrottleZeroRPMReturnsNil(t *testing.T) {
	if th := NewThrottle(0); th != nil {
		t.Fatal("expected nil for rpm=0")
	}
	if th := NewThrottle(-5); th != nil {
		t.Fatal("expected nil for rpm=-5")
	}
}

func TestThrottleAllowsUnderLimit(t *testing.T) {
	th := NewThrottle(5)
	th.window = 100 * time.Millisecond

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		start := time.Now()
		if err := th.Wait(ctx); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
			t.Fatalf("request %d took too long: %v", i, elapsed)
		}
	}
}

func TestThrottleBlocksAtLimit(t *testing.T) {
	th := NewThrottle(3)
	th.window = 200 * time.Millisecond

	ctx := context.Background()
	// Fill the window.
	for i := 0; i < 3; i++ {
		if err := th.Wait(ctx); err != nil {
			t.Fatalf("fill request %d: %v", i, err)
		}
	}

	// The 4th request must block until the window slides.
	start := time.Now()
	if err := th.Wait(ctx); err != nil {
		t.Fatalf("blocked request: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Fatalf("expected blocking ~200ms, got %v", elapsed)
	}
}

func TestThrottleRespectsContextCancellation(t *testing.T) {
	th := NewThrottle(1)
	th.window = 5 * time.Second // long window so it will definitely block

	ctx := context.Background()
	// Fill the single slot.
	if err := th.Wait(ctx); err != nil {
		t.Fatalf("first request: %v", err)
	}

	// Cancel context while blocked.
	cancelCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := th.Wait(cancelCtx)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestThrottleConcurrentAccess(t *testing.T) {
	th := NewThrottle(10)
	th.window = 500 * time.Millisecond

	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := th.Wait(ctx); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Wait error: %v", err)
	}
}

// TestThrottleTryAcquireAtomic verifies that tryAcquire is atomic: concurrent goroutines
// cannot over-acquire slots beyond the rpm limit within a single window.
func TestThrottleTryAcquireAtomic(t *testing.T) {
	const rpm = 5
	th := NewThrottle(rpm)
	th.window = 2 * time.Second // long window to prevent expiry during test

	var wg sync.WaitGroup
	acquired := make(chan struct{}, rpm*3) // buffer for all attempts

	// Launch many goroutines all trying to acquire at once
	for i := 0; i < rpm*3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wait := th.tryAcquire()
			if wait == 0 {
				acquired <- struct{}{}
			}
		}()
	}

	wg.Wait()
	close(acquired)

	count := 0
	for range acquired {
		count++
	}

	if count > rpm {
		t.Errorf("tryAcquire allowed %d acquires, but rpm limit is %d", count, rpm)
	}
	if count < rpm {
		t.Errorf("tryAcquire only allowed %d acquires, expected %d", count, rpm)
	}
}

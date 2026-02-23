package github

import (
	"sync"
	"testing"
	"time"
)

func TestNewIdleDetector_StartsActive(t *testing.T) {
	d := NewIdleDetector()
	if !d.HasIssues() {
		t.Error("expected HasIssues() to return true initially")
	}
	if d.IsIdle() {
		t.Error("expected IsIdle() to return false initially")
	}
}

func TestIdleDetector_SetHasIssues(t *testing.T) {
	d := NewIdleDetector()

	d.SetHasIssues(false)
	if d.HasIssues() {
		t.Error("expected HasIssues() to return false after SetHasIssues(false)")
	}

	d.SetHasIssues(true)
	if !d.HasIssues() {
		t.Error("expected HasIssues() to return true after SetHasIssues(true)")
	}
}

func TestIdleDetector_IsIdle_NoThreshold_WithIssues(t *testing.T) {
	d := NewIdleDetector() // hasIssues=true, threshold=0
	if d.IsIdle() {
		t.Error("expected IsIdle()=false when has issues")
	}
}

func TestIdleDetector_IsIdle_NoThreshold_WithoutIssues(t *testing.T) {
	d := NewIdleDetector()
	d.SetHasIssues(false)
	// With threshold=0, idle immediately
	if !d.IsIdle() {
		t.Error("expected IsIdle()=true when no issues and threshold=0")
	}
}

func TestIdleDetector_IsIdle_WithThreshold_NotYetExpired(t *testing.T) {
	d := NewIdleDetector()
	d.SetIdleThreshold(10 * time.Minute)
	d.SetHasIssues(false)

	// Threshold not yet reached → not idle
	if d.IsIdle() {
		t.Error("expected IsIdle()=false when threshold not yet expired")
	}
}

func TestIdleDetector_IsIdle_WithThreshold_Expired(t *testing.T) {
	d := NewIdleDetector()
	// Use a very short threshold
	d.SetIdleThreshold(1 * time.Millisecond)
	d.SetHasIssues(false)

	// Wait for threshold to expire
	time.Sleep(5 * time.Millisecond)

	if !d.IsIdle() {
		t.Error("expected IsIdle()=true when threshold has expired")
	}
}

func TestIdleDetector_IsIdle_RevertOnIssues(t *testing.T) {
	d := NewIdleDetector()
	d.SetIdleThreshold(1 * time.Millisecond)
	d.SetHasIssues(false)
	time.Sleep(5 * time.Millisecond)

	// Confirm idle
	if !d.IsIdle() {
		t.Fatal("expected idle before issue return")
	}

	// Issues return → no longer idle
	d.SetHasIssues(true)
	if d.IsIdle() {
		t.Error("expected IsIdle()=false after issues return")
	}
}

func TestIdleDetector_SetHasIssues_IdempotentFalse(t *testing.T) {
	// Calling SetHasIssues(false) multiple times should not reset issuesGoneAt
	d := NewIdleDetector()
	d.SetHasIssues(false)
	firstGoneAt := d.issuesGoneAt

	time.Sleep(5 * time.Millisecond)
	d.SetHasIssues(false) // second call - should keep original issuesGoneAt

	if d.issuesGoneAt != firstGoneAt {
		t.Error("expected issuesGoneAt to remain unchanged on repeated SetHasIssues(false)")
	}
}

func TestIdleDetector_AdaptInterval_WithIssues(t *testing.T) {
	d := NewIdleDetector() // starts with hasIssues=true
	normal := 30 * time.Second
	idle := 15 * time.Minute

	got := d.AdaptInterval(normal, idle)
	if got != normal {
		t.Errorf("expected normal interval %v when has issues, got %v", normal, got)
	}
}

func TestIdleDetector_AdaptInterval_WithoutIssues(t *testing.T) {
	d := NewIdleDetector()
	d.SetHasIssues(false)

	normal := 30 * time.Second
	idle := 15 * time.Minute

	got := d.AdaptInterval(normal, idle)
	if got != idle {
		t.Errorf("expected idle interval %v when no issues (threshold=0), got %v", idle, got)
	}
}

func TestIdleDetector_AdaptInterval_IdleNotSlowerThanNormal(t *testing.T) {
	d := NewIdleDetector()
	d.SetHasIssues(false)

	// When idle <= normal, always return normal (idle interval must be slower)
	normal := 15 * time.Minute
	idle := 30 * time.Second // idle is actually faster - should be ignored

	got := d.AdaptInterval(normal, idle)
	if got != normal {
		t.Errorf("expected normal interval %v when idle <= normal, got %v", normal, got)
	}
}

func TestIdleDetector_AdaptInterval_EqualIntervals(t *testing.T) {
	d := NewIdleDetector()
	d.SetHasIssues(false)

	interval := 15 * time.Minute
	got := d.AdaptInterval(interval, interval)
	if got != interval {
		t.Errorf("expected %v when idle == normal, got %v", interval, got)
	}
}

func TestIdleDetector_AdaptInterval_ThresholdNotExpired(t *testing.T) {
	d := NewIdleDetector()
	d.SetIdleThreshold(10 * time.Minute) // long threshold
	d.SetHasIssues(false)

	normal := 30 * time.Second
	idle := 15 * time.Minute

	// Threshold not expired → should use normal interval
	got := d.AdaptInterval(normal, idle)
	if got != normal {
		t.Errorf("expected normal interval when threshold not expired, got %v", got)
	}
}

func TestIdleDetector_ConcurrencySafe(t *testing.T) {
	d := NewIdleDetector()
	var wg sync.WaitGroup

	// Many concurrent reads and writes should not cause data races
	for i := range 100 {
		wg.Add(3)
		go func(b bool) {
			defer wg.Done()
			d.SetHasIssues(b)
		}(i%2 == 0)
		go func() {
			defer wg.Done()
			_ = d.HasIssues()
		}()
		go func() {
			defer wg.Done()
			_ = d.IsIdle()
		}()
	}
	wg.Wait()
}

func TestIdleDetector_AdaptInterval_Transitions(t *testing.T) {
	d := NewIdleDetector()
	normal := 60 * time.Second
	idle := 15 * time.Minute

	// Start active → normal interval
	if got := d.AdaptInterval(normal, idle); got != normal {
		t.Errorf("step1: expected %v, got %v", normal, got)
	}

	// No issues → idle interval (threshold=0)
	d.SetHasIssues(false)
	if got := d.AdaptInterval(normal, idle); got != idle {
		t.Errorf("step2: expected %v, got %v", idle, got)
	}

	// Issues return → back to normal
	d.SetHasIssues(true)
	if got := d.AdaptInterval(normal, idle); got != normal {
		t.Errorf("step3: expected %v, got %v", normal, got)
	}
}

// --- Dormancy tests ---

func TestIdleDetector_IsDormant_DisabledByDefault(t *testing.T) {
	d := NewIdleDetector()
	d.SetHasIssues(false)

	// dormancyThreshold is 0 by default → dormancy disabled
	if d.IsDormant() {
		t.Error("expected IsDormant()=false when dormancyThreshold is 0 (disabled)")
	}
}

func TestIdleDetector_IsDormant_WithIssues(t *testing.T) {
	d := NewIdleDetector()
	d.SetDormancyThreshold(1 * time.Millisecond)
	// hasIssues is true → never dormant

	time.Sleep(5 * time.Millisecond)

	if d.IsDormant() {
		t.Error("expected IsDormant()=false when there are active issues")
	}
}

func TestIdleDetector_IsDormant_ThresholdNotExpired(t *testing.T) {
	d := NewIdleDetector()
	d.SetDormancyThreshold(10 * time.Minute)
	d.SetHasIssues(false)

	// Threshold not yet reached → not dormant
	if d.IsDormant() {
		t.Error("expected IsDormant()=false when dormancy threshold not yet expired")
	}
}

func TestIdleDetector_IsDormant_ThresholdExpired(t *testing.T) {
	d := NewIdleDetector()
	d.SetDormancyThreshold(1 * time.Millisecond)
	d.SetHasIssues(false)

	// Wait for threshold to expire
	time.Sleep(5 * time.Millisecond)

	if !d.IsDormant() {
		t.Error("expected IsDormant()=true when dormancy threshold has expired")
	}
}

func TestIdleDetector_IsDormant_RevertOnWake(t *testing.T) {
	d := NewIdleDetector()
	d.SetDormancyThreshold(1 * time.Millisecond)
	d.SetHasIssues(false)
	time.Sleep(5 * time.Millisecond)

	// Confirm dormant
	if !d.IsDormant() {
		t.Fatal("expected dormant before wake")
	}

	// Wake → no longer dormant
	d.Wake()
	if d.IsDormant() {
		t.Error("expected IsDormant()=false after Wake()")
	}
	if !d.HasIssues() {
		t.Error("expected HasIssues()=true after Wake()")
	}
}

func TestIdleDetector_Wake_ResetsIdleState(t *testing.T) {
	d := NewIdleDetector()
	d.SetIdleThreshold(1 * time.Millisecond)
	d.SetDormancyThreshold(1 * time.Millisecond)
	d.SetHasIssues(false)
	time.Sleep(5 * time.Millisecond)

	// Both idle and dormant
	if !d.IsIdle() {
		t.Fatal("expected idle")
	}
	if !d.IsDormant() {
		t.Fatal("expected dormant")
	}

	// Wake resets both
	d.Wake()
	if d.IsIdle() {
		t.Error("expected IsIdle()=false after Wake()")
	}
	if d.IsDormant() {
		t.Error("expected IsDormant()=false after Wake()")
	}
}

func TestIdleDetector_IsDormant_SetHasIssuesWakes(t *testing.T) {
	d := NewIdleDetector()
	d.SetDormancyThreshold(1 * time.Millisecond)
	d.SetHasIssues(false)
	time.Sleep(5 * time.Millisecond)

	if !d.IsDormant() {
		t.Fatal("expected dormant")
	}

	// SetHasIssues(true) exits dormancy
	d.SetHasIssues(true)
	if d.IsDormant() {
		t.Error("expected IsDormant()=false after SetHasIssues(true)")
	}
}

func TestIdleDetector_IsDormant_WithIdleThreshold(t *testing.T) {
	// IsDormant uses the same issuesGoneAt as idle, so idle and dormancy share the timer.
	// With idleThreshold=1ms and dormancyThreshold=10ms, at 5ms should be idle but not dormant.
	d := NewIdleDetector()
	d.SetIdleThreshold(1 * time.Millisecond)
	d.SetDormancyThreshold(100 * time.Millisecond)
	d.SetHasIssues(false)

	time.Sleep(5 * time.Millisecond)

	// Should be idle (1ms expired) but not dormant (100ms not yet expired)
	if !d.IsIdle() {
		t.Error("expected IsIdle()=true")
	}
	if d.IsDormant() {
		t.Error("expected IsDormant()=false (dormancy threshold not yet expired)")
	}
}

func TestIdleDetector_IsDormant_ConcurrencySafe(t *testing.T) {
	d := NewIdleDetector()
	d.SetDormancyThreshold(1 * time.Millisecond)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(3)
		go func(b bool) {
			defer wg.Done()
			d.SetHasIssues(b)
		}(i%2 == 0)
		go func() {
			defer wg.Done()
			_ = d.IsDormant()
		}()
		go func() {
			defer wg.Done()
			d.Wake()
		}()
	}
	wg.Wait()
}

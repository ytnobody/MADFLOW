package github

import (
	"log"
	"sync"
	"time"
)

// IdleDetector tracks whether the system has active GitHub issues.
// When there are no active issues for longer than idleThreshold, the system
// is considered "idle" and pollers should reduce their polling frequency
// to avoid unnecessary GitHub API calls.
//
// When there are no active issues for longer than dormancyThreshold (measured
// from when issues first disappeared), the system enters "dormancy" and pollers
// should stop making GitHub API calls entirely. Dormancy can be exited by
// calling Wake(), which resets the detector to the active state.
//
// The detector is concurrency-safe and can be shared between Syncer and EventWatcher.
type IdleDetector struct {
	mu                sync.RWMutex
	hasIssues         bool
	issuesGoneAt      time.Time     // when hasIssues last became false
	idleThreshold     time.Duration // how long with no issues before going idle (0 = immediately)
	dormancyThreshold time.Duration // how long with no issues before entering dormancy (0 = disabled)
}

// NewIdleDetector creates an IdleDetector that starts in an active state
// (assuming there may be issues until confirmed otherwise).
// The default threshold is 0 (idle immediately when no issues are found).
// Use SetIdleThreshold to configure a delay before entering idle mode.
// Use SetDormancyThreshold to configure when polling stops entirely.
func NewIdleDetector() *IdleDetector {
	return &IdleDetector{hasIssues: true}
}

// SetIdleThreshold sets how long the system must have no active issues before
// entering idle mode. A threshold of 0 means entering idle mode immediately.
// This method is safe to call concurrently.
func (d *IdleDetector) SetIdleThreshold(threshold time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.idleThreshold = threshold
}

// SetDormancyThreshold sets how long the system must have no active issues before
// entering dormancy (completely stopping GitHub API polling).
// A threshold of 0 (the default) disables dormancy.
// The threshold is measured from when issues first disappeared, so it should be
// larger than idleThreshold to form a progression: active → idle → dormant.
// This method is safe to call concurrently.
func (d *IdleDetector) SetDormancyThreshold(threshold time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dormancyThreshold = threshold
}

// SetHasIssues updates whether there are currently active (open/in-progress) issues.
// This is typically called by the Syncer after each sync cycle, and by the EventWatcher
// when a new issue event is detected.
func (d *IdleDetector) SetHasIssues(has bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if has {
		d.hasIssues = true
		d.issuesGoneAt = time.Time{} // reset
	} else if d.hasIssues {
		// Transition from active to inactive: record the time
		d.hasIssues = false
		d.issuesGoneAt = time.Now()
	}
	// If already !hasIssues, keep existing issuesGoneAt
}

// HasIssues returns true if there are currently active issues.
func (d *IdleDetector) HasIssues() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.hasIssues
}

// IsIdle returns true if the system has been without active issues for at least
// idleThreshold duration. If idleThreshold is 0, returns true immediately when
// there are no active issues.
func (d *IdleDetector) IsIdle() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.hasIssues {
		return false
	}
	if d.idleThreshold <= 0 {
		return true
	}
	return time.Since(d.issuesGoneAt) >= d.idleThreshold
}

// AdaptInterval returns normal if the system is not idle, or idle if it is.
// If idle <= normal, the normal interval is always returned (idle mode must be slower).
func (d *IdleDetector) AdaptInterval(normal, idle time.Duration) time.Duration {
	if !d.IsIdle() || idle <= normal {
		return normal
	}
	return idle
}

// IsDormant returns true if the system has been without active issues for at least
// dormancyThreshold duration. Returns false if dormancyThreshold is 0 (dormancy disabled).
func (d *IdleDetector) IsDormant() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.hasIssues || d.dormancyThreshold <= 0 {
		return false
	}
	return time.Since(d.issuesGoneAt) >= d.dormancyThreshold
}

// Wake resets the detector to the active state, ending any idle or dormancy condition.
// This causes IsDormant() and IsIdle() to return false until the next time issues
// disappear for the configured threshold durations.
// Typically called when an operator signals that the system should resume polling,
// for example via the WAKE_GITHUB orchestrator command.
func (d *IdleDetector) Wake() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hasIssues = true
	d.issuesGoneAt = time.Time{}
	log.Println("[idle-detector] woken: GitHub polling resuming")
}

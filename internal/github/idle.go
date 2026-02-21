package github

import (
	"sync"
	"time"
)

// IdleDetector tracks whether the system has active GitHub issues.
// When there are no active issues for longer than idleThreshold, the system
// is considered "idle" and pollers should reduce their polling frequency
// to avoid unnecessary GitHub API calls.
//
// The detector is concurrency-safe and can be shared between Syncer and EventWatcher.
type IdleDetector struct {
	mu            sync.RWMutex
	hasIssues     bool
	issuesGoneAt  time.Time     // when hasIssues last became false
	idleThreshold time.Duration // how long with no issues before going idle (0 = immediately)
}

// NewIdleDetector creates an IdleDetector that starts in an active state
// (assuming there may be issues until confirmed otherwise).
// The default threshold is 0 (idle immediately when no issues are found).
// Use SetIdleThreshold to configure a delay before entering idle mode.
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

package github

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"time"
)

const (
	// defaultRateLimitThreshold is the minimum number of remaining GitHub API
	// calls required before the Syncer will proceed with an API request.
	// When remaining <= threshold, the Syncer waits for the rate limit to reset
	// or skips the current sync cycle if the wait would be too long.
	defaultRateLimitThreshold = 10

	// rateLimitMaxWait is the maximum duration the Syncer will wait for the
	// GitHub API rate limit to reset. If the reset time is further away than
	// this duration, the sync cycle is skipped instead of waiting.
	rateLimitMaxWait = 10 * time.Minute
)

// ghRateLimitResponse represents the JSON response from `gh api rate_limit`.
type ghRateLimitResponse struct {
	Resources struct {
		Core ghRateLimitResource `json:"core"`
	} `json:"resources"`
}

// ghRateLimitResource holds the rate limit details for a single resource category.
type ghRateLimitResource struct {
	Limit     int   `json:"limit"`
	Remaining int   `json:"remaining"`
	Reset     int64 `json:"reset"` // Unix timestamp when the rate limit resets
	Used      int   `json:"used"`
}

// WithRateLimitThreshold sets the minimum remaining GitHub API calls required
// before the Syncer will proceed. When the remaining count is at or below this
// threshold, the Syncer either waits for the rate limit to reset (if the reset
// is within rateLimitMaxWait) or skips the current sync cycle.
//
// Setting threshold to 0 disables the rate limit pre-check entirely.
func (s *Syncer) WithRateLimitThreshold(n int) *Syncer {
	s.rateLimitThreshold = n
	return s
}

// checkRateLimit fetches the current GitHub API rate limit and waits or returns
// an error if the remaining count is at or below the configured threshold.
//
// If the threshold is 0, the check is disabled and nil is always returned.
// If the gh CLI call fails, a warning is logged and nil is returned (fail-open).
func (s *Syncer) checkRateLimit() error {
	if s.rateLimitThreshold == 0 {
		return nil
	}

	out, err := exec.Command("gh", "api", "rate_limit").Output()
	if err != nil {
		// Fail open: log a warning but allow the API call to proceed.
		log.Printf("[github-sync] rate limit check failed (proceeding anyway): %v", err)
		return nil
	}

	var resp ghRateLimitResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		log.Printf("[github-sync] rate limit parse failed (proceeding anyway): %v", err)
		return nil
	}

	return s.checkRateLimitWithData(resp)
}

// checkRateLimitWithData evaluates the rate limit response and decides whether
// to wait, skip, or proceed. It is separated from checkRateLimit to allow
// unit testing without invoking the gh CLI.
func (s *Syncer) checkRateLimitWithData(resp ghRateLimitResponse) error {
	if s.rateLimitThreshold == 0 {
		return nil
	}

	core := resp.Resources.Core
	if core.Remaining > s.rateLimitThreshold {
		return nil
	}

	resetAt := time.Unix(core.Reset, 0)
	waitDuration := time.Until(resetAt)

	if waitDuration <= 0 {
		// Reset has already occurred; proceed immediately.
		return nil
	}

	if waitDuration > rateLimitMaxWait {
		return fmt.Errorf(
			"github rate limit nearly exhausted (remaining=%d, threshold=%d); "+
				"reset in %v exceeds max wait %v — skipping sync cycle",
			core.Remaining, s.rateLimitThreshold, waitDuration.Round(time.Second), rateLimitMaxWait,
		)
	}

	// Wait until the rate limit resets, with a small buffer.
	waitWithBuffer := waitDuration + time.Second
	log.Printf(
		"[github-sync] rate limit nearly exhausted (remaining=%d, threshold=%d); "+
			"waiting %v for reset",
		core.Remaining, s.rateLimitThreshold, waitWithBuffer.Round(time.Second),
	)
	time.Sleep(waitWithBuffer)
	return nil
}

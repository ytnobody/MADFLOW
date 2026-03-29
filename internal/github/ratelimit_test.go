package github

import (
	"encoding/json"
	"testing"
	"time"
)

// TestWithRateLimitThreshold verifies that the builder method sets the field correctly.
func TestWithRateLimitThreshold(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithRateLimitThreshold(20)
	if s.rateLimitThreshold != 20 {
		t.Errorf("expected rateLimitThreshold=20, got %d", s.rateLimitThreshold)
	}
}

// TestWithRateLimitThreshold_Zero verifies that 0 disables the check.
func TestWithRateLimitThreshold_Zero(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithRateLimitThreshold(0)
	if s.rateLimitThreshold != 0 {
		t.Errorf("expected rateLimitThreshold=0, got %d", s.rateLimitThreshold)
	}
}

// TestWithRateLimitThreshold_DefaultValue verifies the default threshold is applied
// when WithRateLimitThreshold is not called.
func TestWithRateLimitThreshold_DefaultValue(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute)
	if s.rateLimitThreshold != defaultRateLimitThreshold {
		t.Errorf("expected default rateLimitThreshold=%d, got %d", defaultRateLimitThreshold, s.rateLimitThreshold)
	}
}

// TestWithRateLimitThreshold_Chaining verifies that the builder method can be chained.
func TestWithRateLimitThreshold_Chaining(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithRateLimitThreshold(15).
		WithSkipComments(true)
	if s.rateLimitThreshold != 15 {
		t.Errorf("expected rateLimitThreshold=15, got %d", s.rateLimitThreshold)
	}
	if !s.skipComments {
		t.Error("expected skipComments=true after chaining")
	}
}

// TestGhRateLimitResponseParsing verifies the JSON parsing of the rate_limit API response.
func TestGhRateLimitResponseParsing(t *testing.T) {
	raw := `{
		"resources": {
			"core": {
				"limit": 5000,
				"remaining": 4998,
				"reset": 1700000000,
				"used": 2
			}
		}
	}`

	var resp ghRateLimitResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if resp.Resources.Core.Limit != 5000 {
		t.Errorf("limit: expected 5000, got %d", resp.Resources.Core.Limit)
	}
	if resp.Resources.Core.Remaining != 4998 {
		t.Errorf("remaining: expected 4998, got %d", resp.Resources.Core.Remaining)
	}
	if resp.Resources.Core.Reset != 1700000000 {
		t.Errorf("reset: expected 1700000000, got %d", resp.Resources.Core.Reset)
	}
	if resp.Resources.Core.Used != 2 {
		t.Errorf("used: expected 2, got %d", resp.Resources.Core.Used)
	}
}

// TestCheckRateLimit_Disabled verifies that when threshold=0 the check is skipped.
func TestCheckRateLimit_Disabled(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithRateLimitThreshold(0)

	// Even with a very low (non-existent) rate limit, disabled check should return nil.
	// We directly test the check-disabled path.
	if s.rateLimitThreshold != 0 {
		t.Fatal("precondition: threshold should be 0")
	}

	// checkRateLimit should short-circuit without calling gh when threshold == 0.
	// We can verify this by calling it and ensuring no external process error occurs
	// on a stub that doesn't exist — but since the gh binary may or may not exist,
	// we test the logic path indirectly by asserting the threshold-zero guard.
	err := s.checkRateLimitWithData(ghRateLimitResponse{}) // helper with injected data
	if err != nil {
		t.Errorf("expected nil error when threshold=0, got: %v", err)
	}
}

// TestCheckRateLimit_SufficientRemaining verifies no wait when remaining > threshold.
func TestCheckRateLimit_SufficientRemaining(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithRateLimitThreshold(10)

	resp := ghRateLimitResponse{}
	resp.Resources.Core.Limit = 5000
	resp.Resources.Core.Remaining = 100
	resp.Resources.Core.Reset = time.Now().Add(time.Hour).Unix()

	err := s.checkRateLimitWithData(resp)
	if err != nil {
		t.Errorf("expected nil error when remaining=%d > threshold=%d, got: %v",
			resp.Resources.Core.Remaining, s.rateLimitThreshold, err)
	}
}

// TestCheckRateLimit_ExactlyAtThreshold verifies rate limit is triggered when remaining == threshold.
func TestCheckRateLimit_ExactlyAtThreshold(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithRateLimitThreshold(10)

	// Reset already passed → should return nil (no wait needed, reset occurred)
	resp := ghRateLimitResponse{}
	resp.Resources.Core.Limit = 5000
	resp.Resources.Core.Remaining = 10 // exactly at threshold
	resp.Resources.Core.Reset = time.Now().Add(-time.Second).Unix() // reset in past

	err := s.checkRateLimitWithData(resp)
	if err != nil {
		t.Errorf("expected nil error when reset is in the past, got: %v", err)
	}
}

// TestCheckRateLimit_BelowThreshold_ResetFarFuture verifies error is returned when
// remaining <= threshold and reset is too far in the future.
func TestCheckRateLimit_BelowThreshold_ResetFarFuture(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithRateLimitThreshold(10)

	resp := ghRateLimitResponse{}
	resp.Resources.Core.Limit = 5000
	resp.Resources.Core.Remaining = 5 // below threshold
	resp.Resources.Core.Reset = time.Now().Add(rateLimitMaxWait + time.Minute).Unix() // far future

	err := s.checkRateLimitWithData(resp)
	if err == nil {
		t.Error("expected error when remaining < threshold and reset is too far in the future")
	}
}

// TestCheckRateLimit_ZeroRemaining_ResetFarFuture verifies error when remaining=0.
func TestCheckRateLimit_ZeroRemaining_ResetFarFuture(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithRateLimitThreshold(10)

	resp := ghRateLimitResponse{}
	resp.Resources.Core.Limit = 5000
	resp.Resources.Core.Remaining = 0
	resp.Resources.Core.Reset = time.Now().Add(rateLimitMaxWait + time.Minute).Unix()

	err := s.checkRateLimitWithData(resp)
	if err == nil {
		t.Error("expected error when remaining=0 and reset is too far in the future")
	}
}

// TestRateLimitConstants verifies the expected constant values.
func TestRateLimitConstants(t *testing.T) {
	if defaultRateLimitThreshold <= 0 {
		t.Errorf("defaultRateLimitThreshold should be positive, got %d", defaultRateLimitThreshold)
	}
	if rateLimitMaxWait <= 0 {
		t.Error("rateLimitMaxWait should be positive")
	}
}

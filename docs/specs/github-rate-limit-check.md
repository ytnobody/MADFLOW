# GitHub API Rate Limit Pre-Check

## Overview

Before making GitHub API calls, the `Syncer` checks the remaining rate limit.
If the remaining count is at or below a configurable threshold, the Syncer waits
until the rate limit resets before proceeding, or returns an error to skip the
current sync cycle.

## Design

### New Fields on `Syncer`

- `rateLimitThreshold int` — minimum remaining API calls required before
  proceeding. Default: `10`. A value of `0` disables the pre-check.

### New Builder Method

```go
func (s *Syncer) WithRateLimitThreshold(n int) *Syncer
```

Sets the rate-limit threshold. Calling with `0` disables the check.

### Rate Limit Structs

```go
// ghRateLimitResponse is the parsed JSON from `gh api rate_limit`.
type ghRateLimitResponse struct {
    Resources struct {
        Core ghRateLimitResource `json:"core"`
    } `json:"resources"`
}

type ghRateLimitResource struct {
    Limit     int   `json:"limit"`
    Remaining int   `json:"remaining"`
    Reset     int64 `json:"reset"` // Unix timestamp
    Used      int   `json:"used"`
}
```

### `checkRateLimit()` Behavior

1. If `rateLimitThreshold == 0`, return `nil` immediately (check disabled).
2. Execute `gh api rate_limit` to fetch current rate limit state.
3. Parse the JSON response into `ghRateLimitResponse`.
4. If `core.Remaining > rateLimitThreshold`, return `nil` (safe to proceed).
5. If `core.Remaining <= rateLimitThreshold`:
   - Calculate wait duration: `time.Until(time.Unix(core.Reset, 0)) + 1s buffer`
   - If wait duration ≤ 0, return `nil` (reset has already passed).
   - If wait duration > `rateLimitMaxWait` (default 10 minutes), return an
     error to skip the current sync cycle instead of waiting too long.
   - Otherwise, log a warning and wait the calculated duration.

### Integration

`checkRateLimit()` is called at the start of:
- `fetchIssues(repo string)`
- `fetchComments(repo string, issueNumber int)`

Both functions return an error if `checkRateLimit()` returns an error.

## Constants

- `defaultRateLimitThreshold = 10` — default minimum remaining calls
- `rateLimitMaxWait = 10 * time.Minute` — maximum time to wait for reset

## Edge Cases

- If `gh api rate_limit` fails (e.g. no network), the pre-check logs a warning
  and returns `nil` to allow the sync attempt to proceed (fail-open behavior).
- If the reset time is in the past, proceed immediately.
- If wait time exceeds `rateLimitMaxWait`, return an error to skip (not block).

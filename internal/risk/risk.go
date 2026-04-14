// Package risk provides risk level evaluation for pull requests.
// It determines whether a PR change is LOW, MEDIUM, or HIGH risk,
// and recommends an appropriate merge strategy accordingly.
package risk

import (
	"slices"
	"strings"
)

// Level represents the risk level of a PR change.
type Level int

const (
	// LOW risk: fully automated merge is safe.
	LOW Level = iota
	// MEDIUM risk: automated merge with post-merge confirmation note.
	MEDIUM
	// HIGH risk: human review is required before merging.
	HIGH
)

// String returns a human-readable name for the risk level.
func (l Level) String() string {
	switch l {
	case LOW:
		return "LOW"
	case MEDIUM:
		return "MEDIUM"
	case HIGH:
		return "HIGH"
	default:
		return "UNKNOWN"
	}
}

// MergeStrategy returns the recommended merge approach for the risk level.
func (l Level) MergeStrategy() string {
	switch l {
	case LOW:
		return "auto-merge"
	case MEDIUM:
		return "auto-merge with post-check"
	case HIGH:
		return "human-review-required"
	default:
		return "human-review-required"
	}
}

// PRInfo holds the metadata needed to evaluate the risk of a PR.
type PRInfo struct {
	// FilesChanged is the number of files modified in the PR.
	FilesChanged int
	// LinesAdded is the number of lines added.
	LinesAdded int
	// LinesDeleted is the number of lines deleted.
	LinesDeleted int
	// ChangedPaths contains the file paths that were changed.
	ChangedPaths []string
	// Labels contains the GitHub labels applied to the PR.
	Labels []string
}

// Evaluator evaluates the risk level of a PR.
type Evaluator interface {
	Evaluate(pr PRInfo) Level
}

// defaultEvaluator implements the risk evaluation logic described in the spec.
type defaultEvaluator struct{}

// NewEvaluator returns the default Evaluator implementation.
func NewEvaluator() Evaluator {
	return &defaultEvaluator{}
}

// Evaluate determines the risk level for the given PR metadata.
// Rules (in priority order, highest wins):
//
//   HIGH if any of:
//     - FilesChanged >= 20
//     - LinesAdded + LinesDeleted >= 500
//     - any path starts with "cmd/"
//     - any path is "go.mod" or "go.sum"
//     - any path starts with ".github/workflows/"
//     - label "high-risk" present
//
//   MEDIUM if any of:
//     - FilesChanged >= 10
//     - LinesAdded + LinesDeleted >= 200
//     - any path starts with "internal/orchestrator/" or "internal/config/"
//     - label "medium-risk" present
//
//   LOW otherwise.
func (e *defaultEvaluator) Evaluate(pr PRInfo) Level {
	if isHigh(pr) {
		return HIGH
	}
	if isMedium(pr) {
		return MEDIUM
	}
	return LOW
}

// isHigh returns true if the PR meets any HIGH-risk criterion.
func isHigh(pr PRInfo) bool {
	if pr.FilesChanged >= 20 {
		return true
	}
	if pr.LinesAdded+pr.LinesDeleted >= 500 {
		return true
	}
	for _, p := range pr.ChangedPaths {
		if strings.HasPrefix(p, "cmd/") {
			return true
		}
		if p == "go.mod" || p == "go.sum" {
			return true
		}
		if strings.HasPrefix(p, ".github/workflows/") {
			return true
		}
	}
	if slices.Contains(pr.Labels, "high-risk") {
		return true
	}
	return false
}

// isMedium returns true if the PR meets any MEDIUM-risk criterion.
func isMedium(pr PRInfo) bool {
	if pr.FilesChanged >= 10 {
		return true
	}
	if pr.LinesAdded+pr.LinesDeleted >= 200 {
		return true
	}
	for _, p := range pr.ChangedPaths {
		if strings.HasPrefix(p, "internal/orchestrator/") || strings.HasPrefix(p, "internal/config/") {
			return true
		}
	}
	if slices.Contains(pr.Labels, "medium-risk") {
		return true
	}
	return false
}

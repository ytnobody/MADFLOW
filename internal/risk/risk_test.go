package risk_test

import (
	"testing"

	"github.com/ytnobody/madflow/internal/risk"
)

func TestEvaluate_LowRisk(t *testing.T) {
	tests := []struct {
		name string
		pr   risk.PRInfo
	}{
		{
			name: "docs only change",
			pr: risk.PRInfo{
				FilesChanged:  2,
				LinesAdded:    30,
				LinesDeleted:  5,
				ChangedPaths:  []string{"docs/foo.md", "README.md"},
				Labels:        nil,
			},
		},
		{
			name: "test only change",
			pr: risk.PRInfo{
				FilesChanged:  3,
				LinesAdded:    80,
				LinesDeleted:  10,
				ChangedPaths:  []string{"internal/foo/foo_test.go", "internal/bar/bar_test.go"},
				Labels:        nil,
			},
		},
		{
			name: "small change",
			pr: risk.PRInfo{
				FilesChanged:  2,
				LinesAdded:    20,
				LinesDeleted:  5,
				ChangedPaths:  []string{"internal/foo/foo.go"},
				Labels:        nil,
			},
		},
	}

	e := risk.NewEvaluator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.Evaluate(tt.pr)
			if got != risk.LOW {
				t.Errorf("Evaluate() = %v, want LOW", got)
			}
		})
	}
}

func TestEvaluate_MediumRisk(t *testing.T) {
	tests := []struct {
		name string
		pr   risk.PRInfo
	}{
		{
			name: "many files changed",
			pr: risk.PRInfo{
				FilesChanged:  12,
				LinesAdded:    100,
				LinesDeleted:  50,
				ChangedPaths:  []string{"internal/foo/foo.go"},
				Labels:        nil,
			},
		},
		{
			name: "many lines changed",
			pr: risk.PRInfo{
				FilesChanged:  3,
				LinesAdded:    180,
				LinesDeleted:  50,
				ChangedPaths:  []string{"internal/foo/foo.go"},
				Labels:        nil,
			},
		},
		{
			name: "orchestrator change",
			pr: risk.PRInfo{
				FilesChanged:  2,
				LinesAdded:    30,
				LinesDeleted:  10,
				ChangedPaths:  []string{"internal/orchestrator/orchestrator.go"},
				Labels:        nil,
			},
		},
		{
			name: "config change",
			pr: risk.PRInfo{
				FilesChanged:  1,
				LinesAdded:    5,
				LinesDeleted:  2,
				ChangedPaths:  []string{"internal/config/config.go"},
				Labels:        nil,
			},
		},
		{
			name: "medium-risk label",
			pr: risk.PRInfo{
				FilesChanged:  1,
				LinesAdded:    5,
				LinesDeleted:  2,
				ChangedPaths:  []string{"internal/foo/foo.go"},
				Labels:        []string{"medium-risk"},
			},
		},
	}

	e := risk.NewEvaluator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.Evaluate(tt.pr)
			if got != risk.MEDIUM {
				t.Errorf("Evaluate() = %v, want MEDIUM", got)
			}
		})
	}
}

func TestEvaluate_HighRisk(t *testing.T) {
	tests := []struct {
		name string
		pr   risk.PRInfo
	}{
		{
			name: "too many files",
			pr: risk.PRInfo{
				FilesChanged:  25,
				LinesAdded:    300,
				LinesDeleted:  100,
				ChangedPaths:  []string{"internal/foo/foo.go"},
				Labels:        nil,
			},
		},
		{
			name: "too many lines",
			pr: risk.PRInfo{
				FilesChanged:  5,
				LinesAdded:    450,
				LinesDeleted:  100,
				ChangedPaths:  []string{"internal/foo/foo.go"},
				Labels:        nil,
			},
		},
		{
			name: "cmd change",
			pr: risk.PRInfo{
				FilesChanged:  2,
				LinesAdded:    30,
				LinesDeleted:  5,
				ChangedPaths:  []string{"cmd/madflow/main.go"},
				Labels:        nil,
			},
		},
		{
			name: "go.mod change",
			pr: risk.PRInfo{
				FilesChanged:  2,
				LinesAdded:    5,
				LinesDeleted:  1,
				ChangedPaths:  []string{"go.mod", "go.sum"},
				Labels:        nil,
			},
		},
		{
			name: "ci workflow change",
			pr: risk.PRInfo{
				FilesChanged:  1,
				LinesAdded:    20,
				LinesDeleted:  5,
				ChangedPaths:  []string{".github/workflows/ci.yml"},
				Labels:        nil,
			},
		},
		{
			name: "high-risk label",
			pr: risk.PRInfo{
				FilesChanged:  1,
				LinesAdded:    5,
				LinesDeleted:  2,
				ChangedPaths:  []string{"internal/foo/foo.go"},
				Labels:        []string{"high-risk"},
			},
		},
	}

	e := risk.NewEvaluator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.Evaluate(tt.pr)
			if got != risk.HIGH {
				t.Errorf("Evaluate() = %v, want HIGH", got)
			}
		})
	}
}

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level risk.Level
		want  string
	}{
		{risk.LOW, "LOW"},
		{risk.MEDIUM, "MEDIUM"},
		{risk.HIGH, "HIGH"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.level.String()
			if got != tt.want {
				t.Errorf("Level.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPRInfo_MergeStrategy(t *testing.T) {
	tests := []struct {
		level risk.Level
		want  string
	}{
		{risk.LOW, "auto-merge"},
		{risk.MEDIUM, "auto-merge with post-check"},
		{risk.HIGH, "human-review-required"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.level.MergeStrategy()
			if got != tt.want {
				t.Errorf("Level.MergeStrategy() = %q, want %q", got, tt.want)
			}
		})
	}
}

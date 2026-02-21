package agent

import (
	"errors"
	"fmt"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	cp := NewClaudeProcess(ClaudeOptions{
		SystemPrompt: "You are a test agent",
		Model:        "claude-sonnet-4-6",
		AllowedTools: []string{"Bash", "Read"},
		MaxBudgetUSD: 1.50,
	})

	args := cp.buildArgs("hello world")

	expected := map[string]bool{
		"--print":                        false,
		"--output-format":               false,
		"--system-prompt":               false,
		"--model":                        false,
		"--allowedTools":                 false,
		"--max-budget-usd":              false,
		"--dangerously-skip-permissions": false,
	}

	for _, arg := range args {
		if _, ok := expected[arg]; ok {
			expected[arg] = true
		}
	}

	for flag, found := range expected {
		if !found {
			t.Errorf("expected flag %s in args", flag)
		}
	}

	// Last arg should be the prompt
	if args[len(args)-1] != "hello world" {
		t.Errorf("expected last arg to be prompt, got %s", args[len(args)-1])
	}
}

func TestBuildArgsMinimal(t *testing.T) {
	cp := NewClaudeProcess(ClaudeOptions{})
	args := cp.buildArgs("test")

	// Should not have --system-prompt, --model, --allowedTools, --max-budget-usd
	for _, arg := range args {
		if arg == "--system-prompt" || arg == "--model" || arg == "--allowedTools" || arg == "--max-budget-usd" {
			t.Errorf("unexpected flag %s for minimal options", arg)
		}
	}

	if args[len(args)-1] != "test" {
		t.Errorf("expected last arg to be prompt, got %s", args[len(args)-1])
	}
}

func TestRateLimitError(t *testing.T) {
	inner := fmt.Errorf("quota exceeded")
	rlErr := &RateLimitError{Wrapped: inner}

	// Error() メソッドが内包エラーのメッセージを返すこと
	if rlErr.Error() != "quota exceeded" {
		t.Errorf("RateLimitError.Error() = %q, want %q", rlErr.Error(), "quota exceeded")
	}

	// Unwrap() が内包エラーを返すこと
	if rlErr.Unwrap() != inner {
		t.Errorf("RateLimitError.Unwrap() did not return wrapped error")
	}

	// errors.As が RateLimitError を検出できること
	var target *RateLimitError
	if !errors.As(rlErr, &target) {
		t.Error("errors.As should detect RateLimitError")
	}

	// IsRateLimitError が RateLimitError を検出できること
	if !IsRateLimitError(rlErr) {
		t.Error("IsRateLimitError should return true for RateLimitError")
	}

	// IsRateLimitError がラップされた RateLimitError を検出できること
	wrapped := fmt.Errorf("outer: %w", rlErr)
	if !IsRateLimitError(wrapped) {
		t.Error("IsRateLimitError should return true for wrapped RateLimitError")
	}
}

func TestIsRateLimitError_GeminiKeywords(t *testing.T) {
	tests := []struct {
		errMsg   string
		expected bool
	}{
		{"RESOURCE_EXHAUSTED: quota exceeded", true},
		{"error: ResourceExhausted", true},
		{"quota exceeded for API calls", true},
		{"rate limit exceeded", true},
		{"429 Too Many Requests", true},
		{"overloaded", true},
		{"normal error", false},
		{"connection refused", false},
	}
	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			err := fmt.Errorf("%s", tt.errMsg)
			result := IsRateLimitError(err)
			if result != tt.expected {
				t.Errorf("IsRateLimitError(%q) = %v, want %v", tt.errMsg, result, tt.expected)
			}
		})
	}
}

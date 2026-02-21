package agent

import (
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

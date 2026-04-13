package agent

import (
	"testing"
)

func TestCopilotCLIProcess_ModelName(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"copilot/claude-sonnet-4", "claude-sonnet-4"},
		{"copilot/gpt-5", "gpt-5"},
		{"copilot/claude-sonnet-4.5", "claude-sonnet-4.5"},
		{"copilot/claude-haiku-4.5", "claude-haiku-4.5"},
		{"copilot/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := NewCopilotCLIProcess(CopilotCLIOptions{Model: tt.model})
			got := p.modelName()
			if got != tt.expected {
				t.Errorf("modelName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCopilotCLIProcess_BuildArgs(t *testing.T) {
	p := NewCopilotCLIProcess(CopilotCLIOptions{
		Model: "copilot/gpt-5",
	})

	args := p.buildArgs("test prompt")

	// Verify -p flag with prompt
	found := false
	for i, arg := range args {
		if arg == "-p" && i+1 < len(args) && args[i+1] == "test prompt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected -p 'test prompt' in args: %v", args)
	}

	// Verify --allow-all-tools
	hasAllowAll := false
	for _, arg := range args {
		if arg == "--allow-all-tools" {
			hasAllowAll = true
			break
		}
	}
	if !hasAllowAll {
		t.Errorf("expected --allow-all-tools in args: %v", args)
	}

	// Verify --model
	hasModel := false
	for i, arg := range args {
		if arg == "--model" && i+1 < len(args) && args[i+1] == "gpt-5" {
			hasModel = true
			break
		}
	}
	if !hasModel {
		t.Errorf("expected --model gpt-5 in args: %v", args)
	}

	// Verify --no-color
	hasNoColor := false
	for _, arg := range args {
		if arg == "--no-color" {
			hasNoColor = true
			break
		}
	}
	if !hasNoColor {
		t.Errorf("expected --no-color in args: %v", args)
	}

	// Verify --allow-all-paths
	hasAllowPaths := false
	for _, arg := range args {
		if arg == "--allow-all-paths" {
			hasAllowPaths = true
			break
		}
	}
	if !hasAllowPaths {
		t.Errorf("expected --allow-all-paths in args: %v", args)
	}
}

func TestCopilotCLIProcess_ResetAndClose(t *testing.T) {
	p := NewCopilotCLIProcess(CopilotCLIOptions{Model: "copilot/gpt-5"})

	if err := p.Reset(nil); err != nil {
		t.Errorf("Reset() returned error: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestNewAgent_CopilotPrefix(t *testing.T) {
	agent := NewAgent(AgentConfig{
		ID:          AgentID{Role: RoleEngineer, TeamNum: 99},
		Model:       "copilot/gpt-5",
		ChatLogPath: "/dev/null",
	})

	if _, ok := agent.Process.(*CopilotCLIProcess); !ok {
		t.Errorf("expected CopilotCLIProcess for model 'copilot/gpt-5', got %T", agent.Process)
	}
}

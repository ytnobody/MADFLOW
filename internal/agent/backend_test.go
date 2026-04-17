package agent

import (
	"testing"
)

func TestParseModel(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		wantBackend Backend
		wantModelID ModelID
		wantErr     bool
	}{
		{
			name:        "test special value",
			model:       "test",
			wantBackend: BackendTest,
			wantModelID: "test",
		},
		{
			name:        "gemini prefix",
			model:       "gemini-2.5-pro",
			wantBackend: BackendGeminiAPI,
			wantModelID: "gemini-2.5-pro",
		},
		{
			name:        "gemini flash",
			model:       "gemini-2.5-flash",
			wantBackend: BackendGeminiAPI,
			wantModelID: "gemini-2.5-flash",
		},
		{
			name:        "anthropic prefix",
			model:       "anthropic/claude-opus-4-7",
			wantBackend: BackendAnthropicAPI,
			wantModelID: "anthropic/claude-opus-4-7",
		},
		{
			name:        "anthropic haiku",
			model:       "anthropic/claude-haiku-4-5",
			wantBackend: BackendAnthropicAPI,
			wantModelID: "anthropic/claude-haiku-4-5",
		},
		{
			name:        "copilot prefix",
			model:       "copilot/gpt-4o",
			wantBackend: BackendCopilotCLI,
			wantModelID: "copilot/gpt-4o",
		},
		{
			name:        "claude cli default - sonnet",
			model:       "claude-sonnet-4-6",
			wantBackend: BackendClaudeCLI,
			wantModelID: "claude-sonnet-4-6",
		},
		{
			name:        "claude cli default - haiku",
			model:       "claude-haiku-4-5",
			wantBackend: BackendClaudeCLI,
			wantModelID: "claude-haiku-4-5",
		},
		{
			name:        "claude cli default - opus",
			model:       "claude-opus-4-7",
			wantBackend: BackendClaudeCLI,
			wantModelID: "claude-opus-4-7",
		},
		{
			name:    "empty string returns error",
			model:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, modelID, err := ParseModel(tt.model)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseModel(%q): expected error, got nil", tt.model)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseModel(%q): unexpected error: %v", tt.model, err)
			}
			if backend != tt.wantBackend {
				t.Errorf("ParseModel(%q): backend = %v, want %v", tt.model, backend, tt.wantBackend)
			}
			if modelID != tt.wantModelID {
				t.Errorf("ParseModel(%q): modelID = %q, want %q", tt.model, modelID, tt.wantModelID)
			}
		})
	}
}

func TestBackendString(t *testing.T) {
	tests := []struct {
		backend Backend
		want    string
	}{
		{BackendClaudeCLI, "ClaudeCLI"},
		{BackendAnthropicAPI, "AnthropicAPI"},
		{BackendGeminiAPI, "GeminiAPI"},
		{BackendCopilotCLI, "CopilotCLI"},
		{BackendTest, "Test"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.backend.String()
			if got != tt.want {
				t.Errorf("Backend(%d).String() = %q, want %q", tt.backend, got, tt.want)
			}
		})
	}
}

func TestNewAgentUsesParseModel_Gemini(t *testing.T) {
	cfg := AgentConfig{
		ID:    AgentID{Role: RoleEngineer, TeamNum: 1},
		Model: "gemini-2.5-flash",
	}
	a := NewAgent(cfg)
	if a == nil {
		t.Fatal("NewAgent returned nil")
	}
	// Process should be a GeminiAPIProcess
	if _, ok := a.Process.(*GeminiAPIProcess); !ok {
		t.Errorf("expected GeminiAPIProcess, got %T", a.Process)
	}
}

func TestNewAgentUsesParseModel_Anthropic(t *testing.T) {
	cfg := AgentConfig{
		ID:    AgentID{Role: RoleEngineer, TeamNum: 1},
		Model: "anthropic/claude-haiku-4-5",
	}
	a := NewAgent(cfg)
	if a == nil {
		t.Fatal("NewAgent returned nil")
	}
	if _, ok := a.Process.(*AnthropicAPIProcess); !ok {
		t.Errorf("expected AnthropicAPIProcess, got %T", a.Process)
	}
}

func TestNewAgentUsesParseModel_Test(t *testing.T) {
	cfg := AgentConfig{
		ID:    AgentID{Role: RoleEngineer, TeamNum: 1},
		Model: "test",
	}
	a := NewAgent(cfg)
	if a == nil {
		t.Fatal("NewAgent returned nil")
	}
	if _, ok := a.Process.(*noopProcess); !ok {
		t.Errorf("expected noopProcess, got %T", a.Process)
	}
}

func TestNewAgentUsesParseModel_ClaudeCLIDefault(t *testing.T) {
	cfg := AgentConfig{
		ID:    AgentID{Role: RoleEngineer, TeamNum: 1},
		Model: "claude-sonnet-4-6",
	}
	a := NewAgent(cfg)
	if a == nil {
		t.Fatal("NewAgent returned nil")
	}
	if _, ok := a.Process.(*ClaudeStreamProcess); !ok {
		t.Errorf("expected ClaudeStreamProcess, got %T", a.Process)
	}
}

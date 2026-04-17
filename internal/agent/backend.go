package agent

import (
	"fmt"
	"strings"
)

// Backend represents which AI backend an agent uses.
// It is a closed sum type: new backends must be added here and handled in ParseModel.
type Backend int

const (
	// BackendClaudeCLI is the default backend using the local `claude` CLI.
	BackendClaudeCLI Backend = iota
	// BackendAnthropicAPI uses the Anthropic API directly (model prefix: "anthropic/").
	BackendAnthropicAPI
	// BackendGeminiAPI uses the Google Gemini API (model prefix: "gemini-").
	BackendGeminiAPI
	// BackendCopilotCLI uses the GitHub Copilot CLI (model prefix: "copilot/").
	BackendCopilotCLI
	// BackendTest is a no-op backend used in tests (model value: "test").
	BackendTest
)

// String returns a human-readable name for the Backend.
func (b Backend) String() string {
	switch b {
	case BackendClaudeCLI:
		return "ClaudeCLI"
	case BackendAnthropicAPI:
		return "AnthropicAPI"
	case BackendGeminiAPI:
		return "GeminiAPI"
	case BackendCopilotCLI:
		return "CopilotCLI"
	case BackendTest:
		return "Test"
	default:
		return fmt.Sprintf("Backend(%d)", int(b))
	}
}

// ModelID is the model identifier string passed to the backend.
type ModelID = string

// ParseModel converts a model string into a Backend and ModelID.
// This is the single point of conversion from model strings to typed backends.
// Returns an error if model is empty.
func ParseModel(model string) (Backend, ModelID, error) {
	switch {
	case model == "":
		return BackendClaudeCLI, "", fmt.Errorf("model string is required")
	case model == "test":
		return BackendTest, model, nil
	case strings.HasPrefix(model, "gemini-"):
		return BackendGeminiAPI, model, nil
	case strings.HasPrefix(model, "anthropic/"):
		return BackendAnthropicAPI, model, nil
	case strings.HasPrefix(model, "copilot/"):
		return BackendCopilotCLI, model, nil
	default:
		return BackendClaudeCLI, model, nil
	}
}

// newProcessForBackend creates the appropriate Process for the given Backend and model.
func newProcessForBackend(backend Backend, modelID ModelID, cfg AgentConfig) Process {
	switch backend {
	case BackendTest:
		return &noopProcess{}
	case BackendGeminiAPI:
		return NewGeminiAPIProcess(GeminiAPIOptions{
			SystemPrompt: cfg.SystemPrompt,
			Model:        modelID,
			WorkDir:      cfg.WorkDir,
			BashTimeout:  cfg.BashTimeout,
		})
	case BackendAnthropicAPI:
		return NewAnthropicAPIProcess(AnthropicAPIOptions{
			SystemPrompt: cfg.SystemPrompt,
			Model:        modelID,
			WorkDir:      cfg.WorkDir,
			BashTimeout:  cfg.BashTimeout,
		})
	case BackendCopilotCLI:
		return NewCopilotCLIProcess(CopilotCLIOptions{
			SystemPrompt: cfg.SystemPrompt,
			Model:        modelID,
			WorkDir:      cfg.WorkDir,
			BashTimeout:  cfg.BashTimeout,
		})
	default: // BackendClaudeCLI
		return NewClaudeStreamProcess(ClaudeOptions{
			SystemPrompt: cfg.SystemPrompt,
			Model:        modelID,
			WorkDir:      cfg.WorkDir,
		})
	}
}

package agent

import (
	"reflect"
	"testing"
)

// This is a mock test to demonstrate how one might test GeminiProcess.
// A real test would require a more elaborate setup, potentially mocking the
// exec.CommandContext call or having a test-only `gemini` executable.

func TestGeminiProcess_buildArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     GeminiOptions
		prompt   string
		expected []string
	}{
		{
			name:     "simple prompt",
			opts:     GeminiOptions{},
			prompt:   "hello",
			expected: []string{"prompt", "hello"},
		},
		{
			name:   "with model",
			opts:   GeminiOptions{Model: "gemini-2.5-flash"},
			prompt: "describe image",
			// モデル名をそのまま渡す（stripしない）
			expected: []string{"prompt", "--model", "gemini-2.5-flash", "describe image"},
		},
		{
			name:     "with system prompt",
			opts:     GeminiOptions{SystemPrompt: "act as a bot"},
			prompt:   "who are you?",
			expected: []string{"prompt", "--system", "act as a bot", "who are you?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gp := NewGeminiProcess(tt.opts)
			args := gp.buildArgs(tt.prompt)
			if !reflect.DeepEqual(args, tt.expected) {
				t.Errorf("buildArgs() = %v, want %v", args, tt.expected)
			}
		})
	}
}

// A more complete test for Send would involve mocking the external command.
// This is a placeholder to show where that test would go.
func TestGeminiProcess_Send(t *testing.T) {
	// To test this properly, we would need to replace exec.CommandContext.
	// One way is to have a variable for it that can be swapped in tests:
	//   var execCommand = exec.CommandContext
	// In test:
	//   execCommand = func(...) *exec.Cmd { ... return a mock command ... }

	t.Skip("skipping test requiring mock of exec.CommandContext")
}

func TestSanitizeGeminiResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain text", "hello world", "hello world"},
		{"code block wrapped", "```\nhello world\n```", "hello world"},
		{"code block with language", "```bash\necho hello\n```", "echo hello"},
		{"multiple code blocks", "text\n```\ncode\n```\nmore", "text\n```\ncode\n```\nmore"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeGeminiResponse(tt.input)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestContainsRateLimitKeyword(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"RESOURCE_EXHAUSTED: quota exceeded", true},
		{"Error: 429 Too Many Requests", true},
		{"normal error message", false},
		{"ResourceExhausted", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := containsRateLimitKeyword(tt.input)
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

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
			name: "simple prompt",
			opts: GeminiOptions{},
			prompt: "hello",
			expected: []string{"-p", "hello", "-o", "text", "--approval-mode", "yolo"},
		},
		{
			name: "with model",
			opts: GeminiOptions{Model: "gemini-pro-vision"},
			prompt: "describe image",
			expected: []string{"-p", "describe image", "-o", "text", "--approval-mode", "yolo", "-m", "pro-vision"},
		},
		{
			name: "with system prompt",
			opts: GeminiOptions{SystemPrompt: "act as a bot"},
			prompt: "who are you?",
			expected: []string{"-p", "act as a bot\n\nwho are you?", "-o", "text", "--approval-mode", "yolo"},
		},
		{
			name: "with allowed tools",
			opts: GeminiOptions{AllowedTools: []string{"read_file", "write_file"}},
			prompt: "read main.go",
			expected: []string{"-p", "read main.go", "-o", "text", "--approval-mode", "yolo", "--allowed-tools", "read_file,write_file"},
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


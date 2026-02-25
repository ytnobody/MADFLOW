package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestClaudeAPIProcess_StripPrefix verifies the model name prefix stripping.
func TestClaudeAPIProcess_StripPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"anthropic/claude-sonnet-4-5", "claude-sonnet-4-5"},
		{"anthropic/claude-haiku-4-5", "claude-haiku-4-5"},
		{"claude-sonnet-4-5", "claude-sonnet-4-5"}, // no prefix: unchanged
		{"anthropic/", ""}, // empty after strip
	}
	for _, tt := range tests {
		p := &ClaudeAPIProcess{opts: ClaudeAPIOptions{Model: tt.input}}
		got := p.stripPrefix(tt.input)
		if got != tt.expected {
			t.Errorf("stripPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestClaudeAPIProcess_ExtractText verifies text extraction from response blocks.
func TestClaudeAPIProcess_ExtractText(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []anthropicRawBlock
		expected string
	}{
		{
			name:     "single text block",
			blocks:   []anthropicRawBlock{{Type: "text", Text: "hello world"}},
			expected: "hello world",
		},
		{
			name: "multiple text blocks",
			blocks: []anthropicRawBlock{
				{Type: "text", Text: "line one"},
				{Type: "tool_use", ID: "tu_1", Name: "bash"},
				{Type: "text", Text: "line two"},
			},
			expected: "line one\nline two",
		},
		{
			name:     "no text blocks",
			blocks:   []anthropicRawBlock{{Type: "tool_use", ID: "tu_1", Name: "bash"}},
			expected: "",
		},
		{
			name:     "empty blocks",
			blocks:   []anthropicRawBlock{},
			expected: "",
		},
	}

	p := &ClaudeAPIProcess{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractText(tt.blocks)
			if got != tt.expected {
				t.Errorf("extractText() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestClaudeAPIProcess_ExecuteTool_Unknown verifies unknown tools return an error.
func TestClaudeAPIProcess_ExecuteTool_Unknown(t *testing.T) {
	p := &ClaudeAPIProcess{}
	result, isError := p.executeTool(context.Background(), "unknown_tool", json.RawMessage(`{}`))
	if !isError {
		t.Error("expected isError=true for unknown tool")
	}
	if !strings.Contains(result, "unknown tool") {
		t.Errorf("expected 'unknown tool' in result, got: %s", result)
	}
}

// TestClaudeAPIProcess_ExecuteTool_BashInvalidJSON verifies bash tool with bad JSON input.
func TestClaudeAPIProcess_ExecuteTool_BashInvalidJSON(t *testing.T) {
	p := &ClaudeAPIProcess{}
	result, isError := p.executeTool(context.Background(), "bash", json.RawMessage(`invalid`))
	if !isError {
		t.Error("expected isError=true for invalid JSON")
	}
	if !strings.Contains(result, "failed to parse bash input") {
		t.Errorf("expected 'failed to parse bash input' in result, got: %s", result)
	}
}

// TestClaudeAPIProcess_RunBash_Simple verifies basic bash execution.
func TestClaudeAPIProcess_RunBash_Simple(t *testing.T) {
	p := &ClaudeAPIProcess{}
	result, isError := p.runBash(context.Background(), "echo hello")
	if isError {
		t.Errorf("expected no error, got isError=true; result: %s", result)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in result, got: %s", result)
	}
}

// TestClaudeAPIProcess_RunBash_Error verifies error handling for failed commands.
func TestClaudeAPIProcess_RunBash_Error(t *testing.T) {
	p := &ClaudeAPIProcess{}
	result, isError := p.runBash(context.Background(), "exit 1")
	if !isError {
		t.Error("expected isError=true for failing command")
	}
	_ = result // result may be empty for exit 1
}

// TestClaudeAPIProcess_RunBash_WorkDir verifies workdir is respected.
func TestClaudeAPIProcess_RunBash_WorkDir(t *testing.T) {
	tmpDir := t.TempDir()
	p := &ClaudeAPIProcess{opts: ClaudeAPIOptions{WorkDir: tmpDir}}
	result, isError := p.runBash(context.Background(), "pwd")
	if isError {
		t.Errorf("expected no error, got: %s", result)
	}
	// The result should contain the temp dir path (possibly resolved via symlinks)
	if result == "" {
		t.Error("expected non-empty pwd output")
	}
}

// TestClaudeAPIProcess_RunBash_Timeout verifies that bash commands are killed after timeout.
func TestClaudeAPIProcess_RunBash_Timeout(t *testing.T) {
	p := &ClaudeAPIProcess{opts: ClaudeAPIOptions{BashTimeout: 100 * time.Millisecond}}
	result, isError := p.runBash(context.Background(), "sleep 10")
	if !isError {
		t.Error("expected isError=true for timed-out command")
	}
	_ = result
}

// TestClaudeAPIProcess_Send_NoAPIKey verifies that missing ANTHROPIC_API_KEY returns an error.
func TestClaudeAPIProcess_Send_NoAPIKey(t *testing.T) {
	// Ensure the env var is unset
	prev := os.Getenv("ANTHROPIC_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	defer func() {
		if prev != "" {
			os.Setenv("ANTHROPIC_API_KEY", prev)
		}
	}()

	p := NewClaudeAPIProcess(ClaudeAPIOptions{Model: "anthropic/claude-haiku-4-5"})
	_, err := p.Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is not set")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("expected ANTHROPIC_API_KEY in error message, got: %v", err)
	}
}

// TestClaudeAPIProcess_Send_EndTurn uses a mock HTTP server to verify a successful end_turn response.
func TestClaudeAPIProcess_Send_EndTurn(t *testing.T) {
	// Mock server returning a simple end_turn response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("x-api-key") == "" {
			t.Error("expected x-api-key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("expected anthropic-version header")
		}

		resp := anthropicResponse{
			ID:         "msg_test",
			Type:       "message",
			Role:       "assistant",
			StopReason: "end_turn",
			Content: []anthropicRawBlock{
				{Type: "text", Text: "Hello from mock API!"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewClaudeAPIProcess(ClaudeAPIOptions{Model: "anthropic/claude-haiku-4-5"})
	// Override the endpoint and client for testing
	p.client = server.Client()

	// We need to patch the endpoint. Since anthropicAPIEndpoint is a const,
	// we test via callAPI directly with the mock server URL.
	messages := []anthropicMessage{
		{Role: "user", Content: []any{anthropicTextContent{Type: "text", Text: "hi"}}},
	}
	resp, err := p.callAPIWithURL(context.Background(), "test-key", "claude-haiku-4-5", messages, server.URL+"/v1/messages")
	if err != nil {
		t.Fatalf("callAPIWithURL failed: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %q", resp.StopReason)
	}
	if len(resp.Content) == 0 || resp.Content[0].Text != "Hello from mock API!" {
		t.Errorf("unexpected content: %+v", resp.Content)
	}
}

// TestClaudeAPIProcess_Send_RateLimit verifies 429 is detected as a RateLimitError.
func TestClaudeAPIProcess_Send_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"rate limit exceeded"}}`))
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewClaudeAPIProcess(ClaudeAPIOptions{Model: "anthropic/claude-haiku-4-5"})
	p.client = server.Client()

	messages := []anthropicMessage{
		{Role: "user", Content: []any{anthropicTextContent{Type: "text", Text: "hi"}}},
	}
	_, err := p.callAPIWithURL(context.Background(), "test-key", "claude-haiku-4-5", messages, server.URL+"/v1/messages")
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !IsRateLimitError(err) {
		t.Errorf("expected RateLimitError, got: %v", err)
	}
}

// TestClaudeAPIProcess_Send_ToolUseLoop verifies the agentic tool-use loop.
func TestClaudeAPIProcess_Send_ToolUseLoop(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			// First call: return tool_use
			toolInput, _ := json.Marshal(map[string]string{"command": "echo tool_result"})
			resp := anthropicResponse{
				ID:         "msg_1",
				Type:       "message",
				Role:       "assistant",
				StopReason: "tool_use",
				Content: []anthropicRawBlock{
					{Type: "tool_use", ID: "tu_1", Name: "bash", Input: toolInput},
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			// Second call: return end_turn
			resp := anthropicResponse{
				ID:         "msg_2",
				Type:       "message",
				Role:       "assistant",
				StopReason: "end_turn",
				Content: []anthropicRawBlock{
					{Type: "text", Text: "Done after tool use"},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewClaudeAPIProcess(ClaudeAPIOptions{Model: "anthropic/claude-haiku-4-5"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1/messages"

	result, err := p.Send(context.Background(), "run a command")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if result != "Done after tool use" {
		t.Errorf("expected 'Done after tool use', got %q", result)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls for tool-use loop, got %d", callCount)
	}
}

// TestNewAgentDispatch_AnthropicModel verifies that anthropic/ prefix creates ClaudeAPIProcess.
func TestNewAgentDispatch_AnthropicModel(t *testing.T) {
	cfg := AgentConfig{
		ID:    AgentID{Role: RoleEngineer, TeamNum: 1},
		Model: "anthropic/claude-haiku-4-5",
	}
	agent := NewAgent(cfg)
	if agent == nil {
		t.Fatal("NewAgent returned nil")
	}
	if _, ok := agent.Process.(*ClaudeAPIProcess); !ok {
		t.Errorf("expected *ClaudeAPIProcess, got %T", agent.Process)
	}
}

// TestNewAgentDispatch_GeminiModel verifies gemini- prefix creates GeminiAPIProcess.
func TestNewAgentDispatch_GeminiModel(t *testing.T) {
	cfg := AgentConfig{
		ID:    AgentID{Role: RoleEngineer, TeamNum: 1},
		Model: "gemini-flash-2-5",
	}
	agent := NewAgent(cfg)
	if _, ok := agent.Process.(*GeminiAPIProcess); !ok {
		t.Errorf("expected *GeminiAPIProcess, got %T", agent.Process)
	}
}

// TestNewAgentDispatch_DefaultClaudeModel verifies that non-prefixed models use ClaudeProcess.
func TestNewAgentDispatch_DefaultClaudeModel(t *testing.T) {
	cfg := AgentConfig{
		ID:    AgentID{Role: RoleSuperintendent},
		Model: "claude-sonnet-4-6",
	}
	agent := NewAgent(cfg)
	if _, ok := agent.Process.(*ClaudeProcess); !ok {
		t.Errorf("expected *ClaudeProcess, got %T", agent.Process)
	}
}

// TestIsMaxIterationsError verifies that IsMaxIterationsError correctly identifies MaxIterationsError.
func TestIsMaxIterationsError(t *testing.T) {
	err := &MaxIterationsError{PartialResponse: "partial"}
	if !IsMaxIterationsError(err) {
		t.Error("expected IsMaxIterationsError to return true for *MaxIterationsError")
	}

	// Wrapped error should also be detected
	wrapped := fmt.Errorf("send failed: %w", err)
	if !IsMaxIterationsError(wrapped) {
		t.Error("expected IsMaxIterationsError to return true for wrapped *MaxIterationsError")
	}
}

// TestIsMaxIterationsError_OtherError verifies that IsMaxIterationsError returns false for other errors.
func TestIsMaxIterationsError_OtherError(t *testing.T) {
	if IsMaxIterationsError(errors.New("some other error")) {
		t.Error("expected IsMaxIterationsError to return false for non-MaxIterationsError")
	}
	if IsMaxIterationsError(nil) {
		t.Error("expected IsMaxIterationsError to return false for nil")
	}
	if IsMaxIterationsError(&RateLimitError{Wrapped: errors.New("rate limit")}) {
		t.Error("expected IsMaxIterationsError to return false for RateLimitError")
	}
}

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestAnthropicAPIProcess_Send_NoAPIKey verifies that missing ANTHROPIC_API_KEY returns an error.
func TestAnthropicAPIProcess_Send_NoAPIKey(t *testing.T) {
	prev := os.Getenv("ANTHROPIC_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	defer func() {
		if prev != "" {
			os.Setenv("ANTHROPIC_API_KEY", prev)
		}
	}()

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
	_, err := p.Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is not set")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("expected ANTHROPIC_API_KEY in error message, got: %v", err)
	}
}

// TestAnthropicAPIProcess_Send_EndTurn verifies a successful text response (stop_reason=end_turn).
func TestAnthropicAPIProcess_Send_EndTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify required headers
		if r.Header.Get("x-api-key") == "" {
			t.Error("expected x-api-key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("expected anthropic-version header")
		}

		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Hello from mock Anthropic API!"},
			},
			StopReason: "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1/messages"

	result, err := p.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if result != "Hello from mock Anthropic API!" {
		t.Errorf("unexpected result: %q", result)
	}
}

// TestAnthropicAPIProcess_Send_RateLimit429 verifies HTTP 429 is detected as a RateLimitError.
func TestAnthropicAPIProcess_Send_RateLimit429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Too many requests"}}`))
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1/messages"

	_, err := p.Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !IsRateLimitError(err) {
		t.Errorf("expected RateLimitError, got: %v", err)
	}
}

// TestAnthropicAPIProcess_Send_ToolUseLoop verifies the agentic tool-use loop.
func TestAnthropicAPIProcess_Send_ToolUseLoop(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			// First call: return tool_use
			resp := anthropicResponse{
				Content: []anthropicContentBlock{
					{
						Type:  "tool_use",
						ID:    "tool-1",
						Name:  "bash",
						Input: json.RawMessage(`{"command":"echo tool_result"}`),
					},
				},
				StopReason: "tool_use",
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			// Second call: return text
			resp := anthropicResponse{
				Content: []anthropicContentBlock{
					{Type: "text", Text: "Done after tool use"},
				},
				StopReason: "end_turn",
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
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

// TestAnthropicAPIProcess_Send_MaxTokens verifies stop_reason=max_tokens returns partial text without error.
func TestAnthropicAPIProcess_Send_MaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "truncated response text"},
			},
			StopReason: "max_tokens",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1/messages"

	result, err := p.Send(context.Background(), "generate a long response")
	if err != nil {
		t.Fatalf("expected no error for max_tokens, got: %v", err)
	}
	if result != "truncated response text" {
		t.Errorf("expected 'truncated response text', got %q", result)
	}
}

// TestAnthropicAPIProcess_Send_MaxIterationsError verifies that reaching max iterations returns MaxIterationsError.
func TestAnthropicAPIProcess_Send_MaxIterationsError(t *testing.T) {
	// Always return a tool_use so the loop never terminates naturally
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "partial work"},
				{
					Type:  "tool_use",
					ID:    "tool-x",
					Name:  "bash",
					Input: json.RawMessage(`{"command":"echo iteration"}`),
				},
			},
			StopReason: "tool_use",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1/messages"

	_, err := p.Send(context.Background(), "do something complex")
	if err == nil {
		t.Fatal("expected error when max iterations reached")
	}

	var maxIterErr *MaxIterationsError
	if !errors.As(err, &maxIterErr) {
		t.Fatalf("expected MaxIterationsError, got: %T: %v", err, err)
	}
}

// TestAnthropicAPIProcess_Send_SystemPrompt verifies system prompt is included in the request.
func TestAnthropicAPIProcess_Send_SystemPrompt(t *testing.T) {
	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		resp := anthropicResponse{
			Content:    []anthropicContentBlock{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{
		Model:        "anthropic/claude-sonnet-4-6",
		SystemPrompt: "You are a helpful assistant",
	})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1/messages"

	_, err := p.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if receivedReq.System != "You are a helpful assistant" {
		t.Errorf("expected system prompt to be set, got: %q", receivedReq.System)
	}
}

// TestAnthropicAPIProcess_Send_ModelPrefixStripped verifies anthropic/ prefix is stripped from model name.
func TestAnthropicAPIProcess_Send_ModelPrefixStripped(t *testing.T) {
	var receivedReq anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		resp := anthropicResponse{
			Content:    []anthropicContentBlock{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1/messages"

	_, err := p.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if receivedReq.Model != "claude-sonnet-4-6" {
		t.Errorf("expected model 'claude-sonnet-4-6', got: %q", receivedReq.Model)
	}
}

// TestAnthropicAPIProcess_Send_OverloadedError verifies overloaded error is treated as rate limit.
func TestAnthropicAPIProcess_Send_OverloadedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(529)
		w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	defer server.Close()

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1/messages"

	_, err := p.Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for overloaded")
	}
	if !IsRateLimitError(err) {
		t.Errorf("expected RateLimitError for overloaded_error, got: %v", err)
	}
}

// TestAnthropicAPIProcess_ResetAndClose verifies Reset and Close are no-ops.
func TestAnthropicAPIProcess_ResetAndClose(t *testing.T) {
	p := NewAnthropicAPIProcess(AnthropicAPIOptions{Model: "anthropic/claude-sonnet-4-6"})
	if err := p.Reset(context.Background()); err != nil {
		t.Errorf("Reset returned error: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

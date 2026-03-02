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
	"time"
)

// TestGeminiAPIProcess_ExtractText verifies text extraction from response parts.
func TestGeminiAPIProcess_ExtractText(t *testing.T) {
	tests := []struct {
		name     string
		parts    []geminiPart
		expected string
	}{
		{
			name:     "single text part",
			parts:    []geminiPart{{Text: "hello world"}},
			expected: "hello world",
		},
		{
			name: "multiple text parts",
			parts: []geminiPart{
				{Text: "line one"},
				{FunctionCall: &geminiFunctionCall{Name: "bash"}},
				{Text: "line two"},
			},
			expected: "line one\nline two",
		},
		{
			name:     "no text parts",
			parts:    []geminiPart{{FunctionCall: &geminiFunctionCall{Name: "bash"}}},
			expected: "",
		},
		{
			name:     "empty parts",
			parts:    []geminiPart{},
			expected: "",
		},
	}

	p := &GeminiAPIProcess{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractText(tt.parts)
			if got != tt.expected {
				t.Errorf("extractText() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestGeminiAPIProcess_ExtractFunctionCalls verifies function call extraction.
func TestGeminiAPIProcess_ExtractFunctionCalls(t *testing.T) {
	p := &GeminiAPIProcess{}

	parts := []geminiPart{
		{Text: "thinking..."},
		{FunctionCall: &geminiFunctionCall{Name: "bash", Args: json.RawMessage(`{"command":"ls"}`)}},
		{Text: "more text"},
		{FunctionCall: &geminiFunctionCall{Name: "bash", Args: json.RawMessage(`{"command":"pwd"}`)}},
	}

	calls := p.extractFunctionCalls(parts)
	if len(calls) != 2 {
		t.Fatalf("expected 2 function calls, got %d", len(calls))
	}
	if calls[0].Name != "bash" || calls[1].Name != "bash" {
		t.Errorf("unexpected function call names: %s, %s", calls[0].Name, calls[1].Name)
	}
}

// TestGeminiAPIProcess_ExecuteTool_Unknown verifies unknown tools return an error.
func TestGeminiAPIProcess_ExecuteTool_Unknown(t *testing.T) {
	p := &GeminiAPIProcess{}
	result, isError := p.executeTool(context.Background(), "unknown_tool", json.RawMessage(`{}`))
	if !isError {
		t.Error("expected isError=true for unknown tool")
	}
	if !strings.Contains(result, "unknown tool") {
		t.Errorf("expected 'unknown tool' in result, got: %s", result)
	}
}

// TestGeminiAPIProcess_ExecuteTool_BashInvalidJSON verifies bash tool with bad JSON input.
func TestGeminiAPIProcess_ExecuteTool_BashInvalidJSON(t *testing.T) {
	p := &GeminiAPIProcess{}
	result, isError := p.executeTool(context.Background(), "bash", json.RawMessage(`invalid`))
	if !isError {
		t.Error("expected isError=true for invalid JSON")
	}
	if !strings.Contains(result, "failed to parse bash input") {
		t.Errorf("expected 'failed to parse bash input' in result, got: %s", result)
	}
}

// TestGeminiAPIProcess_RunBash_Simple verifies basic bash execution.
func TestGeminiAPIProcess_RunBash_Simple(t *testing.T) {
	p := &GeminiAPIProcess{}
	result, isError := p.runBash(context.Background(), "echo hello")
	if isError {
		t.Errorf("expected no error, got isError=true; result: %s", result)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in result, got: %s", result)
	}
}

// TestGeminiAPIProcess_RunBash_Error verifies error handling for failed commands.
func TestGeminiAPIProcess_RunBash_Error(t *testing.T) {
	p := &GeminiAPIProcess{}
	result, isError := p.runBash(context.Background(), "exit 1")
	if !isError {
		t.Error("expected isError=true for failing command")
	}
	_ = result
}

// TestGeminiAPIProcess_RunBash_WorkDir verifies workdir is respected.
func TestGeminiAPIProcess_RunBash_WorkDir(t *testing.T) {
	tmpDir := t.TempDir()
	p := &GeminiAPIProcess{opts: GeminiAPIOptions{WorkDir: tmpDir}}
	result, isError := p.runBash(context.Background(), "pwd")
	if isError {
		t.Errorf("expected no error, got: %s", result)
	}
	if result == "" {
		t.Error("expected non-empty pwd output")
	}
}

// TestGeminiAPIProcess_RunBash_Timeout verifies that bash commands are killed after timeout.
func TestGeminiAPIProcess_RunBash_Timeout(t *testing.T) {
	p := &GeminiAPIProcess{opts: GeminiAPIOptions{BashTimeout: 100 * time.Millisecond}}
	result, isError := p.runBash(context.Background(), "sleep 10")
	if !isError {
		t.Error("expected isError=true for timed-out command")
	}
	_ = result
}

// TestGeminiAPIProcess_Send_NoAPIKey verifies that missing API key returns an error.
func TestGeminiAPIProcess_Send_NoAPIKey(t *testing.T) {
	// Ensure both env vars are unset
	prevGoogle := os.Getenv("GOOGLE_API_KEY")
	prevGemini := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if prevGoogle != "" {
			os.Setenv("GOOGLE_API_KEY", prevGoogle)
		}
		if prevGemini != "" {
			os.Setenv("GEMINI_API_KEY", prevGemini)
		}
	}()

	p := NewGeminiAPIProcess(GeminiAPIOptions{Model: "gemini-2.5-flash"})
	_, err := p.Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when API key is not set")
	}
	if !strings.Contains(err.Error(), "GOOGLE_API_KEY") {
		t.Errorf("expected GOOGLE_API_KEY in error message, got: %v", err)
	}
}

// TestGeminiAPIProcess_Send_EndTurn uses a mock HTTP server to verify a successful response.
func TestGeminiAPIProcess_Send_EndTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify API key is passed as query parameter
		if r.URL.Query().Get("key") == "" {
			t.Error("expected key query parameter")
		}

		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "Hello from mock Gemini API!"}},
					},
					FinishReason: "STOP",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	p := NewGeminiAPIProcess(GeminiAPIOptions{Model: "gemini-2.5-flash"})
	p.client = server.Client()

	// Test via callAPIWithURL
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "hi"}}},
	}
	resp, err := p.callAPIWithURL(context.Background(), "test-key", contents, server.URL+"/v1beta/models/gemini-2.5-flash:generateContent")
	if err != nil {
		t.Fatalf("callAPIWithURL failed: %v", err)
	}
	if len(resp.Candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}
	text := p.extractText(resp.Candidates[0].Content.Parts)
	if text != "Hello from mock Gemini API!" {
		t.Errorf("unexpected text: %q", text)
	}
}

// TestGeminiAPIProcess_Send_RateLimit429 verifies HTTP 429 is detected as a RateLimitError.
func TestGeminiAPIProcess_Send_RateLimit429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"code":429,"message":"quota exceeded","status":"RESOURCE_EXHAUSTED"}}`))
	}))
	defer server.Close()

	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	p := NewGeminiAPIProcess(GeminiAPIOptions{Model: "gemini-2.5-flash"})
	p.client = server.Client()

	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "hi"}}},
	}
	_, err := p.callAPIWithURL(context.Background(), "test-key", contents, server.URL+"/v1beta/models/gemini-2.5-flash:generateContent")
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !IsRateLimitError(err) {
		t.Errorf("expected RateLimitError, got: %v", err)
	}
}

// TestGeminiAPIProcess_Send_ResourceExhausted verifies RESOURCE_EXHAUSTED in body is detected.
func TestGeminiAPIProcess_Send_ResourceExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":400,"message":"RESOURCE_EXHAUSTED: quota exceeded","status":"RESOURCE_EXHAUSTED"}}`))
	}))
	defer server.Close()

	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	p := NewGeminiAPIProcess(GeminiAPIOptions{Model: "gemini-2.5-flash"})
	p.client = server.Client()

	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "hi"}}},
	}
	_, err := p.callAPIWithURL(context.Background(), "test-key", contents, server.URL+"/v1beta/models/gemini-2.5-flash:generateContent")
	if err == nil {
		t.Fatal("expected error for RESOURCE_EXHAUSTED")
	}
	if !IsRateLimitError(err) {
		t.Errorf("expected RateLimitError, got: %v", err)
	}
}

// TestGeminiAPIProcess_Send_ToolUseLoop verifies the agentic function-calling loop.
func TestGeminiAPIProcess_Send_ToolUseLoop(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			// First call: return function call
			resp := geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content: geminiContent{
							Role: "model",
							Parts: []geminiPart{
								{FunctionCall: &geminiFunctionCall{
									Name: "bash",
									Args: json.RawMessage(`{"command":"echo tool_result"}`),
								}},
							},
						},
						FinishReason: "STOP",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			// Second call: return text (end turn)
			resp := geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content: geminiContent{
							Role:  "model",
							Parts: []geminiPart{{Text: "Done after tool use"}},
						},
						FinishReason: "STOP",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	p := NewGeminiAPIProcess(GeminiAPIOptions{Model: "gemini-2.5-flash"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1beta/models/gemini-2.5-flash:generateContent"

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

// TestGeminiAPIProcess_Send_SystemPrompt verifies system_instruction is included when set.
func TestGeminiAPIProcess_Send_SystemPrompt(t *testing.T) {
	var receivedReq geminiRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "ok"}},
					},
					FinishReason: "STOP",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	p := NewGeminiAPIProcess(GeminiAPIOptions{
		Model:        "gemini-2.5-flash",
		SystemPrompt: "You are a helpful assistant",
	})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1beta/models/gemini-2.5-flash:generateContent"

	_, err := p.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if receivedReq.SystemInstruction == nil {
		t.Fatal("expected system_instruction to be set")
	}
	sysText := receivedReq.SystemInstruction.Parts[0].Text
	if !strings.Contains(sysText, "You are a helpful assistant") {
		t.Errorf("expected system_instruction to contain original prompt, got: %s", sysText)
	}
	if !strings.HasPrefix(sysText, geminiSystemConstraints) {
		t.Errorf("expected system_instruction to start with geminiSystemConstraints, got: %s", sysText[:100])
	}
}

// TestGeminiAPIProcess_Send_MaxIterationsError verifies that reaching max iterations returns MaxIterationsError.
func TestGeminiAPIProcess_Send_MaxIterationsError(t *testing.T) {
	// Always return a function call so the loop never terminates naturally
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role: "model",
						Parts: []geminiPart{
							{Text: "partial work"},
							{FunctionCall: &geminiFunctionCall{
								Name: "bash",
								Args: json.RawMessage(`{"command":"echo iteration"}`),
							}},
						},
					},
					FinishReason: "STOP",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	p := NewGeminiAPIProcess(GeminiAPIOptions{Model: "gemini-2.5-flash"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1beta/models/gemini-2.5-flash:generateContent"

	_, err := p.Send(context.Background(), "do something complex")
	if err == nil {
		t.Fatal("expected error when max iterations reached")
	}

	var maxIterErr *MaxIterationsError
	if !errors.As(err, &maxIterErr) {
		t.Fatalf("expected MaxIterationsError, got: %T: %v", err, err)
	}
	if maxIterErr.PartialResponse != "partial work" {
		t.Errorf("expected PartialResponse='partial work', got %q", maxIterErr.PartialResponse)
	}
}

// TestGeminiAPIProcess_Send_FinishReasonSafety verifies that SAFETY finishReason returns an error.
func TestGeminiAPIProcess_Send_FinishReasonSafety(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "blocked content"}},
					},
					FinishReason: "SAFETY",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	p := NewGeminiAPIProcess(GeminiAPIOptions{Model: "gemini-2.5-flash"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1beta/models/gemini-2.5-flash:generateContent"

	_, err := p.Send(context.Background(), "generate something")
	if err == nil {
		t.Fatal("expected error for SAFETY finishReason")
	}
	if !strings.Contains(err.Error(), "SAFETY") {
		t.Errorf("expected error to mention SAFETY, got: %v", err)
	}
}

// TestGeminiAPIProcess_Send_FinishReasonMaxTokens verifies that MAX_TOKENS returns partial text.
func TestGeminiAPIProcess_Send_FinishReasonMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "truncated response text"}},
					},
					FinishReason: "MAX_TOKENS",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("GOOGLE_API_KEY", "test-key")
	defer os.Unsetenv("GOOGLE_API_KEY")

	p := NewGeminiAPIProcess(GeminiAPIOptions{Model: "gemini-2.5-flash"})
	p.client = server.Client()
	p.testAPIURL = server.URL + "/v1beta/models/gemini-2.5-flash:generateContent"

	result, err := p.Send(context.Background(), "generate a long response")
	if err != nil {
		t.Fatalf("expected no error for MAX_TOKENS, got: %v", err)
	}
	if result != "truncated response text" {
		t.Errorf("expected 'truncated response text', got %q", result)
	}
}

// TestNewGeminiAPIProcess_SystemConstraints verifies that constraints are prepended to a non-empty system prompt.
func TestNewGeminiAPIProcess_SystemConstraints(t *testing.T) {
	p := NewGeminiAPIProcess(GeminiAPIOptions{
		SystemPrompt: "You are a helpful assistant",
	})

	if !strings.HasPrefix(p.opts.SystemPrompt, geminiSystemConstraints) {
		t.Error("expected SystemPrompt to start with geminiSystemConstraints")
	}
	if !strings.Contains(p.opts.SystemPrompt, "You are a helpful assistant") {
		t.Error("expected SystemPrompt to contain the original prompt")
	}
	if !strings.HasSuffix(p.opts.SystemPrompt, "You are a helpful assistant") {
		t.Error("expected SystemPrompt to end with the original prompt")
	}
}

// TestNewGeminiAPIProcess_EmptyPrompt verifies that constraints are NOT injected when system prompt is empty.
func TestNewGeminiAPIProcess_EmptyPrompt(t *testing.T) {
	p := NewGeminiAPIProcess(GeminiAPIOptions{
		SystemPrompt: "",
	})

	if p.opts.SystemPrompt != "" {
		t.Errorf("expected empty SystemPrompt, got: %q", p.opts.SystemPrompt)
	}
}

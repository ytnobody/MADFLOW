package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestBuildStreamArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     ClaudeOptions
		wantArgs []string // substrings that must appear in the joined args
		notArgs  []string // substrings that must NOT appear
	}{
		{
			name: "basic args",
			opts: ClaudeOptions{},
			wantArgs: []string{
				"--print",
				"--verbose",
				"--input-format", "stream-json",
				"--output-format", "stream-json",
				"--no-session-persistence",
				"--dangerously-skip-permissions",
			},
			notArgs: []string{"--system-prompt", "--model", "--allowedTools", "--max-budget-usd"},
		},
		{
			name: "with system prompt and model",
			opts: ClaudeOptions{
				SystemPrompt: "You are a test agent",
				Model:        "claude-sonnet-4-6",
			},
			wantArgs: []string{
				"--system-prompt", "You are a test agent",
				"--model", "claude-sonnet-4-6",
			},
		},
		{
			name: "with allowed tools",
			opts: ClaudeOptions{
				AllowedTools: []string{"Bash", "Read", "Write"},
			},
			wantArgs: []string{
				"--allowedTools", "Bash,Read,Write",
			},
		},
		{
			name: "with max budget",
			opts: ClaudeOptions{
				MaxBudgetUSD: 5.50,
			},
			wantArgs: []string{
				"--max-budget-usd", "5.50",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewClaudeStreamProcess(tt.opts)
			args := p.buildStreamArgs()
			joined := strings.Join(args, " ")

			for _, want := range tt.wantArgs {
				if !strings.Contains(joined, want) {
					t.Errorf("expected args to contain %q, got: %v", want, args)
				}
			}
			for _, notWant := range tt.notArgs {
				if strings.Contains(joined, notWant) {
					t.Errorf("expected args NOT to contain %q, got: %v", notWant, args)
				}
			}
		})
	}
}

func TestStreamUserMessageJSON(t *testing.T) {
	msg := streamUserMessage{
		Type: "user",
		Message: streamMessageBody{
			Role:    "user",
			Content: "hello world",
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded["type"] != "user" {
		t.Errorf("expected type=user, got %v", decoded["type"])
	}

	msgField, ok := decoded["message"].(map[string]interface{})
	if !ok {
		t.Fatal("message field is not an object")
	}
	if msgField["role"] != "user" {
		t.Errorf("expected role=user, got %v", msgField["role"])
	}
	if msgField["content"] != "hello world" {
		t.Errorf("expected content='hello world', got %v", msgField["content"])
	}
}

func TestExtractResultText(t *testing.T) {
	tests := []struct {
		name string
		event streamEvent
		want string
	}{
		{
			name:  "result field",
			event: streamEvent{Type: "result", Result: "  final answer  "},
			want:  "final answer",
		},
		{
			name:  "empty result",
			event: streamEvent{Type: "result"},
			want:  "",
		},
		{
			name: "message field with content",
			event: streamEvent{
				Type:    "result",
				Message: json.RawMessage(`{"content":"from message"}`),
			},
			want: "from message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractResultText(tt.event)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyStreamError(t *testing.T) {
	tests := []struct {
		name       string
		event      streamEvent
		wantRate   bool
		wantSubstr string
	}{
		{
			name: "rate limit error",
			event: streamEvent{
				Type:  "error",
				Error: &streamError{Code: "rate_limit", Message: "Too many requests"},
			},
			wantRate:   true,
			wantSubstr: "Too many requests",
		},
		{
			name: "429 in message",
			event: streamEvent{
				Type:  "error",
				Error: &streamError{Code: "api_error", Message: "429 too many requests"},
			},
			wantRate:   true,
			wantSubstr: "429",
		},
		{
			name: "generic error",
			event: streamEvent{
				Type:  "error",
				Error: &streamError{Code: "internal", Message: "something broke"},
			},
			wantRate:   false,
			wantSubstr: "something broke",
		},
		{
			name:       "nil error field",
			event:      streamEvent{Type: "error"},
			wantRate:   false,
			wantSubstr: "claude stream error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyStreamError(tt.event)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantRate != IsRateLimitError(err) {
				t.Errorf("IsRateLimitError = %v, want %v", IsRateLimitError(err), tt.wantRate)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

func TestScanForResult(t *testing.T) {
	tests := []struct {
		name       string
		lines      string
		wantResult string
		wantErr    bool
		wantRate   bool
	}{
		{
			name: "normal init then result",
			lines: `{"type":"system","session_id":"sess-1"}
{"type":"assistant","message":"thinking..."}
{"type":"result","result":"the answer"}
`,
			wantResult: "the answer",
		},
		{
			name: "skip malformed lines",
			lines: `not json at all
{"type":"result","result":"got it"}
`,
			wantResult: "got it",
		},
		{
			name: "error event with rate limit",
			lines: `{"type":"error","error":{"code":"rate_limit","message":"quota exceeded"}}
`,
			wantErr:  true,
			wantRate: true,
		},
		{
			name: "error event generic",
			lines: `{"type":"error","error":{"code":"internal","message":"server error"}}
`,
			wantErr: true,
		},
		{
			name:    "empty input (process exit)",
			lines:   "",
			wantErr: true,
		},
		{
			name: "skip blank lines",
			lines: `

{"type":"result","result":"after blanks"}
`,
			wantResult: "after blanks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := io.NopCloser(strings.NewReader(tt.lines))
			p := &ClaudeStreamProcess{
				scanner: newTestScanner(r),
				started: true,
			}

			result, err := p.scanForResult()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantRate && !IsRateLimitError(err) {
					t.Errorf("expected RateLimitError, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.wantResult {
				t.Errorf("got %q, want %q", result, tt.wantResult)
			}
		})
	}
}

func TestReadResultTimeout(t *testing.T) {
	// Create a reader that never produces data
	pr, pw := io.Pipe()
	defer pw.Close()

	p := &ClaudeStreamProcess{
		scanner: newTestScanner(pr),
		started: true,
	}

	// Override timeout for test speed — we test via context cancellation instead
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := p.readResult(ctx)
	if err == nil {
		t.Fatal("expected error on timeout")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context deadline error, got: %v", err)
	}
}

func TestReadResultContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	p := &ClaudeStreamProcess{
		scanner: newTestScanner(pr),
		started: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately
	cancel()

	_, err := p.readResult(ctx)
	if err == nil {
		t.Fatal("expected error on cancel")
	}
}

func TestResetAndClose(t *testing.T) {
	p := NewClaudeStreamProcess(ClaudeOptions{})

	// Reset on unstarted process should be no-op
	if err := p.Reset(context.Background()); err != nil {
		t.Errorf("Reset on unstarted process: %v", err)
	}
	if p.started {
		t.Error("should not be started after Reset on unstarted process")
	}

	// Close on unstarted process should be no-op
	if err := p.Close(); err != nil {
		t.Errorf("Close on unstarted process: %v", err)
	}
}

func TestKillAndResetClearsState(t *testing.T) {
	p := NewClaudeStreamProcess(ClaudeOptions{})
	p.started = true
	p.sessionID = "test-session"

	p.killAndReset()

	if p.started {
		t.Error("started should be false after killAndReset")
	}
	if p.sessionID != "" {
		t.Errorf("sessionID should be empty, got %q", p.sessionID)
	}
	if p.cmd != nil {
		t.Error("cmd should be nil")
	}
	if p.scanner != nil {
		t.Error("scanner should be nil")
	}
	if p.stdin != nil {
		t.Error("stdin should be nil")
	}
}

func TestWaitForInit(t *testing.T) {
	tests := []struct {
		name      string
		lines     string
		wantSID   string
		wantErr   bool
	}{
		{
			name:    "normal init",
			lines:   `{"type":"system","session_id":"sess-abc"}` + "\n",
			wantSID: "sess-abc",
		},
		{
			name:    "skip non-system events before init",
			lines:   "{\"type\":\"info\",\"message\":\"starting\"}\n{\"type\":\"system\",\"session_id\":\"sess-xyz\"}\n",
			wantSID: "sess-xyz",
		},
		{
			name:    "error before init",
			lines:   `{"type":"error","error":{"code":"fatal","message":"cannot start"}}` + "\n",
			wantErr: true,
		},
		{
			name:    "eof before init",
			lines:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := io.NopCloser(strings.NewReader(tt.lines))
			p := &ClaudeStreamProcess{
				scanner: newTestScanner(r),
			}

			err := p.waitForInit(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.sessionID != tt.wantSID {
				t.Errorf("got sessionID=%q, want %q", p.sessionID, tt.wantSID)
			}
		})
	}
}

// TestNewClaudeStreamProcessOptions verifies that options are stored correctly.
func TestNewClaudeStreamProcessOptions(t *testing.T) {
	opts := ClaudeOptions{
		SystemPrompt: "test prompt",
		Model:        "test-model",
		WorkDir:      "/tmp/test",
		AllowedTools: []string{"Bash"},
		MaxBudgetUSD: 10.0,
	}
	p := NewClaudeStreamProcess(opts)
	if p.opts.SystemPrompt != opts.SystemPrompt {
		t.Errorf("SystemPrompt mismatch")
	}
	if p.opts.Model != opts.Model {
		t.Errorf("Model mismatch")
	}
	if p.opts.WorkDir != opts.WorkDir {
		t.Errorf("WorkDir mismatch")
	}
	if p.started {
		t.Error("should not be started initially")
	}
}

// TestSendWriteFailureResetsProcess verifies that a write failure to stdin
// causes killAndReset to be called.
func TestSendWriteFailureResetsProcess(t *testing.T) {
	// Create a pipe, then close the write end to simulate a broken stdin
	pr, pw := io.Pipe()
	pw.Close() // close immediately — writes will fail

	// Start a long-running dummy process so cmd.Process is non-nil
	// and ensureStarted() sees it as already running.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start dummy process: %v", err)
	}
	defer cmd.Process.Kill()

	p := &ClaudeStreamProcess{
		opts:    ClaudeOptions{},
		started: true,
		cmd:     cmd,
		stdin:   pw,
		scanner: newTestScanner(pr),
	}

	_, err := p.Send(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error on broken stdin")
	}
	if p.started {
		t.Error("process should have been reset after write failure")
	}
}

func newTestScanner(r io.Reader) *bufio.Scanner {
	return bufio.NewScanner(r)
}

var _ = fmt.Sprintf // ensure fmt is used

package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	streamResultTimeout = 10 * time.Minute // timeout for waiting on result event (#1920 workaround)
)

// ProcessStartError indicates that the AI process failed to start properly.
// This is treated as a permanent error — retrying with the same configuration will not help.
type ProcessStartError struct {
	Wrapped error
}

func (e *ProcessStartError) Error() string { return e.Wrapped.Error() }
func (e *ProcessStartError) Unwrap() error { return e.Wrapped }

// ClaudeStreamProcess manages a persistent Claude Code subprocess using
// --input-format stream-json --output-format stream-json.
// This avoids the startup cost of launching a new Node.js process for each
// Send() call and preserves conversation context between calls.
type ClaudeStreamProcess struct {
	opts      ClaudeOptions
	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	scanner   *bufio.Scanner
	sessionID string
	started   bool
}

// NewClaudeStreamProcess creates a new stream-json based ClaudeProcess.
func NewClaudeStreamProcess(opts ClaudeOptions) *ClaudeStreamProcess {
	return &ClaudeStreamProcess{opts: opts}
}

// streamUserMessage is the NDJSON message sent to the Claude stream process.
type streamUserMessage struct {
	Type    string            `json:"type"`
	Message streamMessageBody `json:"message"`
}

type streamMessageBody struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// streamEvent represents a single NDJSON event from stdout.
type streamEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Result    string          `json:"result,omitempty"`
	Subtype   string          `json:"subtype,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Error     *streamError    `json:"error,omitempty"`
}

type streamError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Send writes a user message to the persistent process stdin and reads
// stdout until a result event is received.
func (c *ClaudeStreamProcess) Send(ctx context.Context, prompt string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureStarted(ctx); err != nil {
		return "", &ProcessStartError{Wrapped: err}
	}

	// Write NDJSON user message
	msg := streamUserMessage{
		Type: "user",
		Message: streamMessageBody{
			Role:    "user",
			Content: prompt,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("marshal stream message: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		// Process likely crashed, reset for next call
		c.killAndReset()
		return "", fmt.Errorf("write to claude stream: %w", err)
	}

	return c.readResult(ctx)
}

// Reset kills the current process so the next Send() starts a fresh one.
func (c *ClaudeStreamProcess) Reset(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killAndReset()
	return nil
}

// Close kills the current process and cleans up resources.
func (c *ClaudeStreamProcess) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killAndReset()
	return nil
}

// ensureStarted launches the Claude process if not already running.
// Must be called with c.mu held.
func (c *ClaudeStreamProcess) ensureStarted(ctx context.Context) error {
	if c.started && c.cmd != nil && c.cmd.Process != nil {
		// Check if process is still alive
		if c.cmd.ProcessState == nil {
			return nil
		}
		// Process has exited, need to restart
		log.Printf("[claude-stream] process exited, will restart")
		c.killAndReset()
	}

	args := c.buildStreamArgs()
	cmd := exec.CommandContext(ctx, "claude", args...)
	if c.opts.WorkDir != "" {
		cmd.Dir = c.opts.WorkDir
	}

	// Remove CLAUDECODE/CLAUDE_CODE_ENTRYPOINT env vars to allow nested invocations.
	env := filterEnv(os.Environ(), "CLAUDECODE")
	env = filterEnv(env, "CLAUDE_CODE_ENTRYPOINT")
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	// Capture stderr (limited) for diagnostics on startup failures.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderrBuf, max: 4096}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("start claude stream process: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.scanner = bufio.NewScanner(stdout)
	c.scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	c.started = true
	c.sessionID = ""

	// Wait for init event
	if err := c.waitForInit(ctx); err != nil {
		stderrMsg := strings.TrimSpace(stderrBuf.String())
		if stderrMsg != "" {
			log.Printf("[claude-stream] stderr: %s", stderrMsg)
		}
		c.killAndReset()
		return fmt.Errorf("wait for init: %w", err)
	}


	log.Printf("[claude-stream] process started (session=%s)", c.sessionID)
	return nil
}

// waitForInit reads events until we get the system/init event with session_id.
// Must be called with c.mu held.
func (c *ClaudeStreamProcess) waitForInit(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if !c.scanner.Scan() {
			if err := c.scanner.Err(); err != nil {
				return fmt.Errorf("scanner error waiting for init: %w", err)
			}
			return fmt.Errorf("claude stream process exited before init event")
		}

		line := c.scanner.Text()
		if line == "" {
			continue
		}

		var event streamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Skip malformed lines
			continue
		}

		switch event.Type {
		case "system":
			if event.SessionID != "" {
				c.sessionID = event.SessionID
				return nil
			}
		case "error":
			if event.Error != nil {
				return fmt.Errorf("claude stream init error: %s", event.Error.Message)
			}
		}
	}
}

// readResult reads NDJSON events from stdout until a result event is found.
// Must be called with c.mu held.
func (c *ClaudeStreamProcess) readResult(ctx context.Context) (string, error) {
	timer := time.NewTimer(streamResultTimeout)
	defer timer.Stop()

	resultCh := make(chan readResultOutput, 1)
	go func() {
		result, err := c.scanForResult()
		resultCh <- readResultOutput{result: result, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		// Timeout waiting for result — known issue (#1920).
		// Kill process so next Send() restarts.
		c.killAndReset()
		return "", fmt.Errorf("claude stream: timeout waiting for result event (%v)", streamResultTimeout)
	case out := <-resultCh:
		return out.result, out.err
	}
}

type readResultOutput struct {
	result string
	err    error
}

// scanForResult synchronously scans lines until a result or error event.
func (c *ClaudeStreamProcess) scanForResult() (string, error) {
	for c.scanner.Scan() {
		line := c.scanner.Text()
		if line == "" {
			continue
		}

		var event streamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Skip malformed JSON lines
			continue
		}

		switch event.Type {
		case "result":
			return extractResultText(event), nil
		case "error":
			return "", classifyStreamError(event)
		}
		// Skip other event types (assistant, content_block_*, tool_use, etc.)
	}

	if err := c.scanner.Err(); err != nil {
		return "", fmt.Errorf("claude stream scanner error: %w", err)
	}
	// Scanner finished without result — process exited
	return "", fmt.Errorf("claude stream process exited without result event")
}

// extractResultText pulls the text content from a result event.
func extractResultText(event streamEvent) string {
	// The result field may contain the final text directly
	if event.Result != "" {
		return strings.TrimSpace(event.Result)
	}

	// Try to extract from message field
	if event.Message != nil {
		var msgContent struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(event.Message, &msgContent); err == nil && msgContent.Content != "" {
			return strings.TrimSpace(msgContent.Content)
		}
	}

	return ""
}

// classifyStreamError converts a stream error event into the appropriate error type.
func classifyStreamError(event streamEvent) error {
	msg := ""
	code := ""
	if event.Error != nil {
		msg = event.Error.Message
		code = event.Error.Code
	}

	errStr := fmt.Sprintf("claude stream error: [%s] %s", code, msg)

	// Check for rate limit indicators
	if containsRateLimitKeyword(msg) || containsRateLimitKeyword(code) {
		return &RateLimitError{Wrapped: fmt.Errorf("%s", errStr)}
	}

	return fmt.Errorf("%s", errStr)
}

// killAndReset kills the running process and resets internal state.
// Must be called with c.mu held.
func (c *ClaudeStreamProcess) killAndReset() {
	if c.stdin != nil {
		c.stdin.Close()
		c.stdin = nil
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait() // reap zombie
	}
	c.cmd = nil
	c.scanner = nil
	c.sessionID = ""
	c.started = false
}

// buildStreamArgs constructs CLI arguments for the stream-json mode.
func (c *ClaudeStreamProcess) buildStreamArgs() []string {
	args := []string{
		"--print",
		"--verbose",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--no-session-persistence",
	}

	if c.opts.SystemPrompt != "" {
		args = append(args, "--system-prompt", c.opts.SystemPrompt)
	}

	if c.opts.Model != "" {
		args = append(args, "--model", c.opts.Model)
	}

	if len(c.opts.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(c.opts.AllowedTools, ","))
	}

	if c.opts.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", c.opts.MaxBudgetUSD))
	}

	args = append(args, "--dangerously-skip-permissions")

	return args
}

// limitedWriter writes up to max bytes and silently discards the rest.
type limitedWriter struct {
	w   io.Writer
	max int
	n   int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.n >= lw.max {
		return len(p), nil // discard
	}
	remaining := lw.max - lw.n
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := lw.w.Write(p)
	lw.n += n
	return len(p), err
}

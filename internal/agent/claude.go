package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Process is the interface for sending prompts to an AI backend.
type Process interface {
	Send(ctx context.Context, prompt string) (string, error)
	Reset(ctx context.Context) error // Reset conversation context (e.g. restart process)
	Close() error                    // Clean up resources on agent shutdown
}

// ClaudeOptions configures a Claude Code subprocess.
type ClaudeOptions struct {
	SystemPrompt string
	Model        string
	WorkDir      string
	AllowedTools []string
	MaxBudgetUSD float64
}

// ClaudeProcess manages Claude Code subprocess invocations.
// Each Send() call starts a new `claude -p` process, which autonomously
// executes tools and returns the final response.
type ClaudeProcess struct {
	opts ClaudeOptions
}

func NewClaudeProcess(opts ClaudeOptions) *ClaudeProcess {
	return &ClaudeProcess{opts: opts}
}

// Send invokes `claude -p` with the given prompt and returns the response.
// The subprocess runs to completion, executing any tools as needed.
func (c *ClaudeProcess) Send(ctx context.Context, prompt string) (string, error) {
	args := c.buildArgs(prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	if c.opts.WorkDir != "" {
		cmd.Dir = c.opts.WorkDir
	}

	// Remove CLAUDECODE env var to allow nested invocations.
	// MADFLOW intentionally spawns claude as subprocesses.
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If context was cancelled, return context error
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("claude process failed: %w\nstderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

func (c *ClaudeProcess) buildArgs(prompt string) []string {
	args := []string{
		"--print",
		"--output-format", "text",
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

	args = append(args, prompt)

	return args
}

func (c *ClaudeProcess) Reset(ctx context.Context) error { return nil }
func (c *ClaudeProcess) Close() error                    { return nil }

// RateLimitError はレート制限に抵触したことを示す専用エラー型。
type RateLimitError struct {
	Wrapped error
}

func (e *RateLimitError) Error() string {
	return e.Wrapped.Error()
}

func (e *RateLimitError) Unwrap() error {
	return e.Wrapped
}

// containsRateLimitKeyword は文字列がレート制限関連のキーワードを含むか検査する。
// Gemini CLI の stderr 出力から直接レート制限を検出するために使用する。
func containsRateLimitKeyword(s string) bool {
	lower := strings.ToLower(s)
	keywords := []string{
		"resource_exhausted",
		"quota exceeded",
		"rate limit",
		"429",
		"too many requests",
		"resourceexhausted",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// IsRateLimitError checks whether the error indicates a token/rate limit.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	// 型ベースのチェック（RateLimitError 型でラップされている場合）
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		return true
	}
	// 既存の文字列チェック（後方互換性）
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "token limit") ||
		strings.Contains(msg, "usage limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "overloaded") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "quota exceeded") ||
		strings.Contains(msg, "resourceexhausted")
}

// filterEnv returns a copy of env with the given key removed.
func filterEnv(env []string, key string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			result = append(result, e)
		}
	}
	return result
}

package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Process is the interface for sending prompts to an AI backend.
type Process interface {
	Send(ctx context.Context, prompt string) (string, error)
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

// IsRateLimitError checks whether the error indicates a token/rate limit.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "token limit") ||
		strings.Contains(msg, "usage limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "overloaded")
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

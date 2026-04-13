package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CopilotCLIOptions configures the GitHub Copilot CLI backend.
type CopilotCLIOptions struct {
	SystemPrompt string
	Model        string
	WorkDir      string
	BashTimeout  time.Duration
}

// CopilotCLIProcess sends prompts to the GitHub Copilot CLI (`copilot -p`).
// Each Send() call invokes `copilot -p "<prompt>" --allow-all-tools` as a subprocess.
type CopilotCLIProcess struct {
	opts CopilotCLIOptions
}

// NewCopilotCLIProcess creates a new CopilotCLIProcess.
func NewCopilotCLIProcess(opts CopilotCLIOptions) *CopilotCLIProcess {
	return &CopilotCLIProcess{opts: opts}
}

func (c *CopilotCLIProcess) Reset(ctx context.Context) error { return nil }
func (c *CopilotCLIProcess) Close() error                    { return nil }

// modelName returns the bare model name with the "copilot/" prefix stripped.
func (c *CopilotCLIProcess) modelName() string {
	return strings.TrimPrefix(c.opts.Model, "copilot/")
}

// Send invokes `copilot -p` with the given prompt and returns the response.
func (c *CopilotCLIProcess) Send(ctx context.Context, prompt string) (string, error) {
	// Check for authentication token
	if os.Getenv("GH_TOKEN") == "" && os.Getenv("GITHUB_TOKEN") == "" {
		// copilot CLI can also use browser-based auth, so this is only a warning-level check.
		// We proceed anyway — the CLI will prompt or fail with its own error.
	}

	if c.opts.BashTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.BashTimeout)
		defer cancel()
	}

	args := c.buildArgs(prompt)

	cmd := exec.CommandContext(ctx, "copilot", args...)
	if c.opts.WorkDir != "" {
		cmd.Dir = c.opts.WorkDir
	}

	// Pass through environment but filter out variables that may conflict.
	env := filterEnv(os.Environ(), "CLAUDECODE")
	env = filterEnv(env, "CLAUDE_CODE_ENTRYPOINT")
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		stderrStr := stderr.String()
		// Check for rate limit indicators in stderr
		if containsRateLimitKeyword(stderrStr) {
			return "", &RateLimitError{
				Wrapped: fmt.Errorf("copilot CLI rate limit: %s", strings.TrimSpace(stderrStr)),
			}
		}

		return "", fmt.Errorf("copilot CLI process failed: %w\nstderr: %s", err, stderrStr)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// buildArgs constructs the command-line arguments for the copilot CLI.
func (c *CopilotCLIProcess) buildArgs(prompt string) []string {
	args := []string{
		"-p", prompt,
		"--allow-all-tools",
		"--allow-all-paths",
		"--no-color",
	}

	model := c.modelName()
	if model != "" {
		args = append(args, "--model", model)
	}

	return args
}

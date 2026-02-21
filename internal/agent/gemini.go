package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// GeminiOptions configures a Gemini CLI subprocess.
type GeminiOptions struct {
	SystemPrompt string
	Model        string
	WorkDir      string
	AllowedTools []string
}

// GeminiProcess manages Gemini CLI subprocess invocations.
type GeminiProcess struct {
	opts GeminiOptions
}

func NewGeminiProcess(opts GeminiOptions) *GeminiProcess {
	return &GeminiProcess{opts: opts}
}

// Send invokes `gemini -p` with the given prompt and returns the response.
func (g *GeminiProcess) Send(ctx context.Context, prompt string) (string, error) {
	var response string
	var err error

	// Implement exponential backoff for rate limiting
	backoff := 1 * time.Second
	maxRetries := 5

	for i := 0; i < maxRetries; i++ {
		args := g.buildArgs(prompt)

		cmd := exec.CommandContext(ctx, "gemini", args...)
		if g.opts.WorkDir != "" {
			cmd.Dir = g.opts.WorkDir
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err = cmd.Run()
		if err == nil {
			response = strings.TrimSpace(stdout.String())
			return response, nil
		}

		// If context was cancelled, return context error
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		// Check if the error is a rate limit error
		if IsRateLimitError(err) {
			// Exponential backoff
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		// For other errors, return immediately
		return "", fmt.Errorf("gemini process failed: %w\nstderr: %s", err, stderr.String())
	}

	return "", fmt.Errorf("gemini process failed after %d retries: %w", maxRetries, err)
}

func (g *GeminiProcess) buildArgs(prompt string) []string {
	args := []string{
		"-p", prompt,
		"-o", "text",
		"--approval-mode", "yolo", // Corresponds to claude's --dangerously-skip-permissions
	}

	if g.opts.Model != "" {
		// The gemini CLI might not have a --model flag in the same way claude does.
		// Model selection is often handled via configuration or environment variables.
		// For now, we assume the model can be passed.
		// The model name for gemini should be just the model name, e.g. "gemini-pro"
		modelName := strings.TrimPrefix(g.opts.Model, "gemini-")
		if modelName != "" {
			args = append(args, "-m", modelName)
		}
	}

	if len(g.opts.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(g.opts.AllowedTools, ","))
	}
	
	// Prepend system prompt to the main prompt if it exists
    if g.opts.SystemPrompt != "" {
        prompt = g.opts.SystemPrompt + "\n\n" + prompt
        args[1] = prompt // Update the prompt in args
    }

	return args
}

// NOTE: IsRateLimitError is defined in claude.go. Since both files are in the
// same 'agent' package, GeminiProcess can use it. We'll need to make sure
// the error messages from the gemini CLI are covered.
// A more robust solution might be to have a shared error handling utility.
// For now, we assume the existing function is sufficient.

func filterEnvGemini(env []string, key string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			result = append(result, e)
		}
	}
	return result
}

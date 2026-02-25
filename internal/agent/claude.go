package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-go"
)

// Process is the interface for sending prompts to an AI backend.
type Process interface {
	Send(ctx context.Context, prompt string) (string, error)
}

// ClaudeOptions configures the Claude API client.
type ClaudeOptions struct {
	SystemPrompt string
	Model        string
}

// ClaudeProcess manages Claude API invocations.
type ClaudeProcess struct {
	opts   ClaudeOptions
	client *anthropic.Client
}

func NewClaudeProcess(opts ClaudeOptions) (*ClaudeProcess, error) {
	apiKey := os.Getenv("CLAUDE_API_KEY")
	if apiKey == "" {
		return nil, errors.New("CLAUDE_API_KEY environment variable is not set")
	}

	client, err := anthropic.NewClient(anthropic.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Claude API client: %w", err)
	}
	return &ClaudeProcess{opts: opts, client: client}, nil
}

// Send invokes Claude API with the given prompt and returns the response.
func (c *ClaudeProcess) Send(ctx context.Context, prompt string) (string, error) {
	resp, err := c.client.Messages(ctx, anthropic.MessagesRequest{
		Model: anthropic.Model(c.opts.Model),
		Messages: []anthropic.Message{
			{Role: anthropic.User, Content: prompt},
		},
		System:      c.opts.SystemPrompt,
		MaxTokens:   4096,
		Temperature: 0.7,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "rate limit") ||
			strings.Contains(strings.ToLower(err.Error()), "resource_exhausted") ||
			strings.Contains(strings.ToLower(err.Error()), "quota exceeded") {
			return "", &RateLimitError{Wrapped: err}
		}
		return "", fmt.Errorf("Claude API call failed: %w", err)
	}

	if len(resp.Content) == 0 || resp.Content[0].Text == "" {
		return "", errors.New("empty response from Claude API")
	}

	return strings.TrimSpace(resp.Content[0].Text), nil
}

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

// IsRateLimitError checks whether the error indicates a token/rate limit.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		return true
	}

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

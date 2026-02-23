package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	anthropicAPIEndpoint = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion  = "2023-06-01"
	anthropicMaxTokens   = 8096
	anthropicMaxIter     = 20 // maximum agentic loop iterations
)

// ClaudeAPIOptions configures the Anthropic API-based agent process.
type ClaudeAPIOptions struct {
	SystemPrompt string
	Model        string
	WorkDir      string
}

// ClaudeAPIProcess sends prompts to the Anthropic Messages API using ANTHROPIC_API_KEY.
// It implements an agentic loop: if the model calls tools (bash), it executes them
// and feeds the results back until the model finishes.
type ClaudeAPIProcess struct {
	opts       ClaudeAPIOptions
	client     *http.Client
	testAPIURL string // overrides anthropicAPIEndpoint in tests
}

// NewClaudeAPIProcess creates a new ClaudeAPIProcess.
func NewClaudeAPIProcess(opts ClaudeAPIOptions) *ClaudeAPIProcess {
	return &ClaudeAPIProcess{
		opts:   opts,
		client: &http.Client{Timeout: 300 * time.Second},
	}
}

// apiURL returns the effective API endpoint URL (test override or production).
func (c *ClaudeAPIProcess) apiURL() string {
	if c.testAPIURL != "" {
		return c.testAPIURL
	}
	return anthropicAPIEndpoint
}

// --- Anthropic API types ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type anthropicTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicToolUseContent struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type anthropicToolResultContent struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"`
	Role         string               `json:"role"`
	Content      []anthropicRawBlock  `json:"content"`
	StopReason   string               `json:"stop_reason"`
	Error        *anthropicErrorBlock `json:"error,omitempty"`
}

type anthropicRawBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicErrorBlock struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

// bashInputSchema defines the input parameters for the bash tool.
var bashInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"command": map[string]any{
			"type":        "string",
			"description": "The bash command to execute",
		},
	},
	"required": []string{"command"},
}

// bashTool is the tool definition passed to the Anthropic API.
var bashTool = anthropicTool{
	Name:        "bash",
	Description: "Execute a bash command in the working directory and return stdout/stderr. Use this for all file operations, git commands, and shell tasks.",
	InputSchema: bashInputSchema,
}

// Send implements the Process interface.
// It calls the Anthropic Messages API with an agentic loop:
//  1. Send the prompt
//  2. If the model uses tools, execute them and feed results back
//  3. Repeat until stop_reason == "end_turn" or max iterations reached
func (c *ClaudeAPIProcess) Send(ctx context.Context, prompt string) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY is not set; claude-api backend requires an Anthropic API key")
	}

	model := c.stripPrefix(c.opts.Model)
	if model == "" {
		model = "claude-sonnet-4-5"
	}

	messages := []anthropicMessage{
		{
			Role:    "user",
			Content: []any{anthropicTextContent{Type: "text", Text: prompt}},
		},
	}

	for range anthropicMaxIter {
		resp, err := c.callAPI(ctx, apiKey, model, messages)
		if err != nil {
			return "", err
		}

		// Check for error in response body
		if resp.Error != nil {
			errMsg := resp.Error.Message
			if containsRateLimitKeyword(errMsg) {
				return "", &RateLimitError{Wrapped: fmt.Errorf("anthropic API rate limit: %s", errMsg)}
			}
			return "", fmt.Errorf("anthropic API error: %s", errMsg)
		}

		// Append the assistant's response to the message history
		assistantContent := make([]any, 0, len(resp.Content))
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				assistantContent = append(assistantContent, anthropicTextContent{
					Type: "text",
					Text: block.Text,
				})
			case "tool_use":
				assistantContent = append(assistantContent, anthropicToolUseContent{
					Type:  "tool_use",
					ID:    block.ID,
					Name:  block.Name,
					Input: block.Input,
				})
			}
		}
		messages = append(messages, anthropicMessage{
			Role:    "assistant",
			Content: assistantContent,
		})

		// If the model is done, extract the final text response
		if resp.StopReason != "tool_use" {
			return c.extractText(resp.Content), nil
		}

		// Process tool calls
		toolResults := make([]any, 0)
		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}
			result, isError := c.executeTool(block.Name, block.Input)
			toolResults = append(toolResults, anthropicToolResultContent{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   result,
				IsError:   isError,
			})
		}

		// Append tool results as a user message
		messages = append(messages, anthropicMessage{
			Role:    "user",
			Content: toolResults,
		})
	}

	return "", fmt.Errorf("claude-api: reached maximum iterations (%d) without completing", anthropicMaxIter)
}

// callAPI sends a single request to the Anthropic Messages API.
func (c *ClaudeAPIProcess) callAPI(ctx context.Context, apiKey, model string, messages []anthropicMessage) (*anthropicResponse, error) {
	return c.callAPIWithURL(ctx, apiKey, model, messages, c.apiURL())
}

// callAPIWithURL sends a request to the given URL. It is used directly in tests
// to point at a mock HTTP server without spawning a real API call.
func (c *ClaudeAPIProcess) callAPIWithURL(ctx context.Context, apiKey, model string, messages []anthropicMessage, url string) (*anthropicResponse, error) {
	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: anthropicMaxTokens,
		System:    c.opts.SystemPrompt,
		Messages:  messages,
		Tools:     []anthropicTool{bashTool},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("content-type", "application/json")

	httpResp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// Handle HTTP error status codes
	if httpResp.StatusCode == 429 {
		return nil, &RateLimitError{
			Wrapped: fmt.Errorf("anthropic API rate limit (HTTP 429): %s", strings.TrimSpace(string(body))),
		}
	}
	if httpResp.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic API HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(body))
	}

	return &resp, nil
}

// executeTool runs the requested tool and returns (output, isError).
func (c *ClaudeAPIProcess) executeTool(toolName string, input json.RawMessage) (string, bool) {
	switch toolName {
	case "bash":
		var params struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return fmt.Sprintf("failed to parse bash input: %v", err), true
		}
		return c.runBash(params.Command)
	default:
		return fmt.Sprintf("unknown tool: %s", toolName), true
	}
}

// runBash executes a bash command and returns (output, isError).
func (c *ClaudeAPIProcess) runBash(command string) (string, bool) {
	cmd := exec.Command("bash", "-c", command)
	if c.opts.WorkDir != "" {
		cmd.Dir = c.opts.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := strings.TrimSpace(stdout.String())
	if stderr.Len() > 0 {
		if result != "" {
			result += "\n"
		}
		result += "STDERR:\n" + strings.TrimSpace(stderr.String())
	}
	if result == "" && err != nil {
		result = fmt.Sprintf("command failed: %v", err)
	}

	return result, err != nil
}

// extractText collects all text blocks from the response content.
func (c *ClaudeAPIProcess) extractText(blocks []anthropicRawBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// stripPrefix removes the "anthropic/" prefix from model names.
// e.g. "anthropic/claude-sonnet-4-5" → "claude-sonnet-4-5"
func (c *ClaudeAPIProcess) stripPrefix(model string) string {
	return strings.TrimPrefix(model, "anthropic/")
}

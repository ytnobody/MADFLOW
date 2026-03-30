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
	anthropicVersion     = "2023-06-01"
	anthropicMaxTokens   = 8096
	anthropicMaxIter     = 25 // maximum agentic loop iterations
)

// AnthropicAPIOptions configures the Anthropic API-based agent process.
type AnthropicAPIOptions struct {
	SystemPrompt string
	Model        string
	WorkDir      string
	BashTimeout  time.Duration
}

// AnthropicAPIProcess sends prompts to the Anthropic Messages API using ANTHROPIC_API_KEY.
// It implements an agentic loop: if the model calls tools (bash), it executes them
// and feeds the results back until the model finishes.
type AnthropicAPIProcess struct {
	opts       AnthropicAPIOptions
	client     *http.Client
	testAPIURL string // overrides the endpoint in tests
}

// NewAnthropicAPIProcess creates a new AnthropicAPIProcess.
func NewAnthropicAPIProcess(opts AnthropicAPIOptions) *AnthropicAPIProcess {
	return &AnthropicAPIProcess{
		opts:   opts,
		client: &http.Client{Timeout: 300 * time.Second},
	}
}

func (a *AnthropicAPIProcess) Reset(ctx context.Context) error { return nil }
func (a *AnthropicAPIProcess) Close() error                    { return nil }

// apiURL returns the effective API endpoint (test override or production).
func (a *AnthropicAPIProcess) apiURL() string {
	if a.testAPIURL != "" {
		return a.testAPIURL
	}
	return anthropicAPIEndpoint
}

// modelName returns the bare model name with the "anthropic/" prefix stripped.
func (a *AnthropicAPIProcess) modelName() string {
	return strings.TrimPrefix(a.opts.Model, "anthropic/")
}

// --- Anthropic API request/response types ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// For tool_result blocks
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Error      *anthropicError         `json:"error,omitempty"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// anthropicBashTool is the tool declaration for the bash tool.
var anthropicBashTool = anthropicTool{
	Name:        "bash",
	Description: "Execute a bash command in the working directory and return stdout/stderr. Use this for all file operations, git commands, and shell tasks.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The bash command to execute",
			},
		},
		"required": []string{"command"},
	},
}

// Send implements the Process interface.
// It calls the Anthropic Messages API with an agentic loop:
//  1. Send the prompt
//  2. If the model uses tool_use blocks, execute them and feed results back
//  3. Repeat until stop_reason is end_turn/max_tokens or max iterations reached
func (a *AnthropicAPIProcess) Send(ctx context.Context, prompt string) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY is not set; claude-api backend requires an Anthropic API key")
	}

	messages := []anthropicMessage{
		{Role: "user", Content: prompt},
	}

	var lastText string
	for range anthropicMaxIter {
		resp, err := a.callAPI(ctx, apiKey, messages)
		if err != nil {
			return "", err
		}

		// Check for API-level error in the response body
		if resp.Error != nil {
			errMsg := resp.Error.Message
			errType := resp.Error.Type
			if strings.Contains(errType, "rate_limit") || strings.Contains(errType, "overloaded") ||
				containsRateLimitKeyword(errMsg) {
				return "", &RateLimitError{Wrapped: fmt.Errorf("anthropic API rate limit: %s", errMsg)}
			}
			return "", fmt.Errorf("anthropic API error (%s): %s", errType, errMsg)
		}

		// Track last text from this response
		if text := a.extractText(resp.Content); text != "" {
			lastText = text
		}

		// Handle stop reasons
		switch resp.StopReason {
		case "max_tokens":
			return lastText, nil
		case "end_turn":
			return a.extractText(resp.Content), nil
		}

		// Check for tool_use blocks
		toolUses := a.extractToolUses(resp.Content)
		if len(toolUses) == 0 {
			// No tool calls, return text
			return a.extractText(resp.Content), nil
		}

		// Append assistant message with the full content
		messages = append(messages, anthropicMessage{
			Role:    "assistant",
			Content: resp.Content,
		})

		// Execute tools and collect results
		var toolResults []anthropicContentBlock
		for _, tu := range toolUses {
			result, _ := a.executeTool(ctx, tu.Name, tu.Input)
			toolResults = append(toolResults, anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   result,
			})
		}

		// Append tool results as user message
		messages = append(messages, anthropicMessage{
			Role:    "user",
			Content: toolResults,
		})
	}

	return "", &MaxIterationsError{PartialResponse: lastText}
}

// callAPI sends a single request to the Anthropic Messages API.
func (a *AnthropicAPIProcess) callAPI(ctx context.Context, apiKey string, messages []anthropicMessage) (*anthropicResponse, error) {
	reqBody := anthropicRequest{
		Model:     a.modelName(),
		MaxTokens: anthropicMaxTokens,
		Messages:  messages,
		Tools:     []anthropicTool{anthropicBashTool},
	}
	if a.opts.SystemPrompt != "" {
		reqBody.System = a.opts.SystemPrompt
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL(), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	httpResp, err := a.client.Do(req)
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

	// Handle HTTP-level rate limit errors
	if httpResp.StatusCode == 429 || httpResp.StatusCode == 529 {
		return nil, &RateLimitError{
			Wrapped: fmt.Errorf("anthropic API rate limit (HTTP %d): %s", httpResp.StatusCode, strings.TrimSpace(string(body))),
		}
	}
	if httpResp.StatusCode >= 400 {
		bodyStr := string(body)
		if containsRateLimitKeyword(bodyStr) || strings.Contains(bodyStr, "overloaded") {
			return nil, &RateLimitError{
				Wrapped: fmt.Errorf("anthropic API rate limit (HTTP %d): %s", httpResp.StatusCode, strings.TrimSpace(bodyStr)),
			}
		}
		return nil, fmt.Errorf("anthropic API HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(bodyStr))
	}

	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(body))
	}

	return &resp, nil
}

// extractText collects all text blocks from the response content.
func (a *AnthropicAPIProcess) extractText(content []anthropicContentBlock) string {
	var texts []string
	for _, block := range content {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

// extractToolUses returns all tool_use blocks from the response content.
func (a *AnthropicAPIProcess) extractToolUses(content []anthropicContentBlock) []anthropicContentBlock {
	var uses []anthropicContentBlock
	for _, block := range content {
		if block.Type == "tool_use" {
			uses = append(uses, block)
		}
	}
	return uses
}

// executeTool runs the requested tool and returns (output, isError).
func (a *AnthropicAPIProcess) executeTool(ctx context.Context, toolName string, input json.RawMessage) (string, bool) {
	switch toolName {
	case "bash":
		var params struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return fmt.Sprintf("failed to parse bash input: %v", err), true
		}
		return a.runBash(ctx, params.Command)
	default:
		return fmt.Sprintf("unknown tool: %s", toolName), true
	}
}

// runBash executes a bash command and returns (output, isError).
func (a *AnthropicAPIProcess) runBash(ctx context.Context, command string) (string, bool) {
	if a.opts.BashTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.opts.BashTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if a.opts.WorkDir != "" {
		cmd.Dir = a.opts.WorkDir
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

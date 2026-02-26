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
	geminiAPIEndpointFmt = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"
	geminiMaxTokens      = 65536 // Gemini 2.5 Pro/Flash support up to 64K-65.5K output tokens
	geminiMaxIter        = 25    // maximum agentic loop iterations (same as gmn default max-turns)
)

// MaxIterationsError indicates that the agentic loop reached the iteration limit
// without the model finishing naturally. It may carry a partial response text.
type MaxIterationsError struct {
	PartialResponse string
}

func (e *MaxIterationsError) Error() string {
	return fmt.Sprintf("gemini-api: reached maximum iterations (%d) without completing", geminiMaxIter)
}

// GeminiAPIOptions configures the Gemini API-based agent process.
type GeminiAPIOptions struct {
	SystemPrompt string
	Model        string
	WorkDir      string
	BashTimeout  time.Duration
}

// GeminiAPIProcess sends prompts to the Gemini REST API using GOOGLE_API_KEY / GEMINI_API_KEY.
// It implements an agentic loop: if the model calls tools (bash), it executes them
// and feeds the results back until the model finishes.
type GeminiAPIProcess struct {
	opts       GeminiAPIOptions
	client     *http.Client
	testAPIURL string // overrides the endpoint in tests
}

// geminiSystemConstraints is prepended to the system prompt for Gemini models
// to enforce strict tool-use discipline and prevent verbose text-only responses.
const geminiSystemConstraints = `## 厳守事項（最優先ルール）

1. **全アクションは bash ツール経由**: ファイル操作、git コマンド、チャットログ書き込み等、すべての操作は bash ツールコールで実行せよ。テキスト出力で代替してはならない。
2. **チャットログへの書き込み**: 必ず bash ツールで echo コマンドを実行せよ。テキスト出力としてチャットログ形式のメッセージを返してはならない。
3. **簡潔な応答**: テキスト応答は最小限にせよ。分析・計画・状況報告はすべて bash ツールでチャットログに書き込め。
4. **思考プロセスの構造化**: 複雑な判断が必要な場合、まず bash ツールで情報を収集し、その結果に基づいて次のアクションを決定せよ。
5. **1ターン1アクション**: 各応答では具体的なアクション（bash ツールコール）を1つ以上実行せよ。テキストのみの応答は禁止。
6. **指示の厳守**: システムプロンプトの指示から逸脱してはならない。独自判断で指示にない作業を行ってはならない。

`

// NewGeminiAPIProcess creates a new GeminiAPIProcess.
func NewGeminiAPIProcess(opts GeminiAPIOptions) *GeminiAPIProcess {
	if opts.SystemPrompt != "" {
		opts.SystemPrompt = geminiSystemConstraints + opts.SystemPrompt
	}
	return &GeminiAPIProcess{
		opts:   opts,
		client: &http.Client{Timeout: 300 * time.Second},
	}
}

// apiURL returns the effective API endpoint URL (test override or production).
func (g *GeminiAPIProcess) apiURL(model string) string {
	if g.testAPIURL != "" {
		return g.testAPIURL
	}
	return fmt.Sprintf(geminiAPIEndpointFmt, model)
}

// --- Gemini API types ---

type geminiRequest struct {
	SystemInstruction *geminiContent   `json:"system_instruction,omitempty"`
	Contents          []geminiContent  `json:"contents"`
	Tools             []geminiToolDecl `json:"tools,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generation_config,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResp `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiFunctionResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiToolDecl struct {
	FunctionDeclarations []geminiFuncDecl `json:"function_declarations"`
}

type geminiFuncDecl struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type geminiGenConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// geminiBashTool is the function declaration for the bash tool.
var geminiBashTool = geminiToolDecl{
	FunctionDeclarations: []geminiFuncDecl{
		{
			Name:        "bash",
			Description: "Execute a bash command in the working directory and return stdout/stderr. Use this for all file operations, git commands, and shell tasks.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The bash command to execute",
					},
				},
				"required": []string{"command"},
			},
		},
	},
}

// Send implements the Process interface.
// It calls the Gemini generateContent API with an agentic loop:
//  1. Send the prompt
//  2. If the model uses function calls, execute them and feed results back
//  3. Repeat until no more function calls or max iterations reached
func (g *GeminiAPIProcess) Send(ctx context.Context, prompt string) (string, error) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("GOOGLE_API_KEY (or GEMINI_API_KEY) is not set; gemini-api backend requires a Google API key")
	}

	model := g.opts.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	contents := []geminiContent{
		{
			Role:  "user",
			Parts: []geminiPart{{Text: prompt}},
		},
	}

	var lastText string
	for range geminiMaxIter {
		resp, err := g.callAPI(ctx, apiKey, model, contents)
		if err != nil {
			return "", err
		}

		// Check for API-level error
		if resp.Error != nil {
			errMsg := resp.Error.Message
			if resp.Error.Code == 429 || containsRateLimitKeyword(errMsg) || containsRateLimitKeyword(resp.Error.Status) {
				return "", &RateLimitError{Wrapped: fmt.Errorf("gemini API rate limit: %s", errMsg)}
			}
			return "", fmt.Errorf("gemini API error: %s", errMsg)
		}

		if len(resp.Candidates) == 0 {
			return "", fmt.Errorf("gemini API returned no candidates")
		}

		candidate := resp.Candidates[0]

		// Check finishReason for content filtering
		switch candidate.FinishReason {
		case "SAFETY", "RECITATION", "PROHIBITED_CONTENT":
			text := g.extractText(candidate.Content.Parts)
//			log.Printf("[gemini-api] response blocked by finishReason=%s", candidate.FinishReason)
			return text, fmt.Errorf("gemini-api: response blocked (finishReason=%s)", candidate.FinishReason)
		case "MAX_TOKENS":
			text := g.extractText(candidate.Content.Parts)
			if text != "" {
				lastText = text
			}
			funcCalls := g.extractFunctionCalls(candidate.Content.Parts)
			if len(funcCalls) > 0 {
//				log.Printf("[gemini-api] MAX_TOKENS with incomplete tool calls, returning partial text")
			}
			// Return whatever text we got; tool calls may be truncated
			return lastText, nil
		}

		// Append the model's response to conversation history
		contents = append(contents, geminiContent{
			Role:  "model",
			Parts: candidate.Content.Parts,
		})

		// Track last text for MaxIterationsError
		if text := g.extractText(candidate.Content.Parts); text != "" {
			lastText = text
		}

		// Check for function calls
		funcCalls := g.extractFunctionCalls(candidate.Content.Parts)
		if len(funcCalls) == 0 {
			// No function calls — extract text and return
			return g.extractText(candidate.Content.Parts), nil
		}

		// Execute function calls and build response parts
		var responseParts []geminiPart
		for _, fc := range funcCalls {
			result, isError := g.executeTool(ctx, fc.Name, fc.Args)
			respMap := map[string]any{"output": result}
			if isError {
				respMap["error"] = true
			}
			responseParts = append(responseParts, geminiPart{
				FunctionResponse: &geminiFunctionResp{
					Name:     fc.Name,
					Response: respMap,
				},
			})
		}

		// Append function responses as user turn
		contents = append(contents, geminiContent{
			Role:  "user",
			Parts: responseParts,
		})
	}

	return "", &MaxIterationsError{PartialResponse: lastText}
}

// callAPI sends a single request to the Gemini generateContent API.
func (g *GeminiAPIProcess) callAPI(ctx context.Context, apiKey, model string, contents []geminiContent) (*geminiResponse, error) {
	return g.callAPIWithURL(ctx, apiKey, contents, g.apiURL(model))
}

// callAPIWithURL sends a request to the given URL. Used in tests to point at a mock server.
func (g *GeminiAPIProcess) callAPIWithURL(ctx context.Context, apiKey string, contents []geminiContent, url string) (*geminiResponse, error) {
	reqBody := geminiRequest{
		Contents:         contents,
		Tools:            []geminiToolDecl{geminiBashTool},
		GenerationConfig: &geminiGenConfig{MaxOutputTokens: geminiMaxTokens},
	}

	if g.opts.SystemPrompt != "" {
		reqBody.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: g.opts.SystemPrompt}},
		}
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Append API key as query parameter
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	fullURL := url + sep + "key=" + apiKey

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := g.client.Do(req)
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
			Wrapped: fmt.Errorf("gemini API rate limit (HTTP 429): %s", strings.TrimSpace(string(body))),
		}
	}
	if httpResp.StatusCode >= 400 {
		// Check for RESOURCE_EXHAUSTED in error body
		bodyStr := string(body)
		if containsRateLimitKeyword(bodyStr) {
			return nil, &RateLimitError{
				Wrapped: fmt.Errorf("gemini API rate limit (HTTP %d): %s", httpResp.StatusCode, strings.TrimSpace(bodyStr)),
			}
		}
		return nil, fmt.Errorf("gemini API HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(bodyStr))
	}

	var resp geminiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(body))
	}

	return &resp, nil
}

// extractFunctionCalls extracts all function call parts from a response.
func (g *GeminiAPIProcess) extractFunctionCalls(parts []geminiPart) []*geminiFunctionCall {
	var calls []*geminiFunctionCall
	for i := range parts {
		if parts[i].FunctionCall != nil {
			calls = append(calls, parts[i].FunctionCall)
		}
	}
	return calls
}

// extractText collects all text parts from the response.
func (g *GeminiAPIProcess) extractText(parts []geminiPart) string {
	var texts []string
	for _, p := range parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

// executeTool runs the requested tool and returns (output, isError).
func (g *GeminiAPIProcess) executeTool(ctx context.Context, toolName string, input json.RawMessage) (string, bool) {
	switch toolName {
	case "bash":
		var params struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return fmt.Sprintf("failed to parse bash input: %v", err), true
		}
		return g.runBash(ctx, params.Command)
	default:
		return fmt.Sprintf("unknown tool: %s", toolName), true
	}
}

// runBash executes a bash command and returns (output, isError).
func (g *GeminiAPIProcess) runBash(ctx context.Context, command string) (string, bool) {
	if g.opts.BashTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.opts.BashTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if g.opts.WorkDir != "" {
		cmd.Dir = g.opts.WorkDir
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

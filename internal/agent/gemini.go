package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// GmnOptions configures a gmn CLI subprocess.
type GmnOptions struct {
	SystemPrompt string
	Model        string
	WorkDir      string
	AllowedTools []string
}

// GmnProcess manages gmn CLI subprocess invocations.
type GmnProcess struct {
	opts GmnOptions
}

func NewGmnProcess(opts GmnOptions) *GmnProcess {
	return &GmnProcess{opts: opts}
}

// Send invokes `gmn` with the given prompt and returns the response.
func (g *GmnProcess) Send(ctx context.Context, prompt string) (string, error) {
	args := g.buildArgs(prompt)

	cmd := exec.CommandContext(ctx, "gmn", args...)
	if g.opts.WorkDir != "" {
		cmd.Dir = g.opts.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		response := strings.TrimSpace(stdout.String())
		return response, nil
	}

	// コンテキストキャンセルの場合
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// stderr の内容もエラーに含める（rate limit 検出用）
	stderrStr := stderr.String()
	wrappedErr := fmt.Errorf("gmn process failed: %w\nstderr: %s", err, stderrStr)

	// stderr にレート制限関連の文字列がある場合、専用エラー型で返す
	if containsRateLimitKeyword(stderrStr) || IsRateLimitError(err) {
		return "", &RateLimitError{Wrapped: wrappedErr}
	}

	return "", wrappedErr
}

func (g *GmnProcess) buildArgs(prompt string) []string {
	// システムプロンプトがある場合、プロンプトの先頭に付加する
	combinedPrompt := prompt
	if g.opts.SystemPrompt != "" {
		combinedPrompt = g.opts.SystemPrompt + "\n\n---\n\n" + prompt
	}

	args := []string{
		"-p", combinedPrompt,
		"-o", "text",
		"--yolo",
	}

	// モデル名を指定
	if g.opts.Model != "" {
		args = append(args, "-m", g.opts.Model)
	}

	return args
}

// GeminiOptions configures a Gemini CLI subprocess (legacy).
type GeminiOptions struct {
	SystemPrompt string
	Model        string
	WorkDir      string
	AllowedTools []string
}

// GeminiProcess manages Gemini CLI subprocess invocations (legacy).
type GeminiProcess struct {
	opts GeminiOptions
}

func NewGeminiProcess(opts GeminiOptions) *GeminiProcess {
	return &GeminiProcess{opts: opts}
}

// Send invokes `gemini -p` with the given prompt and returns the response.
func (g *GeminiProcess) Send(ctx context.Context, prompt string) (string, error) {
	args := g.buildArgs(prompt)

	cmd := exec.CommandContext(ctx, "gemini", args...)
	if g.opts.WorkDir != "" {
		cmd.Dir = g.opts.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		response := strings.TrimSpace(stdout.String())
		response = sanitizeGeminiResponse(response)
		return response, nil
	}

	// コンテキストキャンセルの場合
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// stderr の内容もエラーに含める（rate limit 検出用）
	stderrStr := stderr.String()
	wrappedErr := fmt.Errorf("gemini process failed: %w\nstderr: %s", err, stderrStr)

	// stderr にレート制限関連の文字列がある場合、専用エラー型で返す
	if containsRateLimitKeyword(stderrStr) || IsRateLimitError(err) {
		return "", &RateLimitError{Wrapped: wrappedErr}
	}

	return "", wrappedErr
}

func (g *GeminiProcess) buildArgs(prompt string) []string {
	// システムプロンプトがある場合、プロンプトの先頭に付加する
	// （Gemini CLI には --system-prompt フラグがないため）
	combinedPrompt := prompt
	if g.opts.SystemPrompt != "" {
		combinedPrompt = g.opts.SystemPrompt + "\n\n" + prompt
	}

	args := []string{
		"-p", combinedPrompt,
		"-o", "text",
		"--approval-mode", "yolo",
	}

	// モデル名はそのまま渡す（gemini- prefix を strip しない）
	if g.opts.Model != "" {
		args = append(args, "-m", g.opts.Model)
	}

	if len(g.opts.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(g.opts.AllowedTools, ","))
	}

	return args
}

// sanitizeGeminiResponse はGeminiのレスポンスからマークダウンのコードフェンスを除去する。
func sanitizeGeminiResponse(response string) string {
	lines := strings.Split(response, "\n")

	// 全体が単一のコードブロックで包まれている場合のみ除去
	if len(lines) >= 2 &&
		strings.HasPrefix(strings.TrimSpace(lines[0]), "```") &&
		strings.TrimSpace(lines[len(lines)-1]) == "```" {
		// 先頭と末尾のフェンスを除去
		inner := lines[1 : len(lines)-1]
		return strings.TrimSpace(strings.Join(inner, "\n"))
	}
	return response
}

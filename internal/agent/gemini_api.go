package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

type GeminiAPIOptions struct {
	Client       *genai.Client
	Model        string
	SystemPrompt string
	WorkDir      string
	CacheName    string
}

type GeminiAPIProcess struct {
	client       *genai.Client
	model        string
	systemPrompt string
	workDir      string
	mu           sync.RWMutex
	cacheName    string
}

func NewGeminiAPIProcess(opts GeminiAPIOptions) *GeminiAPIProcess {
	return &GeminiAPIProcess{client: opts.Client, model: opts.Model, systemPrompt: opts.SystemPrompt, workDir: opts.WorkDir, cacheName: opts.CacheName}
}

func (g *GeminiAPIProcess) SetCacheName(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cacheName = name
}

func (g *GeminiAPIProcess) GetCacheName() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.cacheName
}

func (g *GeminiAPIProcess) Send(ctx context.Context, prompt string) (string, error) {
	g.mu.RLock()
	cacheName := g.cacheName
	systemPrompt := g.systemPrompt
	g.mu.RUnlock()

	cfg := &genai.GenerateContentConfig{}
	if cacheName != "" {
		cfg.CachedContent = cacheName
	} else if systemPrompt != "" {
		cfg.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: systemPrompt}}}
	}

	contents := []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: prompt}}}}

	backoff := 2 * time.Second
	var lastErr error
	for i := 0; i < 5; i++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, cfg)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if IsRateLimitError(err) {
				log.Printf("[gemini_api] rate limit hit, backing off for %v", backoff)
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(backoff):
				}
				backoff *= 2
				continue
			}
			return "", fmt.Errorf("gemini API error: %w", err)
		}
		return extractText(resp), nil
	}
	return "", fmt.Errorf("gemini API failed after 5 retries: %w", lastErr)
}

func extractText(resp *genai.GenerateContentResponse) string {
	if resp == nil {
		return ""
	}
	var parts []string
	for _, c := range resp.Candidates {
		if c.Content == nil {
			continue
		}
		for _, p := range c.Content.Parts {
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

package agent_test

import (
	"testing"

	"github.com/ytnobody/madflow/internal/agent"
)

func TestGeminiAPIProcess_SetGetCacheName(t *testing.T) {
	proc := agent.NewGeminiAPIProcess(agent.GeminiAPIOptions{
		Model: "gemini-2.5-flash",
	})

	if name := proc.GetCacheName(); name != "" {
		t.Errorf("expected empty cache name, got: %s", name)
	}

	proc.SetCacheName("cachedContents/test-123")
	if name := proc.GetCacheName(); name != "cachedContents/test-123" {
		t.Errorf("expected 'cachedContents/test-123', got: %s", name)
	}
}

func TestGeminiAPIProcess_NewWithOptions(t *testing.T) {
	opts := agent.GeminiAPIOptions{
		Model:        "gemini-2.5-flash",
		SystemPrompt: "You are a helpful assistant",
		WorkDir:      "/tmp",
		CacheName:    "cachedContents/initial",
	}
	proc := agent.NewGeminiAPIProcess(opts)
	if proc == nil {
		t.Fatal("NewGeminiAPIProcess returned nil")
	}
	if proc.GetCacheName() != "cachedContents/initial" {
		t.Errorf("expected initial cache name, got: %s", proc.GetCacheName())
	}
}

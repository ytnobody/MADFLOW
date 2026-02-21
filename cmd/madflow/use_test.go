package main

import (
	"testing"

	"github.com/ytnobody/madflow/internal/config"
)

func TestResolvePreset_Claude(t *testing.T) {
	models, err := resolvePreset("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if models.Superintendent != "claude-sonnet-4-6" {
		t.Errorf("claude preset: expected superintendent claude-sonnet-4-6, got %s", models.Superintendent)
	}
	if models.Engineer != "claude-sonnet-4-6" {
		t.Errorf("claude preset: expected engineer claude-sonnet-4-6, got %s", models.Engineer)
	}
}

func TestResolvePreset_Gemini(t *testing.T) {
	models, err := resolvePreset("gemini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if models.Superintendent != "gemini-2.0-flash" {
		t.Errorf("gemini preset: expected superintendent gemini-2.0-flash, got %s", models.Superintendent)
	}
	if models.Engineer != "gemini-2.0-flash" {
		t.Errorf("gemini preset: expected engineer gemini-2.0-flash, got %s", models.Engineer)
	}
}

func TestResolvePreset_Mixed(t *testing.T) {
	models, err := resolvePreset("mixed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if models.Superintendent != "claude-sonnet-4-6" {
		t.Errorf("mixed preset: expected superintendent claude-sonnet-4-6, got %s", models.Superintendent)
	}
	if models.Engineer != "gemini-2.0-flash" {
		t.Errorf("mixed preset: expected engineer gemini-2.0-flash, got %s", models.Engineer)
	}
}

func TestResolvePreset_Economy(t *testing.T) {
	models, err := resolvePreset("economy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if models.Superintendent != "claude-haiku-4-5" {
		t.Errorf("economy preset: expected superintendent claude-haiku-4-5, got %s", models.Superintendent)
	}
	if models.Engineer != "claude-haiku-4-5" {
		t.Errorf("economy preset: expected engineer claude-haiku-4-5, got %s", models.Engineer)
	}
}

func TestResolvePreset_Unknown(t *testing.T) {
	_, err := resolvePreset("unknown-backend")
	if err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}
}

func TestResolvePreset_ReturnsModelConfig(t *testing.T) {
	// Verify all presets return a non-empty ModelConfig
	presets := []string{"claude", "gemini", "mixed", "economy"}
	for _, preset := range presets {
		models, err := resolvePreset(preset)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", preset, err)
			continue
		}
		if models == (config.ModelConfig{}) {
			t.Errorf("%s: expected non-empty ModelConfig", preset)
		}
		if models.Superintendent == "" {
			t.Errorf("%s: Superintendent model is empty", preset)
		}
		if models.Engineer == "" {
			t.Errorf("%s: Engineer model is empty", preset)
		}
	}
}

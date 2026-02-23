package main

import (
	"strings"
	"testing"
)

func TestPresets_AllDefined(t *testing.T) {
	expectedPresets := []string{"claude", "gemini", "claude-cheap", "gemini-cheap", "hybrid", "hybrid-cheap"}
	for _, name := range expectedPresets {
		if _, ok := presets[name]; !ok {
			t.Errorf("preset %q is not defined", name)
		}
	}
}

func TestPresets_Models(t *testing.T) {
	tests := []struct {
		preset         string
		superintendent string
		engineer       string
	}{
		{"claude", "claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"gemini", "gemini-pro-2-5", "gemini-pro-2-5"},
		{"claude-cheap", "claude-sonnet-4-6", "claude-haiku-4-5"},
		{"gemini-cheap", "gemini-flash-2-5", "gemini-flash-2-5"},
		{"hybrid", "claude-sonnet-4-6", "gemini-pro-2-5"},
		{"hybrid-cheap", "claude-sonnet-4-6", "gemini-flash-2-5"},
	}

	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			m, ok := presets[tt.preset]
			if !ok {
				t.Fatalf("preset %q not found", tt.preset)
			}
			if m.Superintendent != tt.superintendent {
				t.Errorf("superintendent: got %q, want %q", m.Superintendent, tt.superintendent)
			}
			if m.Engineer != tt.engineer {
				t.Errorf("engineer: got %q, want %q", m.Engineer, tt.engineer)
			}
		})
	}
}

func TestUpdateModelsSection(t *testing.T) {
	input := `[project]
name = "test"

[agent]
context_reset_minutes = 8

[agent.models]
superintendent = "claude-opus-4-6"
engineer = "claude-sonnet-4-6"

[branches]
main = "main"
`

	t.Run("switch to gemini preset", func(t *testing.T) {
		result, err := updateModelsSection(input, "gemini-pro-2-5", "gemini-pro-2-5")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, `superintendent = "gemini-pro-2-5"`) {
			t.Errorf("superintendent not updated: %s", result)
		}
		if !strings.Contains(result, `engineer = "gemini-pro-2-5"`) {
			t.Errorf("engineer not updated: %s", result)
		}
		// Other sections must be preserved.
		if !strings.Contains(result, `[project]`) {
			t.Error("project section lost")
		}
		if !strings.Contains(result, `[branches]`) {
			t.Error("branches section lost")
		}
	})

	t.Run("switch to hybrid preset", func(t *testing.T) {
		result, err := updateModelsSection(input, "claude-sonnet-4-6", "gemini-pro-2-5")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, `superintendent = "claude-sonnet-4-6"`) {
			t.Errorf("superintendent not updated: %s", result)
		}
		if !strings.Contains(result, `engineer = "gemini-pro-2-5"`) {
			t.Errorf("engineer not updated: %s", result)
		}
	})

	t.Run("switch to claude-cheap preset", func(t *testing.T) {
		result, err := updateModelsSection(input, "claude-sonnet-4-6", "claude-haiku-4-5")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, `superintendent = "claude-sonnet-4-6"`) {
			t.Errorf("superintendent not updated: %s", result)
		}
		if !strings.Contains(result, `engineer = "claude-haiku-4-5"`) {
			t.Errorf("engineer not updated: %s", result)
		}
	})
}

func TestUpdateModelsSection_MissingKeys(t *testing.T) {
	// Config with no superintendent key.
	inputMissingSuper := `[agent.models]
engineer = "claude-sonnet-4-6"
`
	_, err := updateModelsSection(inputMissingSuper, "gemini-pro-2-5", "gemini-pro-2-5")
	if err == nil {
		t.Error("expected error for missing superintendent key, got nil")
	}

	// Config with no engineer key.
	inputMissingEng := `[agent.models]
superintendent = "claude-sonnet-4-6"
`
	_, err = updateModelsSection(inputMissingEng, "gemini-pro-2-5", "gemini-pro-2-5")
	if err == nil {
		t.Error("expected error for missing engineer key, got nil")
	}
}

func TestFormatPresets(t *testing.T) {
	out := formatPresets()
	expectedPresets := []string{"claude", "gemini", "claude-cheap", "gemini-cheap", "hybrid", "hybrid-cheap"}
	for _, name := range expectedPresets {
		if !strings.Contains(out, name) {
			t.Errorf("formatPresets output missing preset %q", name)
		}
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ytnobody/madflow/internal/project"
)

// presetModels defines the model configuration for each preset name.
type presetModels struct {
	Superintendent string
	Engineer       string
}

// presets maps preset name → model configuration.
var presets = map[string]presetModels{
	"claude": {
		Superintendent: "claude-sonnet-4-6",
		Engineer:       "claude-sonnet-4-6",
	},
	"gemini": {
		Superintendent: "gemini-2.5-pro",
		Engineer:       "gemini-2.5-pro",
	},
	"claude-cheap": {
		Superintendent: "claude-sonnet-4-6",
		Engineer:       "claude-haiku-4-5",
	},
	"gemini-cheap": {
		Superintendent: "gemini-2.5-flash",
		Engineer:       "gemini-2.5-flash",
	},
	"hybrid": {
		Superintendent: "claude-sonnet-4-6",
		Engineer:       "gemini-2.5-pro",
	},
	"hybrid-cheap": {
		Superintendent: "claude-sonnet-4-6",
		Engineer:       "gemini-2.5-flash",
	},
	// Anthropic API key-based presets (require ANTHROPIC_API_KEY)
	"claude-api-standard": {
		Superintendent: "anthropic/claude-sonnet-4-6",
		Engineer:       "anthropic/claude-haiku-4-5",
	},
	"claude-api-cheap": {
		Superintendent: "anthropic/claude-haiku-4-5",
		Engineer:       "anthropic/claude-haiku-4-5",
	},
}

// cmdUse switches the active model preset in madflow.toml.
func cmdUse(preset string) error {
	if preset == "" {
		return fmt.Errorf("usage: madflow use <preset>\n\nAvailable presets:\n%s", formatPresets())
	}

	models, ok := presets[preset]
	if !ok {
		return fmt.Errorf("unknown preset %q\n\nAvailable presets:\n%s", preset, formatPresets())
	}

	configPath, err := findConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	updated, err := updateModelsSection(string(data), models.Superintendent, models.Engineer)
	if err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	if err := os.WriteFile(configPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("Switched to preset %q:\n  superintendent = %q\n  engineer       = %q\n", preset, models.Superintendent, models.Engineer)
	fmt.Printf("Config updated: %s\n", configPath)
	return nil
}

// updateModelsSection replaces the superintendent and engineer model values
// in the [agent.models] section of a TOML config string.
// It preserves all other content (comments, ordering, unrelated keys).
func updateModelsSection(content, superintendent, engineer string) (string, error) {
	reSuper := regexp.MustCompile(`(?m)^(\s*superintendent\s*=\s*)".+"`)
	reEng := regexp.MustCompile(`(?m)^(\s*engineer\s*=\s*)".+"`)

	result := reSuper.ReplaceAllString(content, `${1}"`+superintendent+`"`)
	result = reEng.ReplaceAllString(result, `${1}"`+engineer+`"`)

	// Verify both keys exist after replacement (they should be present in a valid config).
	if !reSuper.MatchString(content) {
		return "", fmt.Errorf("superintendent key not found in [agent.models]")
	}
	if !reEng.MatchString(content) {
		return "", fmt.Errorf("engineer key not found in [agent.models]")
	}

	return result, nil
}

// formatPresets returns a human-readable list of available presets.
func formatPresets() string {
	names := []string{
		"claude", "gemini", "claude-cheap", "gemini-cheap", "hybrid", "hybrid-cheap",
		"claude-api-standard", "claude-api-cheap",
	}
	var sb strings.Builder
	for _, name := range names {
		m := presets[name]
		fmt.Fprintf(&sb, "  %-22s  superintendent=%s, engineer=%s\n", name, m.Superintendent, m.Engineer)
	}
	return sb.String()
}

// findConfigPath locates the madflow.toml configuration file.
func findConfigPath() (string, error) {
	proj, err := project.Detect()
	if err != nil {
		return "", err
	}

	configPath := ""
	for _, p := range proj.Paths {
		cp := filepath.Join(p, "madflow.toml")
		if _, err := os.Stat(cp); err == nil {
			configPath = cp
			break
		}
	}
	if configPath == "" {
		cwd, _ := os.Getwd()
		cp := filepath.Join(cwd, "madflow.toml")
		if _, err := os.Stat(cp); err == nil {
			configPath = cp
		}
	}

	if configPath == "" {
		return "", fmt.Errorf("madflow.toml not found")
	}

	return configPath, nil
}

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ytnobody/madflow/internal/config"
	"github.com/ytnobody/madflow/internal/project"
)

const useUsage = `Usage: madflow use <claude|gemini|mixed|economy>

Switches all agent models in madflow.toml to a specific backend.
  claude:  All roles use Claude models (high capability)
             superintendent: claude-sonnet-4-6
             engineer:       claude-sonnet-4-6
  gemini:  All roles use Gemini Flash (cost-effective)
             superintendent: gemini-2.0-flash
             engineer:       gemini-2.0-flash
  mixed:   Strategic roles use Claude, execution roles use Gemini (recommended)
             superintendent: claude-sonnet-4-6
             engineer:       gemini-2.0-flash
  economy: All roles use the most cost-effective Claude models
             superintendent: claude-haiku-4-5
             engineer:       claude-haiku-4-5
`

// resolvePreset returns the ModelConfig for a given backend preset name.
// Returns an error for unknown preset names.
func resolvePreset(backend string) (config.ModelConfig, error) {
	switch backend {
	case "claude":
		return config.ModelConfig{
			Superintendent: "claude-sonnet-4-6",
			Engineer:       "claude-sonnet-4-6",
		}, nil
	case "gemini":
		return config.ModelConfig{
			Superintendent: "gemini-2.0-flash",
			Engineer:       "gemini-2.0-flash",
		}, nil
	case "mixed":
		return config.ModelConfig{
			Superintendent: "claude-sonnet-4-6",
			Engineer:       "gemini-2.0-flash",
		}, nil
	case "economy":
		return config.ModelConfig{
			Superintendent: "claude-haiku-4-5",
			Engineer:       "claude-haiku-4-5",
		}, nil
	default:
		return config.ModelConfig{}, fmt.Errorf("unknown backend: %s", backend)
	}
}

func cmdUse() error {
	if len(os.Args) < 3 {
		fmt.Fprint(os.Stderr, useUsage)
		return nil
	}
	backend := os.Args[2]

	newModels, err := resolvePreset(backend)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		fmt.Fprint(os.Stderr, useUsage)
		return nil
	}

	// Find and load config
	configPath, err := findConfigPath()
	if err != nil {
		return err
	}

	// Read the raw file content
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	// Unmarshal to get the structure
	var cfg config.Config
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}

	// Update models
	cfg.Agent.Models = newModels

	// Marshal back to TOML
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	// Write back to the file
	if err := os.WriteFile(configPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("Switched all models to %s in %s\n", backend, configPath)
	return nil
}

// Helper to find config path, similar to loadProjectConfig but only returns path
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

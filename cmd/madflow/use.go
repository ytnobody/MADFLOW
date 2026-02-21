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

const useUsage = `Usage: madflow use <claude|gemini>

Switches all agent models in madflow.toml to a specific backend.
`

func cmdUse() error {
	if len(os.Args) < 3 {
		fmt.Fprint(os.Stderr, useUsage)
		return nil
	}
	backend := os.Args[2]

	var newModels config.ModelConfig
	switch backend {
	case "claude":
		newModels = config.ModelConfig{
			Superintendent: "claude-3-opus-20240229",
			PM:             "claude-3-sonnet-20240229",
			Architect:      "claude-3-opus-20240229",
			Engineer:       "claude-3-haiku-20240307",
			Reviewer:       "claude-3-sonnet-20240229",
			ReleaseManager: "claude-3-haiku-20240307",
		}
	case "gemini":
		newModels = config.ModelConfig{
			Superintendent: "gemini-1.5-pro-latest",
			PM:             "gemini-1.5-pro-latest",
			Architect:      "gemini-1.5-pro-latest",
			Engineer:       "gemini-1.5-flash-latest",
			Reviewer:       "gemini-1.5-pro-latest",
			ReleaseManager: "gemini-1.5-flash-latest",
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown backend: %s\n", backend)
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

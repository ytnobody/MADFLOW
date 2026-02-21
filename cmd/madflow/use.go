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

const useUsage = `Usage: madflow use <claude|gemini|mixed>

Switches all agent models in madflow.toml to a specific backend.
  claude: All roles use Claude models (stable, higher cost)
  gemini: All roles use Gemini models (cost-effective)
  mixed:  Strategic roles (superintendent, PM, architect, RM) use Claude,
          execution roles (engineer, reviewer) use Gemini (recommended for cost optimization)
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
			Superintendent: "claude-opus-4-6",
			PM:             "claude-sonnet-4-6",
			Architect:      "claude-opus-4-6",
			Engineer:       "claude-sonnet-4-6",
			Reviewer:       "claude-sonnet-4-6",
			ReleaseManager: "claude-haiku-4-5",
		}
	case "gemini":
		newModels = config.ModelConfig{
			Superintendent: "gemini-2.5-pro",
			PM:             "gemini-2.5-flash",
			Architect:      "gemini-2.5-pro",
			Engineer:       "gemini-2.5-flash",
			Reviewer:       "gemini-2.5-flash",
			ReleaseManager: "gemini-2.5-flash",
		}
	case "mixed":
		newModels = config.ModelConfig{
			Superintendent: "claude-sonnet-4-6",
			PM:             "claude-haiku-4-5",
			Architect:      "claude-sonnet-4-6",
			Engineer:       "gemini-2.5-flash",
			Reviewer:       "gemini-2.5-flash",
			ReleaseManager: "claude-haiku-4-5",
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

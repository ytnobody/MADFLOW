package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ytnobody/madflow/internal/project"
)

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

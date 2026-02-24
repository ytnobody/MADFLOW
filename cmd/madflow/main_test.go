package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdInit_GeneratedConfigDoesNotContainDeprecatedRoles(t *testing.T) {
	// Create a temporary directory to act as the project root.
	tmpDir := t.TempDir()

	// Change working directory to tmp so cmdInit writes files there.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Override os.Args to simulate: madflow init --name testproject
	origArgs := os.Args
	os.Args = []string{"madflow", "init", "--name", "testproject", "--repo", tmpDir}
	defer func() { os.Args = origArgs }()

	if err := cmdInit(); err != nil {
		t.Fatalf("cmdInit() error: %v", err)
	}

	configPath := filepath.Join(tmpDir, "madflow.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read generated madflow.toml: %v", err)
	}

	content := string(data)

	// Verify deprecated roles are not present.
	if strings.Contains(content, "reviewer") {
		t.Errorf("generated madflow.toml contains deprecated 'reviewer' key:\n%s", content)
	}
	if strings.Contains(content, "release_manager") {
		t.Errorf("generated madflow.toml contains deprecated 'release_manager' key:\n%s", content)
	}

	// Verify required roles are present.
	if !strings.Contains(content, "superintendent") {
		t.Errorf("generated madflow.toml is missing 'superintendent' key:\n%s", content)
	}
	if !strings.Contains(content, "engineer") {
		t.Errorf("generated madflow.toml is missing 'engineer' key:\n%s", content)
	}
}

func TestRoleColors_DoesNotContainDeprecatedRoles(t *testing.T) {
	deprecatedRoles := []string{"reviewer", "release_manager"}
	for _, role := range deprecatedRoles {
		if _, ok := roleColors[role]; ok {
			t.Errorf("roleColors map contains deprecated role %q", role)
		}
	}
}

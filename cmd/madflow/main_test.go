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

func TestCmdInit_CreatesDefaultPrompts(t *testing.T) {
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

	// Verify that prompts/ directory was created.
	promptsDir := filepath.Join(tmpDir, "prompts")
	info, err := os.Stat(promptsDir)
	if err != nil {
		t.Fatalf("prompts/ directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("prompts/ is not a directory")
	}

	// Verify that default prompt files exist and are non-empty.
	for _, name := range []string{"superintendent.md", "engineer.md"} {
		path := filepath.Join(promptsDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("default prompt file %s was not created: %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("default prompt file %s is empty", name)
		}
	}
}

func TestCmdInit_PreservesExistingPrompts(t *testing.T) {
	// Verify that cmdInit does NOT overwrite already-existing prompt files.
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Pre-create the prompts directory with custom content.
	promptsDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatalf("failed to create prompts dir: %v", err)
	}
	customContent := []byte("# custom prompt – do not overwrite")
	customFile := filepath.Join(promptsDir, "superintendent.md")
	if err := os.WriteFile(customFile, customContent, 0644); err != nil {
		t.Fatalf("failed to write custom prompt: %v", err)
	}

	origArgs := os.Args
	os.Args = []string{"madflow", "init", "--name", "testproject", "--repo", tmpDir}
	defer func() { os.Args = origArgs }()

	if err := cmdInit(); err != nil {
		t.Fatalf("cmdInit() error: %v", err)
	}

	// The custom file must not have been overwritten.
	got, err := os.ReadFile(customFile)
	if err != nil {
		t.Fatalf("failed to read superintendent.md: %v", err)
	}
	if string(got) != string(customContent) {
		t.Errorf("cmdInit overwrote existing prompt file; got %q, want %q", got, customContent)
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

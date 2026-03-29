package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitAndDetect(t *testing.T) {
	// Override home dir for test
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	projectDir := filepath.Join(tmpHome, "my-app")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Init
	if err := Init("my-app", []string{projectDir}); err != nil {
		t.Fatal(err)
	}

	// Verify data directories created
	dataDir := filepath.Join(tmpHome, madflowDir, "my-app")
	for _, sub := range []string{"issues", "memos"} {
		if _, err := os.Stat(filepath.Join(dataDir, sub)); err != nil {
			t.Errorf("expected %s dir to exist: %v", sub, err)
		}
	}

	// Verify registry
	regPath := filepath.Join(tmpHome, madflowDir, projectsFile)
	if _, err := os.Stat(regPath); err != nil {
		t.Fatal("expected projects.toml to exist")
	}

	// Detect from cwd
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(projectDir)

	proj, err := Detect()
	if err != nil {
		t.Fatal(err)
	}
	if proj.ID != "my-app" {
		t.Errorf("expected project ID my-app, got %s", proj.ID)
	}
}

func TestDetectFromConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	projectDir := t.TempDir()
	configContent := `
[project]
name = "config-app"

[[project.repos]]
name = "main"
path = "."
`
	if err := os.WriteFile(filepath.Join(projectDir, configFileName), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(projectDir)

	proj, err := Detect()
	if err != nil {
		t.Fatal(err)
	}
	if proj.ID != "config-app" {
		t.Errorf("expected project ID config-app, got %s", proj.ID)
	}
}

func TestDetectNotFound(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	emptyDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(emptyDir)

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for unregistered directory")
	}
}

// TestInitCreatesDirectoriesWithRestrictedPermissions verifies that Init creates
// data directories with 0700 permissions.
func TestInitCreatesDirectoriesWithRestrictedPermissions(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	projectDir := filepath.Join(tmpHome, "perm-test-app")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := Init("perm-test-app", []string{projectDir}); err != nil {
		t.Fatal(err)
	}

	dataDir := filepath.Join(tmpHome, madflowDir, "perm-test-app")
	for _, sub := range []string{"issues", "memos"} {
		info, err := os.Stat(filepath.Join(dataDir, sub))
		if err != nil {
			t.Fatalf("expected %s dir to exist: %v", sub, err)
		}
		got := info.Mode().Perm()
		want := os.FileMode(0700)
		if got != want {
			t.Errorf("data dir %s permission: got %04o, want %04o", sub, got, want)
		}
	}
}

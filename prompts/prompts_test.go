package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDefault_Superintendent(t *testing.T) {
	data, err := ReadDefault("superintendent.md")
	if err != nil {
		t.Fatalf("ReadDefault(superintendent.md): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty content for superintendent.md")
	}
}

func TestReadDefault_Engineer(t *testing.T) {
	data, err := ReadDefault("engineer.md")
	if err != nil {
		t.Fatalf("ReadDefault(engineer.md): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty content for engineer.md")
	}
}

func TestReadDefault_Unknown(t *testing.T) {
	_, err := ReadDefault("unknown.md")
	if err == nil {
		t.Fatal("expected error for unknown prompt file")
	}
}

func TestWriteDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDefaults(dir); err != nil {
		t.Fatalf("WriteDefaults: %v", err)
	}

	for _, name := range []string{"superintendent.md", "engineer.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("expected non-empty %s", name)
		}
	}
}

func TestWriteDefaults_PreservesExisting(t *testing.T) {
	dir := t.TempDir()

	// Write a custom superintendent.md first
	custom := []byte("custom content")
	if err := os.WriteFile(filepath.Join(dir, "superintendent.md"), custom, 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteDefaults(dir); err != nil {
		t.Fatalf("WriteDefaults: %v", err)
	}

	// Custom file should be preserved
	got, err := os.ReadFile(filepath.Join(dir, "superintendent.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Errorf("expected custom content to be preserved, got %q", got)
	}

	// engineer.md (which was not pre-existing) should have been created
	if _, err := os.Stat(filepath.Join(dir, "engineer.md")); err != nil {
		t.Errorf("expected engineer.md to be created: %v", err)
	}
}

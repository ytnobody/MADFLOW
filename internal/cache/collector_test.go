package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ytnobody/madflow/internal/cache"
)

func TestCollector_Collect(t *testing.T) {
	// Create a temp directory with test files
	tmpDir := t.TempDir()
	writeFile(t, tmpDir, "main.go", "package main\nfunc main() {}")
	writeFile(t, tmpDir, "README.md", "# Test Project")
	writeFile(t, tmpDir, "go.mod", "module test\ngo 1.21")
	writeFile(t, tmpDir, "vendor/vendor.go", "package vendor") // should be excluded

	patterns := []string{"*.go", "*.md", "go.mod"}
	c := cache.NewCollector(tmpDir, patterns, 1024)

	content, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	// Should have main.go, README.md, go.mod (3 files, vendor excluded)
	if len(content.ProjectFiles) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(content.ProjectFiles), fileNames(content.ProjectFiles))
	}

	// Verify vendor is excluded
	for _, f := range content.ProjectFiles {
		if filepath.HasPrefix(f.Path, "vendor/") {
			t.Errorf("vendor file should be excluded: %s", f.Path)
		}
	}
}

func TestCollector_Hash_Deterministic(t *testing.T) {
	c := cache.NewCollector("", nil, 0)
	content := &cache.CacheContent{
		SystemInstruction: "test system",
		ProjectFiles: []cache.ProjectFile{
			{Path: "main.go", Content: "package main"},
			{Path: "go.mod", Content: "module test"},
		},
	}

	hash1 := c.Hash(content)
	hash2 := c.Hash(content)

	if hash1 != hash2 {
		t.Errorf("Hash() is not deterministic: %s != %s", hash1, hash2)
	}
	if len(hash1) == 0 {
		t.Error("Hash() returned empty string")
	}
}

func TestCollector_Hash_ChangesOnContentChange(t *testing.T) {
	c := cache.NewCollector("", nil, 0)

	content1 := &cache.CacheContent{
		ProjectFiles: []cache.ProjectFile{
			{Path: "main.go", Content: "package main"},
		},
	}
	content2 := &cache.CacheContent{
		ProjectFiles: []cache.ProjectFile{
			{Path: "main.go", Content: "package main\nfunc main() {}"},
		},
	}

	hash1 := c.Hash(content1)
	hash2 := c.Hash(content2)

	if hash1 == hash2 {
		t.Error("Hash() should change when content changes")
	}
}

func TestCollector_ExcludesLargeFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a normal file (1KB)
	writeFile(t, tmpDir, "small.go", "package main\n")

	// Write a large file (exceeding limit)
	largeContent := make([]byte, 2*1024*1024) // 2MB
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "large.go"), largeContent, 0644); err != nil {
		t.Fatal(err)
	}

	c := cache.NewCollector(tmpDir, []string{"*.go"}, 1024) // max 1024KB = 1MB
	content, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	for _, f := range content.ProjectFiles {
		if f.Path == "large.go" {
			t.Error("large.go should have been excluded")
		}
	}
	if len(content.ProjectFiles) != 1 || content.ProjectFiles[0].Path != "small.go" {
		t.Errorf("expected only small.go, got: %v", fileNames(content.ProjectFiles))
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func fileNames(files []cache.ProjectFile) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Path
	}
	return names
}

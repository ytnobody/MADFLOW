package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ytnobody/madflow/internal/config"
)

func TestLoadConfig_WithCacheSection(t *testing.T) {
	content := `
[project]
name = "test"
[[project.repos]]
name = "repo1"
path = "/tmp/repo1"

[agent]
context_reset_minutes = 8

[branches]
main = "main"
develop = "develop"

[cache]
enabled = true
ttl_minutes = 45
max_file_size_kb = 512
file_patterns = ["*.go", "*.md"]
`
	tmpFile := filepath.Join(t.TempDir(), "madflow.toml")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cache == nil {
		t.Fatal("Cache config should not be nil")
	}
	if !cfg.Cache.Enabled {
		t.Error("Cache.Enabled should be true")
	}
	if cfg.Cache.TTLMinutes != 45 {
		t.Errorf("Cache.TTLMinutes = %d, want 45", cfg.Cache.TTLMinutes)
	}
	if cfg.Cache.MaxFileSizeKB != 512 {
		t.Errorf("Cache.MaxFileSizeKB = %d, want 512", cfg.Cache.MaxFileSizeKB)
	}
	if len(cfg.Cache.FilePatterns) != 2 {
		t.Errorf("Cache.FilePatterns len = %d, want 2", len(cfg.Cache.FilePatterns))
	}
}

func TestLoadConfig_CacheDefaults(t *testing.T) {
	content := `
[project]
name = "test"
[[project.repos]]
name = "repo1"
path = "/tmp/repo1"

[cache]
enabled = true
`
	tmpFile := filepath.Join(t.TempDir(), "madflow.toml")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cache == nil {
		t.Fatal("Cache config should not be nil")
	}
	// Defaults should be applied
	if cfg.Cache.TTLMinutes != 30 {
		t.Errorf("Cache.TTLMinutes = %d, want 30 (default)", cfg.Cache.TTLMinutes)
	}
	if cfg.Cache.MaxFileSizeKB != 1024 {
		t.Errorf("Cache.MaxFileSizeKB = %d, want 1024 (default)", cfg.Cache.MaxFileSizeKB)
	}
}

func TestLoadConfig_NoCacheSection(t *testing.T) {
	content := `
[project]
name = "test"
[[project.repos]]
name = "repo1"
path = "/tmp/repo1"
`
	tmpFile := filepath.Join(t.TempDir(), "madflow.toml")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cache != nil {
		t.Error("Cache config should be nil when not configured")
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "/tmp/test-app"

[agent]
context_reset_minutes = 10

[agent.models]
superintendent = "claude-opus-4-6"

[branches]
main = "main"
develop = "develop"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Project.Name != "test-app" {
		t.Errorf("expected project name test-app, got %s", cfg.Project.Name)
	}
	if len(cfg.Project.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(cfg.Project.Repos))
	}
	if cfg.Agent.ContextResetMinutes != 10 {
		t.Errorf("expected context_reset_minutes 10, got %d", cfg.Agent.ContextResetMinutes)
	}
	if cfg.Branches.FeaturePrefix != "feature/issue-" {
		t.Errorf("expected default feature prefix, got %s", cfg.Branches.FeaturePrefix)
	}
}

func TestLoadDefaults(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Agent.ContextResetMinutes != 8 {
		t.Errorf("expected default 8, got %d", cfg.Agent.ContextResetMinutes)
	}
	if cfg.Agent.Models.Superintendent != "claude-sonnet-4-6" {
		t.Errorf("expected default superintendent model claude-sonnet-4-6, got %s", cfg.Agent.Models.Superintendent)
	}
	if cfg.Agent.Models.Engineer != "claude-haiku-4-5" {
		t.Errorf("expected default engineer model claude-haiku-4-5, got %s", cfg.Agent.Models.Engineer)
	}
}

func TestLoadValidationError(t *testing.T) {
	content := `
[project]
name = ""

[[project.repos]]
name = "main"
path = "."
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadWithGitHub(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "api"
path = "/tmp/api"

[github]
owner = "myorg"
repos = ["api"]
sync_interval_minutes = 10
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.GitHub == nil {
		t.Fatal("expected github config")
	}
	if cfg.GitHub.Owner != "myorg" {
		t.Errorf("expected owner myorg, got %s", cfg.GitHub.Owner)
	}
	if cfg.GitHub.SyncIntervalMinutes != 10 {
		t.Errorf("expected sync interval 10, got %d", cfg.GitHub.SyncIntervalMinutes)
	}
}

func TestEventPollSecondsDefault(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "api"
path = "/tmp/api"

[github]
owner = "myorg"
repos = ["api"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.GitHub.EventPollSeconds != 60 {
		t.Errorf("expected default event_poll_seconds 60, got %d", cfg.GitHub.EventPollSeconds)
	}
	if cfg.GitHub.SyncIntervalMinutes != 15 {
		t.Errorf("expected default sync_interval_minutes 15, got %d", cfg.GitHub.SyncIntervalMinutes)
	}
}

func TestEventPollSecondsCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "api"
path = "/tmp/api"

[github]
owner = "myorg"
repos = ["api"]
event_poll_seconds = 30
sync_interval_minutes = 10
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.GitHub.EventPollSeconds != 30 {
		t.Errorf("expected event_poll_seconds 30, got %d", cfg.GitHub.EventPollSeconds)
	}
	if cfg.GitHub.SyncIntervalMinutes != 10 {
		t.Errorf("expected sync_interval_minutes 10, got %d", cfg.GitHub.SyncIntervalMinutes)
	}
}

func TestChatlogMaxLinesDefault(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Agent.ChatlogMaxLines != 500 {
		t.Errorf("expected default chatlog_max_lines 500, got %d", cfg.Agent.ChatlogMaxLines)
	}
}

func TestChatlogMaxLinesCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
chatlog_max_lines = 1000
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Agent.ChatlogMaxLines != 1000 {
		t.Errorf("expected chatlog_max_lines 1000, got %d", cfg.Agent.ChatlogMaxLines)
	}
}

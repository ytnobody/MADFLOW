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

func TestMainCheckIntervalHoursDefault(t *testing.T) {
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

	if cfg.Agent.MainCheckIntervalHours != 6 {
		t.Errorf("expected default main_check_interval_hours 6, got %d", cfg.Agent.MainCheckIntervalHours)
	}
}

func TestMainCheckIntervalHoursCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
main_check_interval_hours = 12
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

	if cfg.Agent.MainCheckIntervalHours != 12 {
		t.Errorf("expected main_check_interval_hours 12, got %d", cfg.Agent.MainCheckIntervalHours)
	}
}

func TestMainCheckIntervalHoursDisabled(t *testing.T) {
	// Setting to -1 is invalid but 0 would trigger the default (6).
	// Users who want to disable must set a negative value... actually
	// looking at the design, 0 triggers the default. Let's verify
	// the default is applied correctly when not specified.
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
main_check_interval_hours = 0
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

	// 0 triggers the default of 6
	if cfg.Agent.MainCheckIntervalHours != 6 {
		t.Errorf("expected default 6 when 0 is set, got %d", cfg.Agent.MainCheckIntervalHours)
	}
}

func TestExtraPromptDefault(t *testing.T) {
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

	if cfg.Agent.ExtraPrompt != "" {
		t.Errorf("expected empty extra_prompt by default, got %q", cfg.Agent.ExtraPrompt)
	}
}

func TestExtraPromptCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
extra_prompt = "このプロジェクトはGoで書かれています。コーディング規約を遵守してください。"
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

	expected := "このプロジェクトはGoで書かれています。コーディング規約を遵守してください。"
	if cfg.Agent.ExtraPrompt != expected {
		t.Errorf("expected extra_prompt %q, got %q", expected, cfg.Agent.ExtraPrompt)
	}
}

func TestAuthorizedUsersDefault(t *testing.T) {
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

	if len(cfg.AuthorizedUsers) != 0 {
		t.Errorf("expected empty authorized_users by default, got %v", cfg.AuthorizedUsers)
	}
}

func TestAuthorizedUsersCustom(t *testing.T) {
	content := `
authorized_users = ["alice", "bob"]

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

	if len(cfg.AuthorizedUsers) != 2 {
		t.Fatalf("expected 2 authorized_users, got %d", len(cfg.AuthorizedUsers))
	}
	if cfg.AuthorizedUsers[0] != "alice" || cfg.AuthorizedUsers[1] != "bob" {
		t.Errorf("expected [alice bob], got %v", cfg.AuthorizedUsers)
	}
}

func TestDocCheckIntervalHoursDefault(t *testing.T) {
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

	if cfg.Agent.DocCheckIntervalHours != 24 {
		t.Errorf("expected default doc_check_interval_hours 24, got %d", cfg.Agent.DocCheckIntervalHours)
	}
}

func TestDocCheckIntervalHoursCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
doc_check_interval_hours = 48
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

	if cfg.Agent.DocCheckIntervalHours != 48 {
		t.Errorf("expected doc_check_interval_hours 48, got %d", cfg.Agent.DocCheckIntervalHours)
	}
}

func TestGeminiRPMDefault(t *testing.T) {
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

	if cfg.Agent.GeminiRPM != 10 {
		t.Errorf("expected default gemini_rpm 10, got %d", cfg.Agent.GeminiRPM)
	}
}

func TestGeminiRPMCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
gemini_rpm = 10
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

	if cfg.Agent.GeminiRPM != 10 {
		t.Errorf("expected gemini_rpm 10, got %d", cfg.Agent.GeminiRPM)
	}
}

func TestDormancyProbeMinutesDefault(t *testing.T) {
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

	if cfg.Agent.DormancyProbeMinutes != 3 {
		t.Errorf("expected default dormancy_probe_minutes 3, got %d", cfg.Agent.DormancyProbeMinutes)
	}
}

func TestDormancyProbeMinutesCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
dormancy_probe_minutes = 5
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

	if cfg.Agent.DormancyProbeMinutes != 5 {
		t.Errorf("expected dormancy_probe_minutes 5, got %d", cfg.Agent.DormancyProbeMinutes)
	}
}

func TestBashTimeoutMinutesDefault(t *testing.T) {
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

	if cfg.Agent.BashTimeoutMinutes != 5 {
		t.Errorf("expected default bash_timeout_minutes 5, got %d", cfg.Agent.BashTimeoutMinutes)
	}
}

func TestBashTimeoutMinutesCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
bash_timeout_minutes = 10
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

	if cfg.Agent.BashTimeoutMinutes != 10 {
		t.Errorf("expected bash_timeout_minutes 10, got %d", cfg.Agent.BashTimeoutMinutes)
	}
}

func TestGitHubBotCommentPatterns(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[github]
owner = "testowner"
repos = ["testrepo"]
bot_comment_patterns = ["^\\*\\*\\[", "\\[bot\\]$"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.GitHub == nil {
		t.Fatal("expected GitHub config, got nil")
	}
	if len(cfg.GitHub.BotCommentPatterns) != 2 {
		t.Fatalf("expected 2 bot_comment_patterns, got %d", len(cfg.GitHub.BotCommentPatterns))
	}
	if cfg.GitHub.BotCommentPatterns[0] != `^\*\*\[` {
		t.Errorf("unexpected first pattern: %q", cfg.GitHub.BotCommentPatterns[0])
	}
}

func TestGitHubBotCommentPatterns_Empty(t *testing.T) {
	// When bot_comment_patterns is not configured, it should default to nil/empty.
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[github]
owner = "testowner"
repos = ["testrepo"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.GitHub == nil {
		t.Fatal("expected GitHub config, got nil")
	}
	if len(cfg.GitHub.BotCommentPatterns) != 0 {
		t.Errorf("expected empty bot_comment_patterns, got %v", cfg.GitHub.BotCommentPatterns)
	}
}

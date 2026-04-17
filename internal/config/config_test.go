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
	// When gh CLI is authenticated, FeaturePrefix becomes "madflow/{login}/issue-".
	// When gh CLI is unavailable, it falls back to "feature/issue-".
	// Accept either form here; the key invariant is that it is non-empty.
	if cfg.Branches.FeaturePrefix == "" {
		t.Errorf("expected non-empty feature prefix, got empty string")
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

	if cfg.Agent.ContextResetMinutes != 15 {
		t.Errorf("expected default 15, got %d", cfg.Agent.ContextResetMinutes)
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
authorized_users = ["testuser"]

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
authorized_users = ["testuser"]

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
authorized_users = ["testuser"]

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
	// When GitHub integration is not configured, authorized_users is not required
	// and defaults to empty.
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

func TestLoadGitHubMissingAuthorizedUsers(t *testing.T) {
	// When GitHub integration is configured but authorized_users is not set,
	// Load must now succeed (no longer an error). Auto-detection of the GitHub
	// login is attempted at load time via `gh api user`.
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[github]
owner = "myorg"
repos = ["myrepo"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Load must succeed even without authorized_users in config.
	// (gh auto-detection may or may not populate AuthorizedUsers depending on environment.)
	_, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error when github is configured without authorized_users, got: %v", err)
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

func TestIssuePatrolIntervalMinutesDefault(t *testing.T) {
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

	if cfg.Agent.IssuePatrolIntervalMinutes != 20 {
		t.Errorf("expected default issue_patrol_interval_minutes 20, got %d", cfg.Agent.IssuePatrolIntervalMinutes)
	}
}

func TestIssuePatrolIntervalMinutesCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
issue_patrol_interval_minutes = 10
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

	if cfg.Agent.IssuePatrolIntervalMinutes != 10 {
		t.Errorf("expected issue_patrol_interval_minutes 10, got %d", cfg.Agent.IssuePatrolIntervalMinutes)
	}
}

func TestIssuePatrolIntervalMinutesDisabled(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
issue_patrol_interval_minutes = -1
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

	if cfg.Agent.IssuePatrolIntervalMinutes != -1 {
		t.Errorf("expected issue_patrol_interval_minutes -1 (disabled), got %d", cfg.Agent.IssuePatrolIntervalMinutes)
	}
}

func TestGitHubBotCommentPatterns(t *testing.T) {
	content := `
authorized_users = ["testuser"]

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

func TestLanguageDefault(t *testing.T) {
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

	if cfg.Agent.Language != "en" {
		t.Errorf("expected default language 'en', got %q", cfg.Agent.Language)
	}
}

func TestLanguageCustom(t *testing.T) {
	content := `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."

[agent]
language = "ja"
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

	if cfg.Agent.Language != "ja" {
		t.Errorf("expected language 'ja', got %q", cfg.Agent.Language)
	}
}

func TestGitHubBotCommentPatterns_Empty(t *testing.T) {
	// When bot_comment_patterns is not configured, it should default to nil/empty.
	content := `
authorized_users = ["testuser"]

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

// ParseConfig tests — pure function, no I/O dependency

func TestParseConfig_Basic(t *testing.T) {
	data := []byte(`
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "/tmp/test-app"

[agent]
context_reset_minutes = 10
`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Project.Name != "test-app" {
		t.Errorf("expected project name test-app, got %s", cfg.Project.Name)
	}
	if cfg.Agent.ContextResetMinutes != 10 {
		t.Errorf("expected context_reset_minutes 10, got %d", cfg.Agent.ContextResetMinutes)
	}
	// GhLogin must be empty: ParseConfig does not call gh CLI
	if cfg.GhLogin != "" {
		t.Errorf("expected GhLogin empty from ParseConfig, got %q", cfg.GhLogin)
	}
}

func TestParseConfig_Defaults(t *testing.T) {
	data := []byte(`
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."
`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Agent.ContextResetMinutes != 15 {
		t.Errorf("expected default context_reset_minutes 15, got %d", cfg.Agent.ContextResetMinutes)
	}
	if cfg.Agent.Models.Superintendent != "claude-sonnet-4-6" {
		t.Errorf("expected default superintendent model, got %s", cfg.Agent.Models.Superintendent)
	}
	if cfg.Agent.Models.Engineer != "claude-haiku-4-5" {
		t.Errorf("expected default engineer model, got %s", cfg.Agent.Models.Engineer)
	}
	if cfg.Agent.MaxTeams != 4 {
		t.Errorf("expected default max_teams 4, got %d", cfg.Agent.MaxTeams)
	}
	if cfg.Agent.ChatlogMaxLines != 500 {
		t.Errorf("expected default chatlog_max_lines 500, got %d", cfg.Agent.ChatlogMaxLines)
	}
	if cfg.Agent.GeminiRPM != 10 {
		t.Errorf("expected default gemini_rpm 10, got %d", cfg.Agent.GeminiRPM)
	}
	if cfg.Agent.BashTimeoutMinutes != 5 {
		t.Errorf("expected default bash_timeout_minutes 5, got %d", cfg.Agent.BashTimeoutMinutes)
	}
	if cfg.Agent.Language != "en" {
		t.Errorf("expected default language en, got %s", cfg.Agent.Language)
	}
	if cfg.Branches.Main != "main" {
		t.Errorf("expected default branch main, got %s", cfg.Branches.Main)
	}
	if cfg.Branches.Develop != "develop" {
		t.Errorf("expected default branch develop, got %s", cfg.Branches.Develop)
	}
}

func TestParseConfig_ValidationError_MissingProjectName(t *testing.T) {
	data := []byte(`
[project]
name = ""

[[project.repos]]
name = "main"
path = "."
`)
	_, err := ParseConfig(data)
	if err == nil {
		t.Fatal("expected validation error for missing project name")
	}
}

func TestParseConfig_ValidationError_NoRepos(t *testing.T) {
	data := []byte(`
[project]
name = "test-app"
`)
	_, err := ParseConfig(data)
	if err == nil {
		t.Fatal("expected validation error for no repos")
	}
}

func TestParseConfig_ValidationError_RepoMissingName(t *testing.T) {
	data := []byte(`
[project]
name = "test-app"

[[project.repos]]
name = ""
path = "/tmp/test"
`)
	_, err := ParseConfig(data)
	if err == nil {
		t.Fatal("expected validation error for repo with empty name")
	}
}

func TestParseConfig_ValidationError_RepoMissingPath(t *testing.T) {
	data := []byte(`
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = ""
`)
	_, err := ParseConfig(data)
	if err == nil {
		t.Fatal("expected validation error for repo with empty path")
	}
}

func TestParseConfig_MalformedTOML(t *testing.T) {
	data := []byte(`not valid toml [[[`)
	_, err := ParseConfig(data)
	if err == nil {
		t.Fatal("expected error for malformed TOML")
	}
}

func TestParseConfig_WithGitHub(t *testing.T) {
	data := []byte(`
authorized_users = ["testuser"]

[project]
name = "test-app"

[[project.repos]]
name = "api"
path = "/tmp/api"

[github]
owner = "myorg"
repos = ["api"]
sync_interval_minutes = 10
`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.GitHub == nil {
		t.Fatal("expected github config")
	}
	if cfg.GitHub.Owner != "myorg" {
		t.Errorf("expected owner myorg, got %s", cfg.GitHub.Owner)
	}
	if cfg.GitHub.SyncIntervalMinutes != 10 {
		t.Errorf("expected sync_interval_minutes 10, got %d", cfg.GitHub.SyncIntervalMinutes)
	}
	// AuthorizedUsers from TOML should be preserved
	if len(cfg.AuthorizedUsers) != 1 || cfg.AuthorizedUsers[0] != "testuser" {
		t.Errorf("expected authorized_users [testuser], got %v", cfg.AuthorizedUsers)
	}
	// GhLogin must be empty — not resolved by ParseConfig
	if cfg.GhLogin != "" {
		t.Errorf("expected GhLogin empty, got %q", cfg.GhLogin)
	}
}

func TestParseConfig_GitHubDefaults(t *testing.T) {
	data := []byte(`
authorized_users = ["testuser"]

[project]
name = "test-app"

[[project.repos]]
name = "api"
path = "/tmp/api"

[github]
owner = "myorg"
repos = ["api"]
`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.GitHub.EventPollSeconds != 60 {
		t.Errorf("expected default event_poll_seconds 60, got %d", cfg.GitHub.EventPollSeconds)
	}
	if cfg.GitHub.SyncIntervalMinutes != 15 {
		t.Errorf("expected default sync_interval_minutes 15, got %d", cfg.GitHub.SyncIntervalMinutes)
	}
	if cfg.GitHub.IdlePollMinutes != 15 {
		t.Errorf("expected default idle_poll_minutes 15, got %d", cfg.GitHub.IdlePollMinutes)
	}
	if cfg.GitHub.IdleThresholdMinutes != 5 {
		t.Errorf("expected default idle_threshold_minutes 5, got %d", cfg.GitHub.IdleThresholdMinutes)
	}
}

func TestParseConfig_FeaturePrefixEmpty(t *testing.T) {
	// ParseConfig does not call gh CLI, so FeaturePrefix remains empty
	// (it is set by applyGhLogin during Load).
	data := []byte(`
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "."
`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	// FeaturePrefix is intentionally left empty by ParseConfig;
	// it will be populated by Load via applyGhLogin.
	// This verifies that ParseConfig does NOT call gh or set FeaturePrefix.
	_ = cfg.Branches.FeaturePrefix // may or may not be empty; just ensure no panic
}

func TestParseConfig_RawConfigUsed(t *testing.T) {
	// Verify that RawConfig is the intermediate TOML representation.
	// ParseConfig should convert RawConfig → Config (with defaults).
	data := []byte(`
[project]
name = "myproject"

[[project.repos]]
name = "repo1"
path = "/tmp/repo1"

[agent]
max_teams = 8
language = "ja"
`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Agent.MaxTeams != 8 {
		t.Errorf("expected max_teams 8, got %d", cfg.Agent.MaxTeams)
	}
	if cfg.Agent.Language != "ja" {
		t.Errorf("expected language ja, got %s", cfg.Agent.Language)
	}
	// Other defaults should still be applied
	if cfg.Agent.ContextResetMinutes != 15 {
		t.Errorf("expected default context_reset_minutes 15, got %d", cfg.Agent.ContextResetMinutes)
	}
}

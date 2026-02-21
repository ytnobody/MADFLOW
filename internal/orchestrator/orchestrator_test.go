package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/config"
)

func testConfig(repoPath string) *config.Config {
	return &config.Config{
		Project: config.ProjectConfig{
			Name: "test-project",
			Repos: []config.RepoConfig{
				{Name: "main", Path: repoPath},
			},
		},
		Agent: config.AgentConfig{
			ContextResetMinutes: 8,
			Models: config.ModelConfig{
				Superintendent: "claude-opus-4-6",
				PM:             "claude-sonnet-4-6",
				Architect:      "claude-opus-4-6",
				Engineer:       "claude-sonnet-4-6",
				Reviewer:       "claude-sonnet-4-6",
				ReleaseManager: "claude-haiku-4-5",
			},
		},
		Branches: config.BranchConfig{
			Main:          "main",
			Develop:       "develop",
			FeaturePrefix: "feature/issue-",
		},
	}
}

func TestNew(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	promptDir := t.TempDir()

	orc := New(cfg, dir, promptDir)
	if orc == nil {
		t.Fatal("New returned nil")
	}
	if orc.teams == nil {
		t.Error("teams manager is nil")
	}
	if orc.store == nil {
		t.Error("store is nil")
	}
	if orc.chatLog == nil {
		t.Error("chatLog is nil")
	}
}

func TestNewWithGitHub(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.GitHub = &config.GitHubConfig{
		Owner:               "testowner",
		Repos:               []string{"repo1"},
		SyncIntervalMinutes: 5,
	}

	orc := New(cfg, dir, t.TempDir())
	if orc.cfg.GitHub == nil {
		t.Error("GitHub config should be set")
	}
}

func TestFirstRepoPath(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	orc := New(cfg, dir, t.TempDir())

	got := orc.firstRepoPath()
	if got != dir {
		t.Errorf("expected %s, got %s", dir, got)
	}
}

func TestFirstRepoPathEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Project.Repos = nil
	orc := New(cfg, dir, t.TempDir())

	got := orc.firstRepoPath()
	if got != "." {
		t.Errorf("expected '.', got %s", got)
	}
}

func TestHandleCommand(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	// Create issues dir
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)

	orc := New(cfg, dir, t.TempDir())

	// Test unknown command doesn't panic
	msg := chatlog.Message{
		Sender: "pm",
		Body:   "UNKNOWN_CMD",
	}
	orc.handleCommand(t.Context(), msg)
}

func TestHandleTeamCreateMissingID(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)

	orc := New(cfg, dir, t.TempDir())

	// TEAM_CREATE without issue ID should not panic
	orc.handleTeamCreate(t.Context(), "TEAM_CREATE")
}

func TestHandleTeamDisbandMissingID(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)

	orc := New(cfg, dir, t.TempDir())

	// TEAM_DISBAND without issue ID should not panic
	orc.handleTeamDisband("TEAM_DISBAND")
}

func TestChatLogPath(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	orc := New(cfg, dir, t.TempDir())

	expected := filepath.Join(dir, "chatlog.txt")
	if orc.ChatLogPath() != expected {
		t.Errorf("expected %s, got %s", expected, orc.ChatLogPath())
	}
}

func TestStoreAccess(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)

	orc := New(cfg, dir, t.TempDir())
	if orc.Store() == nil {
		t.Error("Store() returned nil")
	}
}

func TestTeamsAccess(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	orc := New(cfg, dir, t.TempDir())

	if orc.Teams() == nil {
		t.Error("Teams() returned nil")
	}
	if orc.Teams().Count() != 0 {
		t.Errorf("expected 0 teams initially, got %d", orc.Teams().Count())
	}
}

func TestCreateTeamAgents(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.MkdirAll(filepath.Join(dir, "memos"), 0755)

	// Create prompt templates
	promptDir := t.TempDir()
	for _, name := range []string{"architect.md", "engineer.md", "reviewer.md"} {
		content := "# " + name + "\nAgent: {{AGENT_ID}} Team: {{TEAM_NUM}}"
		os.WriteFile(filepath.Join(promptDir, name), []byte(content), 0644)
	}

	orc := New(cfg, dir, promptDir)

	architect, engineer, reviewer, err := orc.CreateTeamAgents(1, "test-issue-001")
	if err != nil {
		t.Fatalf("CreateTeamAgents failed: %v", err)
	}
	if architect == nil || engineer == nil || reviewer == nil {
		t.Fatal("one or more agents is nil")
	}

	// Verify agent IDs
	if architect.ID.Role != "architect" {
		t.Errorf("expected architect role, got %s", architect.ID.Role)
	}
	if engineer.ID.Role != "engineer" {
		t.Errorf("expected engineer role, got %s", engineer.ID.Role)
	}
	if reviewer.ID.Role != "reviewer" {
		t.Errorf("expected reviewer role, got %s", reviewer.ID.Role)
	}

	// All agents should have team number 1
	if architect.ID.TeamNum != 1 {
		t.Errorf("expected team num 1, got %d", architect.ID.TeamNum)
	}
}

func TestCreateTeamAgentsWithIssue(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.MkdirAll(filepath.Join(dir, "memos"), 0755)

	// Create prompt templates
	promptDir := t.TempDir()
	for _, name := range []string{"architect.md", "engineer.md", "reviewer.md"} {
		content := "# " + name
		os.WriteFile(filepath.Join(promptDir, name), []byte(content), 0644)
	}

	orc := New(cfg, dir, promptDir)

	// Create an issue first
	iss, err := orc.Store().Create("Test Issue", "Test body")
	if err != nil {
		t.Fatalf("Create issue failed: %v", err)
	}

	architect, _, _, err := orc.CreateTeamAgents(1, iss.ID)
	if err != nil {
		t.Fatalf("CreateTeamAgents failed: %v", err)
	}

	// Architect should have the issue context as original task
	if !strings.Contains(architect.OriginalTask, "Test Issue") {
		t.Errorf("expected original task to contain issue title, got: %s", architect.OriginalTask)
	}
}

func TestCreateTeamAgentsMissingPrompt(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.MkdirAll(filepath.Join(dir, "memos"), 0755)

	// Empty prompt dir - no templates
	promptDir := t.TempDir()

	orc := New(cfg, dir, promptDir)

	_, _, _, err := orc.CreateTeamAgents(1, "test-issue")
	if err == nil {
		t.Fatal("expected error for missing prompt templates")
	}
}

package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ytnobody/madflow/internal/agent"
	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/config"
	"github.com/ytnobody/madflow/internal/issue"
	"github.com/ytnobody/madflow/internal/team"
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
				Engineer:       "claude-sonnet-4-6",
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
		Sender: "superintendent",
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
	for _, name := range []string{"architect.md", "engineer.md"} {
		content := "# " + name + "\nAgent: {{AGENT_ID}} Team: {{TEAM_NUM}}"
		os.WriteFile(filepath.Join(promptDir, name), []byte(content), 0644)
	}

	orc := New(cfg, dir, promptDir)

	engineer, err := orc.CreateTeamAgents(1, "test-issue-001")
	if err != nil {
		t.Fatalf("CreateTeamAgents failed: %v", err)
	}
	if engineer == nil {
		t.Fatal("one or more agents is nil")
	}


	if engineer.ID.Role != "engineer" {
		t.Errorf("expected engineer role, got %s", engineer.ID.Role)
	}


	// All agents should have team number 1
	// if architect.ID.TeamNum != 1 {
	// 	t.Errorf("expected team num 1, got %d", architect.ID.TeamNum)
	// }
}

func TestCreateTeamAgentsWithIssue(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.MkdirAll(filepath.Join(dir, "memos"), 0755)

	// Create prompt templates
	promptDir := t.TempDir()
	for _, name := range []string{"architect.md", "engineer.md"} {
		content := "# " + name
		os.WriteFile(filepath.Join(promptDir, name), []byte(content), 0644)
	}

	orc := New(cfg, dir, promptDir)

	// Create an issue first
	iss, err := orc.Store().Create("Test Issue", "Test body")
	if err != nil {
		t.Fatalf("Create issue failed: %v", err)
	}

	_, err = orc.CreateTeamAgents(1, iss.ID)
	if err != nil {
		t.Fatalf("CreateTeamAgents failed: %v", err)
	}


}

// mockProcess is a test double for agent.Process.
type mockProcess struct{}

func (m *mockProcess) Send(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

// mockTeamFactory creates agents with mock processes for testing.
type mockTeamFactory struct {
	tmpDir string
}

func newMockTeamFactory(t *testing.T) *mockTeamFactory {
	t.Helper()
	return &mockTeamFactory{tmpDir: t.TempDir()}
}

func (f *mockTeamFactory) CreateTeamAgents(teamNum int, issueID string) (engineer *agent.Agent, err error) {
	makeAgent := func(role agent.Role) *agent.Agent {
		id := agent.AgentID{Role: role, TeamNum: teamNum}
		logPath := filepath.Join(f.tmpDir, fmt.Sprintf("chatlog-%s-%d.txt", role, teamNum))
		os.WriteFile(logPath, nil, 0644)
		return agent.NewAgent(agent.AgentConfig{
			ID:            id,
			Role:          role,
			SystemPrompt:  "test",
			Model:         "test",
			ChatLogPath:   logPath,
			MemosDir:      f.tmpDir,
			ResetInterval: time.Hour,
			OriginalTask:  issueID,
			Process:       &mockProcess{},
		})
	}
	return makeAgent(agent.RoleEngineer),
		nil
}

func TestStartAllTeamsCreatesMaxTeamsUnconditionally(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 3
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.WriteFile(filepath.Join(dir, "chatlog.txt"), nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// No issues exist — should still create 3 standby teams
	orc.startAllTeams(ctx)

	if orc.Teams().Count() != 3 {
		t.Errorf("expected 3 standby teams, got %d", orc.Teams().Count())
	}
}

func TestStartAllTeamsAssignsIssues(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 4
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.WriteFile(filepath.Join(dir, "chatlog.txt"), nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 4)

	// Create: 1 open, 1 in_progress, 1 resolved, 1 closed
	statuses := []issue.Status{issue.StatusOpen, issue.StatusInProgress, issue.StatusResolved, issue.StatusClosed}
	for i, s := range statuses {
		iss, err := orc.Store().Create(fmt.Sprintf("Task %d", i), "body")
		if err != nil {
			t.Fatalf("create issue: %v", err)
		}
		iss.Status = s
		orc.Store().Update(iss)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orc.startAllTeams(ctx)

	// All 4 teams created (2 with issues + 2 standby)
	if orc.Teams().Count() != 4 {
		t.Errorf("expected 4 teams, got %d", orc.Teams().Count())
	}

	// Open and in_progress issues should be assigned
	issues, _ := orc.Store().List(issue.StatusFilter{})
	for _, iss := range issues {
		switch iss.Status {
		case issue.StatusInProgress:
			if iss.AssignedTeam == 0 {
				t.Errorf("in_progress issue %s should have assigned team", iss.ID)
			}
		case issue.StatusResolved, issue.StatusClosed:
			if iss.AssignedTeam != 0 {
				t.Errorf("%s issue %s should not have assigned team", iss.Status, iss.ID)
			}
		}
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

	_, err := orc.CreateTeamAgents(1, "test-issue")
	if err == nil {
		t.Fatal("expected error for missing prompt templates")
	}
}

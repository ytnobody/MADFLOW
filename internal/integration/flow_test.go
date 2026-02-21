package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/config"
	"github.com/ytnobody/madflow/internal/issue"
	"github.com/ytnobody/madflow/internal/orchestrator"
)

// TestIssueToTeamCreateFlow tests the flow from issue creation
// to team creation via orchestrator command handling.
func TestIssueToTeamCreateFlow(t *testing.T) {
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	memosDir := filepath.Join(dir, "memos")
	logPath := filepath.Join(dir, "chatlog.txt")
	os.MkdirAll(issuesDir, 0755)
	os.MkdirAll(memosDir, 0755)
	os.WriteFile(logPath, nil, 0644)

	// Create prompts
	promptDir := t.TempDir()
	for _, name := range []string{"superintendent.md", "pm.md", "release_manager.md", "architect.md", "engineer.md", "reviewer.md"} {
		os.WriteFile(filepath.Join(promptDir, name), []byte("# "+name), 0644)
	}

	cfg := &config.Config{
		Project: config.ProjectConfig{
			Name: "test",
			Repos: []config.RepoConfig{
				{Name: "main", Path: dir},
			},
		},
		Agent: config.AgentConfig{
			ContextResetMinutes: 60,
			Models: config.ModelConfig{
				Superintendent: "test",
				PM:             "test",
				Architect:      "test",
				Engineer:       "test",
				Reviewer:       "test",
				ReleaseManager: "test",
			},
		},
		Branches: config.BranchConfig{
			Main:          "main",
			Develop:       "develop",
			FeaturePrefix: "feature/issue-",
		},
	}

	orc := orchestrator.New(cfg, dir, promptDir)

	// Step 1: Create an issue
	store := orc.Store()
	iss, err := store.Create("認証機能の実装", "JWT トークンベースの認証を実装する")
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if iss.ID != "local-001" {
		t.Errorf("expected local-001, got %s", iss.ID)
	}
	if iss.Status != issue.StatusOpen {
		t.Errorf("expected open status, got %s", iss.Status)
	}

	// Step 2: Simulate PM requesting team creation via orchestrator command
	ctx := t.Context()
	msg := chatlog.Message{
		Sender: "pm",
		Body:   "TEAM_CREATE " + iss.ID,
	}
	orc.HandleCommandForTest(ctx, msg)

	// Step 3: Verify team was created
	teams := orc.Teams()
	if teams.Count() != 1 {
		t.Fatalf("expected 1 team, got %d", teams.Count())
	}

	infos := teams.List()
	if infos[0].IssueID != iss.ID {
		t.Errorf("expected team for %s, got %s", iss.ID, infos[0].IssueID)
	}

	// Step 4: Verify issue was updated
	updated, err := store.Get(iss.ID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if updated.Status != issue.StatusInProgress {
		t.Errorf("expected in_progress status, got %s", updated.Status)
	}
	if updated.AssignedTeam != infos[0].ID {
		t.Errorf("expected assigned_team %d, got %d", infos[0].ID, updated.AssignedTeam)
	}

	// Step 5: Disband team
	disbandMsg := chatlog.Message{
		Sender: "release_manager",
		Body:   "TEAM_DISBAND " + iss.ID,
	}
	orc.HandleCommandForTest(ctx, disbandMsg)

	if teams.Count() != 0 {
		t.Errorf("expected 0 teams after disband, got %d", teams.Count())
	}
}

// TestIssueLifecycle tests the full issue status transitions.
func TestIssueLifecycle(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Create
	iss, err := store.Create("テスト機能", "テスト本文")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if iss.Status != issue.StatusOpen {
		t.Errorf("expected open, got %s", iss.Status)
	}

	// open -> in_progress
	iss.Status = issue.StatusInProgress
	iss.AssignedTeam = 1
	if err := store.Update(iss); err != nil {
		t.Fatalf("update to in_progress: %v", err)
	}

	// Verify persistence
	loaded, _ := store.Get(iss.ID)
	if loaded.Status != issue.StatusInProgress {
		t.Errorf("expected in_progress after reload, got %s", loaded.Status)
	}

	// in_progress -> resolved
	iss.Status = issue.StatusResolved
	store.Update(iss)

	// resolved -> closed
	iss.Status = issue.StatusClosed
	store.Update(iss)

	// Verify final state
	final, _ := store.Get(iss.ID)
	if final.Status != issue.StatusClosed {
		t.Errorf("expected closed, got %s", final.Status)
	}

	// List with filter
	open := issue.StatusOpen
	openIssues, _ := store.List(issue.StatusFilter{Status: &open})
	if len(openIssues) != 0 {
		t.Errorf("expected 0 open issues, got %d", len(openIssues))
	}

	closed := issue.StatusClosed
	closedIssues, _ := store.List(issue.StatusFilter{Status: &closed})
	if len(closedIssues) != 1 {
		t.Errorf("expected 1 closed issue, got %d", len(closedIssues))
	}
}

// TestMultipleIssuesAndTeams tests managing multiple issues with separate teams.
func TestMultipleIssuesAndTeams(t *testing.T) {
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	memosDir := filepath.Join(dir, "memos")
	logPath := filepath.Join(dir, "chatlog.txt")
	os.MkdirAll(issuesDir, 0755)
	os.MkdirAll(memosDir, 0755)
	os.WriteFile(logPath, nil, 0644)

	promptDir := t.TempDir()
	for _, name := range []string{"superintendent.md", "pm.md", "release_manager.md", "architect.md", "engineer.md", "reviewer.md"} {
		os.WriteFile(filepath.Join(promptDir, name), []byte("# "+name), 0644)
	}

	cfg := &config.Config{
		Project: config.ProjectConfig{
			Name:  "test",
			Repos: []config.RepoConfig{{Name: "main", Path: dir}},
		},
		Agent: config.AgentConfig{
			ContextResetMinutes: 60,
			Models: config.ModelConfig{
				Superintendent: "test", PM: "test", Architect: "test",
				Engineer: "test", Reviewer: "test", ReleaseManager: "test",
			},
		},
		Branches: config.BranchConfig{
			Main: "main", Develop: "develop", FeaturePrefix: "feature/issue-",
		},
	}

	orc := orchestrator.New(cfg, dir, promptDir)
	store := orc.Store()
	ctx := t.Context()

	// Create 3 issues
	iss1, _ := store.Create("Issue 1", "Body 1")
	iss2, _ := store.Create("Issue 2", "Body 2")
	iss3, _ := store.Create("Issue 3", "Body 3")

	// Create teams for issue 1 and 2
	orc.HandleCommandForTest(ctx, chatlog.Message{Sender: "pm", Body: "TEAM_CREATE " + iss1.ID})
	orc.HandleCommandForTest(ctx, chatlog.Message{Sender: "pm", Body: "TEAM_CREATE " + iss2.ID})

	teams := orc.Teams()
	if teams.Count() != 2 {
		t.Fatalf("expected 2 teams, got %d", teams.Count())
	}

	// Issue 3 should still be open with no team
	i3, _ := store.Get(iss3.ID)
	if i3.Status != issue.StatusOpen {
		t.Errorf("issue 3 should be open, got %s", i3.Status)
	}
	if i3.AssignedTeam != 0 {
		t.Errorf("issue 3 should have no team, got %d", i3.AssignedTeam)
	}

	// Disband team for issue 1
	orc.HandleCommandForTest(ctx, chatlog.Message{Sender: "rm", Body: "TEAM_DISBAND " + iss1.ID})

	if teams.Count() != 1 {
		t.Errorf("expected 1 team after disband, got %d", teams.Count())
	}

	// Remaining team should be for issue 2
	infos := teams.List()
	if infos[0].IssueID != iss2.ID {
		t.Errorf("remaining team should be for %s, got %s", iss2.ID, infos[0].IssueID)
	}
}

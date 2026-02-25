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
	githubPkg "github.com/ytnobody/madflow/internal/github"
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
			ContextResetMinutes: 15,
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

// waitForTeamCount polls until the team manager reaches the expected count
// or the timeout expires. This is needed because startAllTeams is now
// non-blocking (fire-and-forget goroutines).
func waitForTeamCount(t *testing.T, orc *Orchestrator, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if orc.Teams().Count() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d teams, got %d", want, orc.Teams().Count())
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
	waitForTeamCount(t, orc, 3, 5*time.Second)

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
	waitForTeamCount(t, orc, 4, 5*time.Second)

	// All 4 teams created (2 with issues + 2 standby)
	if orc.Teams().Count() != 4 {
		t.Errorf("expected 4 teams, got %d", orc.Teams().Count())
	}

	// Open and in_progress issues should be assigned
	// Wait briefly for async goroutines to update issue assignments.
	time.Sleep(100 * time.Millisecond)
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

func TestStartAllTeamsSkipsPendingApproval(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 3
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.WriteFile(filepath.Join(dir, "chatlog.txt"), nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)

	// Create 1 regular open issue and 1 pending-approval issue.
	regular, err := orc.Store().Create("Regular Issue", "body")
	if err != nil {
		t.Fatalf("create regular issue: %v", err)
	}

	pending, err := orc.Store().Create("Pending Issue", "body")
	if err != nil {
		t.Fatalf("create pending issue: %v", err)
	}
	pending.PendingApproval = true
	orc.Store().Update(pending)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orc.startAllTeams(ctx)
	waitForTeamCount(t, orc, 3, 5*time.Second)

	// 3 teams should be created (1 for regular + 2 standby)
	if orc.Teams().Count() != 3 {
		t.Errorf("expected 3 teams, got %d", orc.Teams().Count())
	}

	// Wait briefly for async goroutines to update issue assignments.
	time.Sleep(100 * time.Millisecond)

	// Regular issue should be assigned, pending issue should NOT be assigned.
	gotRegular, _ := orc.Store().Get(regular.ID)
	if gotRegular.AssignedTeam == 0 {
		t.Error("regular issue should have assigned team")
	}

	gotPending, _ := orc.Store().Get(pending.ID)
	if gotPending.AssignedTeam != 0 {
		t.Error("pending-approval issue should NOT have assigned team")
	}
}

func TestPruneClosedIssues(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.WriteFile(filepath.Join(dir, "chatlog.txt"), nil, 0644)

	orc := New(cfg, dir, t.TempDir())

	// Create issues with various statuses.
	open, _ := orc.Store().Create("Open", "body")

	closed1, _ := orc.Store().Create("Closed 1", "body")
	closed1.Status = issue.StatusClosed
	orc.Store().Update(closed1)

	resolved, _ := orc.Store().Create("Resolved", "body")
	resolved.Status = issue.StatusResolved
	orc.Store().Update(resolved)

	closed2, _ := orc.Store().Create("Closed 2", "body")
	closed2.Status = issue.StatusClosed
	orc.Store().Update(closed2)

	orc.pruneClosedIssues()

	// Only closed issues should be deleted.
	remaining, err := orc.Store().List(issue.StatusFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining issues (open + resolved), got %d", len(remaining))
	}
	ids := map[string]bool{}
	for _, iss := range remaining {
		ids[iss.ID] = true
	}
	if !ids[open.ID] {
		t.Errorf("open issue %s should remain", open.ID)
	}
	if !ids[resolved.ID] {
		t.Errorf("resolved issue %s should remain", resolved.ID)
	}
}

func TestHandleTeamCreateRejectsClosedIssue(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)

	// Create a closed issue.
	iss, _ := orc.Store().Create("Closed Issue", "body")
	iss.Status = issue.StatusClosed
	orc.Store().Update(iss)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	teamsBefore := orc.Teams().Count()
	orc.handleTeamCreate(ctx, fmt.Sprintf("TEAM_CREATE %s", iss.ID))

	// No new team should be created.
	if orc.Teams().Count() != teamsBefore {
		t.Errorf("expected no new team for closed issue, but team count changed from %d to %d", teamsBefore, orc.Teams().Count())
	}

	// Chatlog should contain a rejection message.
	cl := chatlog.New(chatlogPath)
	msgs, _ := cl.Poll("superintendent")
	found := false
	for _, m := range msgs {
		if contains(m.Body, "拒否されました") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected rejection message in chatlog for closed issue TEAM_CREATE")
	}
}

func TestStartAllTeamsResetsStaleAssignment(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 2
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.WriteFile(filepath.Join(dir, "chatlog.txt"), nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 2)

	// Create an in_progress issue with a stale team assignment.
	iss, _ := orc.Store().Create("Stale Issue", "body")
	iss.Status = issue.StatusInProgress
	iss.AssignedTeam = 99
	orc.Store().Update(iss)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orc.startAllTeams(ctx)
	waitForTeamCount(t, orc, 2, 5*time.Second)

	// Wait briefly for async goroutines to update issue assignments.
	time.Sleep(100 * time.Millisecond)

	// The issue should have been re-assigned (team number != 99, i.e. reset then re-assigned).
	got, _ := orc.Store().Get(iss.ID)
	if got.AssignedTeam == 99 {
		t.Errorf("expected stale AssignedTeam=99 to be reset, but it is still 99")
	}
	if got.AssignedTeam == 0 {
		t.Errorf("expected issue to be re-assigned to a new team, but AssignedTeam is 0")
	}
}

// TestCreateTeamAgentsMissingPrompt verifies that CreateTeamAgents succeeds even
// when the prompts directory contains no files, because agent.LoadPrompt now
// falls back to the embedded default templates bundled in the binary.
func TestCreateTeamAgentsMissingPrompt(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.MkdirAll(filepath.Join(dir, "memos"), 0755)

	// Empty prompt dir - no templates; embedded defaults should be used.
	promptDir := t.TempDir()

	orc := New(cfg, dir, promptDir)

	_, err := orc.CreateTeamAgents(1, "test-issue")
	if err != nil {
		t.Fatalf("expected no error when prompts are missing (should fall back to embedded defaults), got: %v", err)
	}
}

func TestCreateTeamAgentsExtraPrompt(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.ExtraPrompt = "プロジェクト固有の追加指示です。"
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.MkdirAll(filepath.Join(dir, "memos"), 0755)

	// Create prompt templates
	promptDir := t.TempDir()
	basePrompt := "# engineer.md\nAgent: {{AGENT_ID}}"
	os.WriteFile(filepath.Join(promptDir, "engineer.md"), []byte(basePrompt), 0644)

	orc := New(cfg, dir, promptDir)

	engineer, err := orc.CreateTeamAgents(1, "test-issue-extra")
	if err != nil {
		t.Fatalf("CreateTeamAgents failed: %v", err)
	}

	// The system prompt should contain both the base prompt and the extra prompt.
	if !contains(engineer.SystemPrompt, "# engineer.md") {
		t.Error("expected system prompt to contain base prompt content")
	}
	if !contains(engineer.SystemPrompt, cfg.Agent.ExtraPrompt) {
		t.Errorf("expected system prompt to contain extra_prompt %q, got %q", cfg.Agent.ExtraPrompt, engineer.SystemPrompt)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestHandleGitHubEvent verifies that the GitHub event callback correctly filters
// bot comments and comments on closed/resolved issues.
func TestHandleGitHubEvent(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())

	// Helper: read all superintendent-targeted lines from chatlog
	readSuperintendentMessages := func() []string {
		cl := chatlog.New(chatlogPath)
		msgs, _ := cl.Poll("superintendent")
		var result []string
		for _, m := range msgs {
			result = append(result, m.Body)
		}
		return result
	}

	// Create test issues with various statuses
	openIss, _ := orc.store.Create("Open Issue", "body")
	openIss.Status = issue.StatusOpen
	orc.store.Update(openIss)

	resolvedIss, _ := orc.store.Create("Resolved Issue", "body")
	resolvedIss.Status = issue.StatusResolved
	orc.store.Update(resolvedIss)

	closedIss, _ := orc.store.Create("Closed Issue", "body")
	closedIss.Status = issue.StatusClosed
	orc.store.Update(closedIss)

	humanComment := &issue.Comment{ID: 1, Author: "human", Body: "Hey this is a human comment", IsBot: false}
	botComment := &issue.Comment{ID: 2, Author: "ytnobody", Body: "**[実装完了]** by `engineer-1`", IsBot: true}

	tests := []struct {
		name        string
		issueID     string
		comment     *issue.Comment
		wantNotify  bool
		description string
	}{
		{
			name:        "human comment on open issue triggers notification",
			issueID:     openIss.ID,
			comment:     humanComment,
			wantNotify:  true,
			description: "Human comments on open issues must be forwarded to superintendent",
		},
		{
			name:        "bot comment on open issue is suppressed",
			issueID:     openIss.ID,
			comment:     botComment,
			wantNotify:  false,
			description: "Bot comments (IsBot=true) must not reach superintendent",
		},
		{
			name:        "human comment on resolved issue is suppressed",
			issueID:     resolvedIss.ID,
			comment:     humanComment,
			wantNotify:  false,
			description: "Comments on resolved issues must not spam superintendent",
		},
		{
			name:        "human comment on closed issue is suppressed",
			issueID:     closedIss.ID,
			comment:     humanComment,
			wantNotify:  false,
			description: "Comments on closed issues must not spam superintendent",
		},
		{
			name:        "nil comment is ignored",
			issueID:     openIss.ID,
			comment:     nil,
			wantNotify:  false,
			description: "Nil comments must not cause panics or notifications",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset chatlog before each sub-test
			os.WriteFile(chatlogPath, nil, 0644)

			orc.handleGitHubEvent(githubPkg.EventTypeIssueComment, tc.issueID, tc.comment)

			msgs := readSuperintendentMessages()
			notified := len(msgs) > 0
			if notified != tc.wantNotify {
				t.Errorf("%s: got notified=%v, want %v (messages: %v)", tc.description, notified, tc.wantNotify, msgs)
			}
		})
	}
}

// --- PullRequestEvent / handlePRMerged tests ---

func TestHandlePRMerged(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)

	// Create an in-progress issue with an assigned team
	iss := &issue.Issue{
		ID:     "owner-repo-001",
		Title:  "Test Issue",
		URL:    "https://api.github.com/repos/owner/repo/issues/1",
		Status: issue.StatusInProgress,
	}
	orc.Store().Update(iss)

	// Create a team for this issue
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tm, err := orc.Teams().Create(ctx, "owner-repo-001")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	iss.AssignedTeam = tm.ID
	orc.Store().Update(iss)

	// Simulate handlePRMerged
	orc.handlePRMerged("owner-repo-001")

	// Verify issue status is closed
	got, err := orc.Store().Get("owner-repo-001")
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if got.Status != issue.StatusClosed {
		t.Errorf("expected status closed, got %s", got.Status)
	}

	// Verify chatlog notification
	cl := chatlog.New(chatlogPath)
	msgs, _ := cl.Poll("superintendent")
	found := false
	for _, m := range msgs {
		if contains(m.Body, "PR merged") && contains(m.Body, "owner-repo-001") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected chatlog notification about PR merge")
	}
}

func TestHandlePRMerged_AlreadyClosed(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.WriteFile(filepath.Join(dir, "chatlog.txt"), nil, 0644)

	orc := New(cfg, dir, t.TempDir())

	// Create an already-closed issue
	iss := &issue.Issue{
		ID:     "owner-repo-002",
		Title:  "Already Closed",
		Status: issue.StatusClosed,
	}
	orc.Store().Update(iss)

	// Should not panic or change anything
	orc.handlePRMerged("owner-repo-002")

	// Status should remain closed
	got, _ := orc.Store().Get("owner-repo-002")
	if got.Status != issue.StatusClosed {
		t.Errorf("expected status to remain closed, got %s", got.Status)
	}
}

func TestHandlePRMerged_IssueNotFound(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.WriteFile(filepath.Join(dir, "chatlog.txt"), nil, 0644)

	orc := New(cfg, dir, t.TempDir())

	// Should not panic when issue is missing
	orc.handlePRMerged("nonexistent-issue-999")
}

func TestHandleGitHubEvent_PullRequest(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())

	// Create an issue
	iss := &issue.Issue{
		ID:     "owner-repo-005",
		Title:  "Test",
		Status: issue.StatusInProgress,
	}
	orc.Store().Update(iss)

	// Call handleGitHubEvent with PullRequest type
	orc.handleGitHubEvent(githubPkg.EventTypePullRequest, "owner-repo-005", nil)

	// Issue should be closed
	got, _ := orc.Store().Get("owner-repo-005")
	if got.Status != issue.StatusClosed {
		t.Errorf("expected status closed, got %s", got.Status)
	}
}

// TestRunGracefulShutdownDuringStartup verifies that Run returns nil (not an
// error) when the context is cancelled while startAllTeams is still running.
// Previously the system returned "wait for agents ready: context canceled"
// which looked like a crash to the user (GitHub Issue #104).
func TestRunGracefulShutdownDuringStartup(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 2
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.MkdirAll(filepath.Join(dir, "memos"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	// Provide a minimal superintendent prompt so startResidentAgents succeeds.
	promptDir := t.TempDir()
	os.WriteFile(filepath.Join(promptDir, "superintendent.md"), []byte("# superintendent"), 0644)

	orc := New(cfg, dir, promptDir)
	// Replace the team factory with the mock so no real Claude Code processes
	// are spawned during startAllTeams.
	orc.teams = team.NewManager(newMockTeamFactory(t), 2)

	// Cancel the context before Run is called so that the context is already
	// done by the time waitForAgentsReady would be reached.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	err := orc.Run(ctx)
	if err != nil {
		t.Errorf("Run() with pre-cancelled context should return nil, got: %v", err)
	}
}

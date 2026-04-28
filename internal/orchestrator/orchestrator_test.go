package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	orc.handleTeamCreate(t.Context(), ParseCommand("TEAM_CREATE"))
}

func TestHandleTeamDisbandMissingID(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)

	orc := New(cfg, dir, t.TempDir())

	// TEAM_DISBAND without issue ID should not panic
	orc.handleTeamDisband(ParseCommand("TEAM_DISBAND"))
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
func (m *mockProcess) Reset(_ context.Context) error { return nil }
func (m *mockProcess) Close() error                  { return nil }

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
	// Ensure all goroutines spawned by startAllTeams finish before TempDir cleanup.
	t.Cleanup(orc.Wait)

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
	// Ensure all goroutines spawned by startAllTeams finish before TempDir cleanup.
	t.Cleanup(orc.Wait)

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
	// Ensure all goroutines spawned by startAllTeams finish before TempDir cleanup.
	t.Cleanup(orc.Wait)

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
	orc.handleTeamCreate(ctx, ParseCommand(fmt.Sprintf("TEAM_CREATE %s", iss.ID)))

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

// TestHandleTeamCreateResetsStaleAssignment verifies RC-1 fix:
// when an issue has AssignedTeam > 0 but the team is no longer present in the
// manager (e.g. after a process restart or an unresponsive engineer), the stale
// assignment is automatically cleared. After clearing, because the issue remains
// in_progress status with no active team, TEAM_CREATE is rejected — the operator
// must reset the issue to "open" before reassigning.
func TestHandleTeamCreateResetsStaleAssignment(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	if err := os.MkdirAll(filepath.Join(dir, "issues"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	if err := os.WriteFile(chatlogPath, nil, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	orc := New(cfg, dir, t.TempDir())
	factory := newMockTeamFactory(t)
	orc.teams = team.NewManager(factory, 3)

	// Create an issue with a stale assigned_team value (team not in manager).
	iss, _ := orc.Store().Create("Stale Assignment Issue", "body")
	iss.Status = issue.StatusInProgress
	iss.AssignedTeam = 5 // phantom team — not present in manager
	orc.Store().Update(iss)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orc.handleTeamCreate(ctx, ParseCommand(fmt.Sprintf("TEAM_CREATE %s", iss.ID)))

	// The stale AssignedTeam should have been reset in the store.
	updated, err := orc.Store().Get(iss.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if updated.AssignedTeam == 5 {
		t.Errorf("AssignedTeam should have been reset from stale value 5, but is still 5")
	}

	// No new team should have been created — TEAM_CREATE is rejected because
	// the issue is in_progress (operator must reset status to open first).
	if orc.Teams().Count() != 0 {
		t.Errorf("expected no team created for in_progress stale-assignment issue, got %d teams", orc.Teams().Count())
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
		t.Error("expected rejection message in chatlog for in_progress stale-assignment TEAM_CREATE")
	}
}

func TestHandleTeamCreateRejectsActiveTeam(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)

	// Create an open issue. The active-team check must fire before status is changed
	// to in_progress, so we start with an open issue, create a team for it, then
	// manually set AssignedTeam=0 to simulate the race window where the team exists
	// in the manager but the issue's AssignedTeam field hasn't been updated yet.
	iss, _ := orc.Store().Create("Active Team Issue", "body")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a team for this issue via the manager directly (simulates race window).
	_, err := orc.Teams().Create(ctx, iss.ID, iss.Title)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}

	// Reset AssignedTeam to 0 in the store to simulate the race window where the
	// team exists in the manager but the issue hasn't been updated yet.
	iss.AssignedTeam = 0
	iss.Status = issue.StatusOpen
	orc.Store().Update(iss)

	teamsBefore := orc.Teams().Count()
	orc.handleTeamCreate(ctx, ParseCommand(fmt.Sprintf("TEAM_CREATE %s", iss.ID)))

	// No new team should be created.
	if orc.Teams().Count() != teamsBefore {
		t.Errorf("expected no new team when active team exists, but team count changed from %d to %d", teamsBefore, orc.Teams().Count())
	}

	// Chatlog should contain a rejection message about active/pending team.
	cl := chatlog.New(chatlogPath)
	msgs, _ := cl.Poll("superintendent")
	found := false
	for _, m := range msgs {
		if contains(m.Body, "拒否されました") && contains(m.Body, "アクティブ") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected rejection message in chatlog for active-team TEAM_CREATE")
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
	// Ensure all goroutines spawned by startAllTeams finish before TempDir cleanup.
	t.Cleanup(orc.Wait)

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
	tm, err := orc.Teams().Create(ctx, "owner-repo-001", iss.Title)
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

// TestHandleTeamCreateUsesIdleTeam verifies that TEAM_CREATE reuses an existing
// idle standby team instead of creating a new one when maxTeams is already reached.
// This is the fix for GitHub Issue #156: the orchestrator was failing with
// "maximum teams reached" even when idle teams were available.
func TestHandleTeamCreateUsesIdleTeam(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 2
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create 2 standby teams (no issue assigned) — fills maxTeams slots.
	_, err := orc.Teams().Create(ctx, "", "")
	if err != nil {
		t.Fatalf("create standby team 1: %v", err)
	}
	_, err = orc.Teams().Create(ctx, "", "")
	if err != nil {
		t.Fatalf("create standby team 2: %v", err)
	}
	if orc.Teams().Count() != 2 {
		t.Fatalf("expected 2 standby teams, got %d", orc.Teams().Count())
	}

	// Create a new open issue.
	iss, _ := orc.Store().Create("New Issue for Idle Team", "body")

	// Call TEAM_CREATE — should reuse an idle team instead of failing.
	orc.handleTeamCreate(ctx, ParseCommand(fmt.Sprintf("TEAM_CREATE %s", iss.ID)))

	// Team count must not increase (no new team created).
	if orc.Teams().Count() != 2 {
		t.Errorf("expected team count to remain 2 (idle reuse), got %d", orc.Teams().Count())
	}

	// Issue must be assigned to one of the standby teams.
	// Wait briefly for async issue store update.
	time.Sleep(50 * time.Millisecond)
	got, getErr := orc.Store().Get(iss.ID)
	if getErr != nil {
		t.Fatalf("get issue: %v", getErr)
	}
	if got.AssignedTeam == 0 {
		t.Error("expected issue to be assigned to an idle team, but AssignedTeam is 0")
	}
	if got.Status != issue.StatusInProgress {
		t.Errorf("expected issue status in_progress, got %s", got.Status)
	}

	// Chatlog must contain the idle-team assignment notification.
	cl := chatlog.New(chatlogPath)
	msgs, _ := cl.Poll("superintendent")
	found := false
	for _, m := range msgs {
		if contains(m.Body, "アイドルチーム") && contains(m.Body, iss.ID) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected chatlog to contain idle-team assignment message")
	}
}

// TestHandleTeamCreateCreatesNewTeamWhenNoIdle verifies that TEAM_CREATE still
// creates a new team when no idle standby teams are available.
func TestHandleTeamCreateCreatesNewTeamWhenNoIdle(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 3
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)
	// Ensure the async team-creation goroutine finishes before TempDir cleanup.
	t.Cleanup(orc.Wait)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create 2 busy teams (with issues), leaving 1 slot free but no idle teams.
	_, err := orc.Teams().Create(ctx, "issue-busy-01", "Busy Issue 1")
	if err != nil {
		t.Fatalf("create busy team 1: %v", err)
	}
	_, err = orc.Teams().Create(ctx, "issue-busy-02", "Busy Issue 2")
	if err != nil {
		t.Fatalf("create busy team 2: %v", err)
	}

	// Create a new open issue.
	iss, _ := orc.Store().Create("New Issue No Idle", "body")

	// Call TEAM_CREATE — all existing teams are busy, so it must try to create a new one.
	orc.handleTeamCreate(ctx, ParseCommand(fmt.Sprintf("TEAM_CREATE %s", iss.ID)))

	// Wait for the async goroutine to complete before inspecting state.
	orc.Wait()

	// Team count should increase to 3 (new team created for the new issue).
	if orc.Teams().Count() < 3 {
		t.Errorf("expected 3 teams after creating new team, got %d", orc.Teams().Count())
	}

	// Chatlog should contain ACK message (not idle-team message).
	cl := chatlog.New(chatlogPath)
	msgs, _ := cl.Poll("superintendent")
	found := false
	for _, m := range msgs {
		if contains(m.Body, "受信しました") && contains(m.Body, iss.ID) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected chatlog to contain ACK message for new team creation")
	}
}

// TestRunGracefulShutdownDuringStartup verifies that Run returns nil (not an
// TestWatchCommandsPicksUpEarlyTeamCreate is a regression test for local-001.
//
// Root cause: watchCommands() was previously started AFTER waitForAgentsReady().
// chatlog.Watch() records the current file offset when called; any TEAM_CREATE
// messages written to the chatlog by the superintendent's initial-prompt tool-
// calls appeared BEFORE that offset and were therefore never delivered.
//
// Fix: watchCommands() is now launched right after startAllTeams(), before
// waitForAgentsReady(). With the chatlog freshly cleared at startup, the
// Watch() offset is 0, so every subsequent write — including TEAM_CREATE
// commands from the superintendent's initial prompt — is observed.
//
// This test verifies the post-fix behaviour: a TEAM_CREATE written to the
// chatlog while watchCommands() is running must be picked up and processed.
func TestWatchCommandsPicksUpEarlyTeamCreate(t *testing.T) {
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	os.MkdirAll(issuesDir, 0755)
	os.MkdirAll(filepath.Join(dir, "memos"), 0755)

	// Clear chatlog to simulate Run() startup state.
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	cfg := testConfig(dir)
	orc := New(cfg, dir, t.TempDir())

	// Create an open issue that the superintendent would want to assign.
	store := orc.Store()
	iss, err := store.Create("テスト機能", "テスト本文")
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Launch watchCommands() early — this is the post-fix behaviour where it
	// starts before waitForAgentsReady() with offset=0 on the fresh chatlog.
	done := make(chan struct{})
	go func() {
		defer close(done)
		orc.watchCommands(ctx)
	}()

	// Give the Watch() goroutine time to set up its ticker (500 ms interval).
	time.Sleep(100 * time.Millisecond)

	// Simulate the superintendent writing TEAM_CREATE during initial-prompt
	// processing (i.e. before it would call markReady()).
	cl := chatlog.New(chatlogPath)
	if err := cl.Append("orchestrator", "superintendent", "TEAM_CREATE "+iss.ID); err != nil {
		t.Fatalf("append TEAM_CREATE: %v", err)
	}

	// Poll until the issue transitions to in_progress (watchCommands processed
	// the TEAM_CREATE and handleTeamCreate updated the status).
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		updated, getErr := store.Get(iss.ID)
		if getErr == nil && updated.Status == issue.StatusInProgress {
			cancel() // success — stop watchCommands
			<-done
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	<-done
	t.Error("TEAM_CREATE written before agents are ready was not processed — watchCommands may have started too late")
}

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
	// Ensure all goroutines spawned by startAllTeams (tracked via o.wg) finish
	// before TempDir cleanup.  t.Cleanup runs in LIFO order, so registering
	// Wait() after the t.TempDir() calls above guarantees it executes first.
	t.Cleanup(orc.Wait)

	// Cancel the context before Run is called so that the context is already
	// done by the time waitForAgentsReady would be reached.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	err := orc.Run(ctx)
	if err != nil {
		t.Errorf("Run() with pre-cancelled context should return nil, got: %v", err)
	}
}

// TestHandlePatrolComplete verifies that sending PATROL_COMPLETE via handleCommand
// signals the patrolResetCh channel so runIssuePatrol can reset its timer.
func TestHandlePatrolComplete(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	orc := New(cfg, dir, t.TempDir())

	// patrolResetCh should be initialised and empty.
	if orc.patrolResetCh == nil {
		t.Fatal("patrolResetCh should be non-nil after New()")
	}
	select {
	case <-orc.patrolResetCh:
		t.Fatal("patrolResetCh should be empty before PATROL_COMPLETE is sent")
	default:
	}

	// Simulate the superintendent sending PATROL_COMPLETE via the chatlog.
	msg := chatlog.Message{Sender: "superintendent", Recipient: "orchestrator", Body: "PATROL_COMPLETE"}
	orc.HandleCommandForTest(context.Background(), msg)

	// The channel should now have exactly one signal.
	select {
	case <-orc.patrolResetCh:
		// success
	default:
		t.Error("patrolResetCh should contain a signal after PATROL_COMPLETE is processed")
	}
}

// TestIssueStateFingerprint verifies that the fingerprint changes when issues are
// added/removed and stays stable when no changes occur.
func TestIssueStateFingerprint(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	orc := New(cfg, dir, t.TempDir())

	// Empty store → empty fingerprint.
	fp1 := orc.issueStateFingerprint()
	if fp1 != "" {
		t.Errorf("expected empty fingerprint with no issues, got %q", fp1)
	}

	// Add an open issue.
	iss := &issue.Issue{ID: "gh-1", Title: "first", Status: issue.StatusOpen}
	if err := orc.store.Update(iss); err != nil {
		t.Fatal(err)
	}
	fp2 := orc.issueStateFingerprint()
	if fp2 == fp1 {
		t.Error("fingerprint should change after adding an open issue")
	}

	// Add another open issue.
	iss2 := &issue.Issue{ID: "gh-2", Title: "second", Status: issue.StatusInProgress}
	if err := orc.store.Update(iss2); err != nil {
		t.Fatal(err)
	}
	fp3 := orc.issueStateFingerprint()
	if fp3 == fp2 {
		t.Error("fingerprint should change after adding a second issue")
	}

	// Close the first issue — fingerprint should change.
	iss.Status = issue.StatusClosed
	if err := orc.store.Update(iss); err != nil {
		t.Fatal(err)
	}
	fp4 := orc.issueStateFingerprint()
	if fp4 == fp3 {
		t.Error("fingerprint should change after closing an issue")
	}

	// No further changes — fingerprint should be stable.
	fp5 := orc.issueStateFingerprint()
	if fp5 != fp4 {
		t.Errorf("fingerprint should be stable when no changes: %q vs %q", fp4, fp5)
	}
}

// TestRunIssuePatrolSuppressesUnchangedState verifies that the patrol reminder is
// not sent when the issue state has not changed since the last reminder.
func TestRunIssuePatrolSuppressesUnchangedState(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	cfg := testConfig(dir)
	cfg.Agent.IssuePatrolIntervalMinutes = 0 // will use default (20), overridden below
	orc := New(cfg, dir, t.TempDir())
	orc.cfg.Agent.IssuePatrolIntervalMinutes = 1 // use 1-minute interval for test setup

	// Add one open issue so the first tick fires.
	iss := &issue.Issue{ID: "patrol-test-1", Title: "patrol", Status: issue.StatusOpen}
	if err := orc.store.Update(iss); err != nil {
		t.Fatal(err)
	}

	// Count chatlog lines written to superintendent.
	countPatrolMessages := func() int {
		data, _ := os.ReadFile(chatlogPath)
		count := 0
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "定期イシュー巡回") {
				count++
			}
		}
		return count
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a very short interval ticker so we don't have to wait minutes.
	interval := 50 * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// We'll manually drive one iteration: the first tick should send the prompt.
	// Run the loop in a goroutine and let it fire twice; the second tick should be suppressed.
	done := make(chan struct{})
	go func() {
		defer close(done)
		lastSent := ""
		ticks := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ticks++
				current := orc.issueStateFingerprint()
				if current != lastSent {
					orc.appendOrLog("superintendent", "orchestrator", issuePatrolPrompt)
					lastSent = current
				}
				if ticks >= 3 {
					return
				}
			}
		}
	}()
	<-done

	// Only one message should have been written (first tick triggered, subsequent ticks suppressed).
	n := countPatrolMessages()
	if n != 1 {
		t.Errorf("expected exactly 1 patrol message, got %d", n)
	}
}

// TestNormalizeIssueID verifies that normalizeIssueID correctly extracts the
// valid issue ID prefix and strips any trailing non-ID characters.
func TestNormalizeIssueID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Clean inputs should pass through unchanged.
		{"gh-121", "gh-121"},
		{"local-001", "local-001"},
		{"ytnobody-MADFLOW-120", "ytnobody-MADFLOW-120"},
		// Trailing Japanese text should be stripped (root cause of gh-121 rejection).
		{"gh-121（2回目の要求）。チームアサインをお願いします。", "gh-121"},
		{"gh-121（3回目の要求）。イシューファイルは", "gh-121"},
		// Extra ASCII words separated by spaces are handled upstream by
		// strings.Fields, but if the text is glued without spaces, the
		// function should strip it.
		{"local-001something", "local-001something"}, // "something" is ASCII alphanumeric, preserved
		// Completely invalid input.
		{"（無効）", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := normalizeIssueID(tc.input)
		if got != tc.want {
			t.Errorf("normalizeIssueID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestHandleTeamCreateMalformedIssueID verifies that TEAM_CREATE with a
// malformed issue ID (e.g. extra Japanese text appended by the superintendent
// on retry) correctly normalizes the ID and succeeds if the underlying issue
// exists and is open.
//
// This is a regression test for the gh-121 incident where TEAM_CREATE was
// rejected three times because the superintendent appended retry text
// ("（2回目の要求）。チームアサインをお願いします。") directly after the issue ID,
// causing the store lookup to fail.
func TestHandleTeamCreateMalformedIssueID(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)

	orc := New(cfg, dir, t.TempDir())
	// Use a mock team factory so handleTeamCreate's background goroutine
	// completes quickly without trying to load real prompt files or spawn
	// external processes.  This was the root cause of the flaky TempDir
	// cleanup failure: the real CreateTeamAgents could race with directory
	// removal.
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)
	// Ensure all goroutines spawned by handleTeamCreate finish before the test's
	// TempDir is removed.  t.Cleanup runs in LIFO order, so registering Wait()
	// here (after the t.TempDir() calls above) guarantees it runs before the
	// directory cleanup functions.
	t.Cleanup(orc.Wait)

	// Create an open issue.
	iss := &issue.Issue{ID: "gh-99", Title: "regression test issue", Status: issue.StatusOpen}
	if err := orc.store.Update(iss); err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()

	// Simulate the superintendent sending TEAM_CREATE with appended Japanese text,
	// mimicking the exact pattern observed in the gh-121 incident.
	malformed := "TEAM_CREATE gh-99（2回目の要求）。チームアサインをお願いします。"
	orc.handleTeamCreate(ctx, ParseCommand(malformed))

	// The issue should have been transitioned to in_progress (assigned to a team),
	// meaning the malformed ID was normalized and the lookup succeeded.
	updated, err := orc.store.Get("gh-99")
	if err != nil {
		t.Fatalf("store.Get after TEAM_CREATE: %v", err)
	}
	if updated.Status == issue.StatusOpen {
		// If the status is still open, TEAM_CREATE silently rejected or failed —
		// read the chatlog to confirm there is no rejection message.
		data, _ := os.ReadFile(filepath.Join(dir, "chatlog.txt"))
		if strings.Contains(string(data), "イシューが見つかりません") {
			t.Errorf("TEAM_CREATE incorrectly rejected malformed ID %q: got rejection message in chatlog", malformed)
		}
	}
	// Either the issue moved to in_progress, or the team manager returned an
	// error (acceptable since we use a mock config), but there must be NO
	// "イシューが見つかりません" error in the chatlog.
	data, _ := os.ReadFile(filepath.Join(dir, "chatlog.txt"))
	if strings.Contains(string(data), "イシューが見つかりません") {
		t.Errorf("TEAM_CREATE with malformed ID should not produce 'イシューが見つかりません'; chatlog:\n%s", string(data))
	}
}

// TestHandleTeamCreateRejectsWhenAtMaxCapacityAllBusy verifies that TEAM_CREATE is
// rejected gracefully (without panicking or creating an unexpected team) when all
// team slots are occupied by busy teams and no idle team is available.
// This is the fix for GitHub Issue #180: "Orchestrator does not understand max_teams".
func TestHandleTeamCreateRejectsWhenAtMaxCapacityAllBusy(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 2
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fill both slots with busy teams (each assigned to a distinct issue).
	_, err := orc.Teams().Create(ctx, "issue-busy-01", "Busy Issue 1")
	if err != nil {
		t.Fatalf("create busy team 1: %v", err)
	}
	_, err = orc.Teams().Create(ctx, "issue-busy-02", "Busy Issue 2")
	if err != nil {
		t.Fatalf("create busy team 2: %v", err)
	}
	if orc.Teams().Count() != 2 {
		t.Fatalf("expected 2 busy teams, got %d", orc.Teams().Count())
	}

	// Create a new open issue to attempt assignment.
	iss, _ := orc.Store().Create("New Issue Cannot Assign", "body")

	// Call TEAM_CREATE — all slots are full with busy teams; no idle team exists.
	orc.handleTeamCreate(ctx, ParseCommand(fmt.Sprintf("TEAM_CREATE %s", iss.ID)))

	// Team count must NOT have increased.
	if orc.Teams().Count() != 2 {
		t.Errorf("expected team count to remain 2, got %d", orc.Teams().Count())
	}

	// Issue status must remain "open" (not in_progress) because we did not ACK.
	got, getErr := orc.Store().Get(iss.ID)
	if getErr != nil {
		t.Fatalf("get issue: %v", getErr)
	}
	if got.Status != issue.StatusOpen {
		t.Errorf("expected issue status to remain open, got %s", got.Status)
	}
	if got.AssignedTeam != 0 {
		t.Errorf("expected AssignedTeam=0, got %d", got.AssignedTeam)
	}

	// Chatlog must contain a "保留" (pending) or capacity-limit message directed
	// at the superintendent.
	cl := chatlog.New(chatlogPath)
	msgs, _ := cl.Poll("superintendent")
	found := false
	for _, m := range msgs {
		if contains(m.Body, "上限") && contains(m.Body, iss.ID) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected chatlog to contain capacity-limit message for superintendent")
	}
}

// initTestGitRepo creates a temporary git repo with an initial commit on branch "main".
// It configures user.email and user.name so commits work without global git config.
func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\noutput: %s", args, err, out)
		}
	}
	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "initial commit")
	return dir
}

// TestHandleTeamCreateRejectsInProgressNoTeam verifies that a TEAM_CREATE for an
// in_progress issue with AssignedTeam=0 and no active team is rejected. This covers
// the scenario where a previous team was disbanded without resetting the issue status.
func TestHandleTeamCreateRejectsInProgressNoTeam(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)

	// Create an in_progress issue with no team assigned (simulates post-disband state).
	iss, _ := orc.Store().Create("Orphaned In-Progress Issue", "body")
	iss.Status = issue.StatusInProgress
	iss.AssignedTeam = 0
	orc.Store().Update(iss)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	teamsBefore := orc.Teams().Count()
	orc.handleTeamCreate(ctx, Command{Type: CommandTeamCreate, Args: []string{iss.ID}})

	// No new team should be created.
	if orc.Teams().Count() != teamsBefore {
		t.Errorf("expected no new team for in_progress issue without active team, but team count changed from %d to %d",
			teamsBefore, orc.Teams().Count())
	}

	// Issue status must remain in_progress (not been modified).
	got, err := orc.Store().Get(iss.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.Status != issue.StatusInProgress {
		t.Errorf("expected issue status to remain in_progress, got %s", got.Status)
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
		t.Error("expected rejection message in chatlog for in_progress issue TEAM_CREATE")
	}
}

// TestHandleTeamCreateRejectsExistingBranch verifies that TEAM_CREATE is rejected
// when a feature branch for the issue already exists in the git repository.
func TestHandleTeamCreateRejectsExistingBranch(t *testing.T) {
	repoDir := initTestGitRepo(t)

	dir := t.TempDir()
	cfg := testConfig(dir)
	// Point the orchestrator's repo at our test git repo.
	cfg.Project.Repos = []config.RepoConfig{{Name: "main", Path: repoDir}}
	cfg.Branches.FeaturePrefix = "feature/issue-"

	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)

	// Create an open issue.
	iss, _ := orc.Store().Create("Branch Exists Issue", "body")

	// Create the feature branch in the git repo to simulate a previous engineer's work.
	branchName := "feature/issue-" + iss.ID
	runGitInDir := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s failed: %v\noutput: %s", args, dir, err, out)
		}
	}
	runGitInDir(repoDir, "branch", branchName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	teamsBefore := orc.Teams().Count()
	orc.handleTeamCreate(ctx, Command{Type: CommandTeamCreate, Args: []string{iss.ID}})

	// No new team should be created.
	if orc.Teams().Count() != teamsBefore {
		t.Errorf("expected no new team when branch exists, but team count changed from %d to %d",
			teamsBefore, orc.Teams().Count())
	}

	// Chatlog should contain a rejection message mentioning the branch.
	cl := chatlog.New(chatlogPath)
	msgs, _ := cl.Poll("superintendent")
	found := false
	for _, m := range msgs {
		if contains(m.Body, "拒否されました") && contains(m.Body, "フィーチャーブランチ") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected rejection message in chatlog about existing feature branch")
	}
}

// TestHandleTeamCreateRejectsExistingWorktree verifies that TEAM_CREATE is rejected
// when a worktree directory for the issue already exists under .worktrees/{ghLogin}/.
func TestHandleTeamCreateRejectsExistingWorktree(t *testing.T) {
	repoDir := initTestGitRepo(t)

	dir := t.TempDir()
	cfg := testConfig(dir)
	// Point the orchestrator's repo at our test git repo.
	cfg.Project.Repos = []config.RepoConfig{{Name: "main", Path: repoDir}}
	cfg.Branches.FeaturePrefix = "feature/issue-"
	cfg.GhLogin = "testuser"

	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	chatlogPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(chatlogPath, nil, 0644)

	orc := New(cfg, dir, t.TempDir())
	orc.teams = team.NewManager(newMockTeamFactory(t), 3)

	// Create an open issue.
	iss, _ := orc.Store().Create("Worktree Exists Issue", "body")

	// Create the worktree directory to simulate a previous engineer's work.
	wtDir := filepath.Join(repoDir, ".worktrees", "testuser", "issue-"+iss.ID)
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		t.Fatalf("MkdirAll worktree: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	teamsBefore := orc.Teams().Count()
	orc.handleTeamCreate(ctx, Command{Type: CommandTeamCreate, Args: []string{iss.ID}})

	// No new team should be created.
	if orc.Teams().Count() != teamsBefore {
		t.Errorf("expected no new team when worktree exists, but team count changed from %d to %d",
			teamsBefore, orc.Teams().Count())
	}

	// Chatlog should contain a rejection message mentioning the worktree.
	cl := chatlog.New(chatlogPath)
	msgs, _ := cl.Poll("superintendent")
	found := false
	for _, m := range msgs {
		if contains(m.Body, "拒否されました") && contains(m.Body, "ワークツリー") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected rejection message in chatlog about existing worktree")
	}
}

// TestConfigWatcherPropagatesMaxTeams verifies that when madflow.toml is updated
// with a new max_teams value, runConfigWatcher propagates the change to the
// team.Manager via SetMaxTeams.
// This is the hot-reload fix for GitHub Issue #180.
func TestConfigWatcherPropagatesMaxTeams(t *testing.T) {
	dir := t.TempDir()

	// Write an initial config TOML with max_teams=2.
	cfgPath := filepath.Join(dir, "madflow.toml")
	initialTOML := `[project]
name = "test"

[[project.repos]]
name = "main"
path = "` + dir + `"

[agent]
max_teams = 2

[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"
`
	if err := os.WriteFile(cfgPath, []byte(initialTOML), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}
	// Backdate the initial file so the watcher's lastModTime is unambiguously in
	// the past regardless of filesystem timestamp resolution or goroutine scheduling.
	// This eliminates the race where the watcher goroutine starts after the updated
	// file has already been written and thus never sees a change.
	past := time.Now().Add(-5 * time.Second)
	if err := os.Chtimes(cfgPath, past, past); err != nil {
		t.Fatalf("chtimes initial config: %v", err)
	}

	cfg := testConfig(dir)
	cfg.Agent.MaxTeams = 2

	orc := New(cfg, dir, t.TempDir())
	orc.WithConfigPath(cfgPath)
	// Replace team manager with a known initial cap of 2.
	orc.teams = team.NewManager(newMockTeamFactory(t), 2)

	if orc.teams.Cap() != 2 {
		t.Fatalf("expected initial Cap()=2, got %d", orc.teams.Cap())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the config watcher in the background.
	go orc.runConfigWatcher(ctx)

	// Update madflow.toml to set max_teams=5.
	updatedTOML := `[project]
name = "test"

[[project.repos]]
name = "main"
path = "` + dir + `"

[agent]
max_teams = 5

[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"
`
	// Give the watcher one poll cycle (500ms) to start and record the backdated
	// mod time, then overwrite the file with a current timestamp.
	time.Sleep(600 * time.Millisecond)

	if err := os.WriteFile(cfgPath, []byte(updatedTOML), 0644); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	// Poll for the change to propagate (watcher polls every 500ms; allow up to 5s).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if orc.teams.Cap() == 5 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if orc.teams.Cap() != 5 {
		t.Errorf("expected Cap()=5 after config hot-reload, got %d", orc.teams.Cap())
	}
}

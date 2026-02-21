package team

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ytnobody/madflow/internal/agent"
)

// mockFactory is a test double for TeamFactory.
// It creates Agent structs with valid ChatLog and Process fields
// so that the goroutines started by Manager.Create can run briefly
// without panicking (they will fail on Process.Send and exit via
// context cancellation).
type mockFactory struct {
	shouldFail bool
	tmpDir     string // temp dir for chatlog files
}

func newMockFactory(t *testing.T) *mockFactory {
	t.Helper()
	return &mockFactory{tmpDir: t.TempDir()}
}

func (m *mockFactory) CreateTeamAgents(teamNum int, issueID string, workDir string) (architect, engineer, reviewer *agent.Agent, err error) {
	if m.shouldFail {
		return nil, nil, nil, fmt.Errorf("factory error")
	}

	makeAgent := func(role agent.Role) *agent.Agent {
		id := agent.AgentID{Role: role, TeamNum: teamNum}
		logPath := filepath.Join(m.tmpDir, fmt.Sprintf("chatlog-%s-%d.txt", role, teamNum))
		// Create an empty chatlog file
		os.WriteFile(logPath, nil, 0644)

		return agent.NewAgent(agent.AgentConfig{
			ID:            id,
			Role:          role,
			SystemPrompt:  "test",
			Model:         "test",
			WorkDir:       m.tmpDir,
			ChatLogPath:   logPath,
			MemosDir:      m.tmpDir,
			ResetInterval: time.Hour,
			OriginalTask:  issueID,
		})
	}

	return makeAgent(agent.RoleArchitect),
		makeAgent(agent.RoleEngineer),
		makeAgent(agent.RoleReviewer),
		nil
}

// createAndCancel is a helper that creates a team within a pre-cancelled context
// so the Agent.Run goroutines exit immediately without trying to invoke claude.
func createAndCancel(t *testing.T, mgr *Manager, issueID string) *Team {
	t.Helper()
	// Cancel the context before Create so goroutines exit immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Create

	team, err := mgr.Create(ctx, issueID)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Give goroutines a moment to observe the cancelled context
	runtime.Gosched()
	return team
}

func TestNewManager(t *testing.T) {
	m := NewManager(newMockFactory(t))
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 teams, got %d", m.Count())
	}
}

func TestCreate(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory)

	team := createAndCancel(t, m, "issue-001")

	if team.ID != 1 {
		t.Errorf("expected team ID 1, got %d", team.ID)
	}
	if team.IssueID != "issue-001" {
		t.Errorf("expected issue ID issue-001, got %s", team.IssueID)
	}
	if team.Architect == nil || team.Engineer == nil || team.Reviewer == nil {
		t.Error("expected all three agents to be non-nil")
	}
}

func TestCreateIncrementsID(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory)

	t1 := createAndCancel(t, m, "issue-001")
	t2 := createAndCancel(t, m, "issue-002")

	if t1.ID != 1 {
		t.Errorf("expected first team ID 1, got %d", t1.ID)
	}
	if t2.ID != 2 {
		t.Errorf("expected second team ID 2, got %d", t2.ID)
	}
}

func TestCreateFactoryError(t *testing.T) {
	factory := newMockFactory(t)
	factory.shouldFail = true
	m := NewManager(factory)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Create(ctx, "issue-001")
	if err == nil {
		t.Fatal("expected error from factory, got nil")
	}
}

func TestCount(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory)

	if m.Count() != 0 {
		t.Errorf("expected 0 teams initially, got %d", m.Count())
	}

	createAndCancel(t, m, "issue-001")
	if m.Count() != 1 {
		t.Errorf("expected 1 team, got %d", m.Count())
	}

	createAndCancel(t, m, "issue-002")
	if m.Count() != 2 {
		t.Errorf("expected 2 teams, got %d", m.Count())
	}
}

func TestList(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory)

	// Empty list
	infos := m.List()
	if len(infos) != 0 {
		t.Errorf("expected empty list, got %d items", len(infos))
	}

	createAndCancel(t, m, "issue-001")
	createAndCancel(t, m, "issue-002")

	infos = m.List()
	if len(infos) != 2 {
		t.Errorf("expected 2 items, got %d", len(infos))
	}

	// Check that both issues appear (order may vary since map iteration is random)
	issueIDs := map[string]bool{}
	for _, info := range infos {
		issueIDs[info.IssueID] = true
	}
	if !issueIDs["issue-001"] {
		t.Error("expected issue-001 in list")
	}
	if !issueIDs["issue-002"] {
		t.Error("expected issue-002 in list")
	}
}

func TestDisband(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory)

	team := createAndCancel(t, m, "issue-001")

	if m.Count() != 1 {
		t.Fatalf("expected 1 team before disband, got %d", m.Count())
	}

	if err := m.Disband(team.ID); err != nil {
		t.Fatalf("Disband failed: %v", err)
	}

	if m.Count() != 0 {
		t.Errorf("expected 0 teams after disband, got %d", m.Count())
	}
}

func TestDisbandNotFound(t *testing.T) {
	m := NewManager(newMockFactory(t))

	err := m.Disband(999)
	if err == nil {
		t.Fatal("expected error for non-existent team, got nil")
	}
}

func TestDisbandByIssue(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory)

	createAndCancel(t, m, "issue-001")
	createAndCancel(t, m, "issue-002")

	if err := m.DisbandByIssue("issue-001"); err != nil {
		t.Fatalf("DisbandByIssue failed: %v", err)
	}

	if m.Count() != 1 {
		t.Errorf("expected 1 remaining team, got %d", m.Count())
	}

	// The remaining team should be issue-002
	infos := m.List()
	if len(infos) != 1 || infos[0].IssueID != "issue-002" {
		t.Errorf("expected issue-002 to remain, got %v", infos)
	}
}

func TestDisbandByIssueNotFound(t *testing.T) {
	m := NewManager(newMockFactory(t))

	err := m.DisbandByIssue("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

// --- Persistence tests ---

// mockIssueChecker is a test double for IssueChecker.
type mockIssueChecker struct {
	finished map[string]bool
}

func (c *mockIssueChecker) IsFinished(issueID string) bool {
	return c.finished[issueID]
}

func TestCreatePersistsState(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "teams.toml")
	factory := newMockFactory(t)
	m := NewManagerWithState(factory, stateFile)

	createAndCancel(t, m, "issue-001")

	// Verify the state file exists and has correct content
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var state teamState
	if err := toml.Unmarshal(data, &state); err != nil {
		t.Fatalf("parse state file: %v", err)
	}

	if state.NextID != 2 {
		t.Errorf("expected next_id 2, got %d", state.NextID)
	}
	if len(state.Teams) != 1 {
		t.Fatalf("expected 1 team entry, got %d", len(state.Teams))
	}
	if state.Teams[0].ID != 1 {
		t.Errorf("expected team ID 1, got %d", state.Teams[0].ID)
	}
	if state.Teams[0].IssueID != "issue-001" {
		t.Errorf("expected issue ID issue-001, got %s", state.Teams[0].IssueID)
	}
}

func TestDisbandUpdatesState(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "teams.toml")
	factory := newMockFactory(t)
	m := NewManagerWithState(factory, stateFile)

	createAndCancel(t, m, "issue-001")
	createAndCancel(t, m, "issue-002")

	if err := m.Disband(1); err != nil {
		t.Fatalf("Disband failed: %v", err)
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var state teamState
	if err := toml.Unmarshal(data, &state); err != nil {
		t.Fatalf("parse state file: %v", err)
	}

	if len(state.Teams) != 1 {
		t.Fatalf("expected 1 team entry after disband, got %d", len(state.Teams))
	}
	if state.Teams[0].IssueID != "issue-002" {
		t.Errorf("expected remaining team issue-002, got %s", state.Teams[0].IssueID)
	}
}

func TestRestoreRebuildsTeams(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "teams.toml")

	// Write a state file manually
	state := teamState{
		NextID: 5,
		Teams: []teamEntry{
			{ID: 3, IssueID: "issue-A"},
			{ID: 4, IssueID: "issue-B"},
		},
	}
	f, err := os.Create(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	toml.NewEncoder(f).Encode(state)
	f.Close()

	factory := newMockFactory(t)
	m := NewManagerWithState(factory, stateFile)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so agents exit immediately

	checker := &mockIssueChecker{finished: map[string]bool{}}
	if err := m.Restore(ctx, checker); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if m.Count() != 2 {
		t.Errorf("expected 2 teams after restore, got %d", m.Count())
	}

	// Check nextID was restored
	m.mu.Lock()
	nextID := m.nextID
	m.mu.Unlock()
	if nextID != 5 {
		t.Errorf("expected nextID 5, got %d", nextID)
	}

	// Verify issue IDs
	infos := m.List()
	issueIDs := map[string]bool{}
	for _, info := range infos {
		issueIDs[info.IssueID] = true
	}
	if !issueIDs["issue-A"] {
		t.Error("expected issue-A in restored teams")
	}
	if !issueIDs["issue-B"] {
		t.Error("expected issue-B in restored teams")
	}
}

func TestRestoreSkipsFinishedIssues(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "teams.toml")

	state := teamState{
		NextID: 4,
		Teams: []teamEntry{
			{ID: 1, IssueID: "issue-done"},
			{ID: 2, IssueID: "issue-active"},
			{ID: 3, IssueID: "issue-also-done"},
		},
	}
	f, err := os.Create(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	toml.NewEncoder(f).Encode(state)
	f.Close()

	factory := newMockFactory(t)
	m := NewManagerWithState(factory, stateFile)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := &mockIssueChecker{finished: map[string]bool{
		"issue-done":      true,
		"issue-also-done": true,
	}}
	if err := m.Restore(ctx, checker); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if m.Count() != 1 {
		t.Errorf("expected 1 team (only active), got %d", m.Count())
	}

	infos := m.List()
	if len(infos) != 1 || infos[0].IssueID != "issue-active" {
		t.Errorf("expected issue-active, got %v", infos)
	}

	// nextID should still be restored
	m.mu.Lock()
	nextID := m.nextID
	m.mu.Unlock()
	if nextID != 4 {
		t.Errorf("expected nextID 4, got %d", nextID)
	}
}

func TestRestoreNoFile(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "nonexistent-teams.toml")

	factory := newMockFactory(t)
	m := NewManagerWithState(factory, stateFile)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := &mockIssueChecker{finished: map[string]bool{}}
	if err := m.Restore(ctx, checker); err != nil {
		t.Fatalf("Restore should not error on missing file: %v", err)
	}

	if m.Count() != 0 {
		t.Errorf("expected 0 teams, got %d", m.Count())
	}
}

// --- Worktree tests ---

// mockWorktreeFactory implements both TeamFactory and WorktreeProvider.
type mockWorktreeFactory struct {
	mockFactory
	preparedDirs   []string
	cleanedDirs    []string
	prepareFail    bool
}

func newMockWorktreeFactory(t *testing.T) *mockWorktreeFactory {
	t.Helper()
	return &mockWorktreeFactory{
		mockFactory: mockFactory{tmpDir: t.TempDir()},
	}
}

func (m *mockWorktreeFactory) PrepareWorktree(teamNum int, issueID string) (string, error) {
	if m.prepareFail {
		return "", fmt.Errorf("prepare worktree failed")
	}
	dir := filepath.Join(m.tmpDir, fmt.Sprintf("worktrees/team-%d", teamNum))
	os.MkdirAll(dir, 0755)
	m.preparedDirs = append(m.preparedDirs, dir)
	return dir, nil
}

func (m *mockWorktreeFactory) CleanupWorktree(workDir string) error {
	m.cleanedDirs = append(m.cleanedDirs, workDir)
	return nil
}

func TestCreateWithWorktree(t *testing.T) {
	factory := newMockWorktreeFactory(t)
	m := NewManager(factory)

	team := createAndCancel(t, m, "issue-wt-001")

	if team.WorkDir == "" {
		t.Error("expected WorkDir to be set")
	}
	if len(factory.preparedDirs) != 1 {
		t.Errorf("expected 1 prepared dir, got %d", len(factory.preparedDirs))
	}
	if team.WorkDir != factory.preparedDirs[0] {
		t.Errorf("expected WorkDir %s, got %s", factory.preparedDirs[0], team.WorkDir)
	}
}

func TestDisbandCleansWorktree(t *testing.T) {
	factory := newMockWorktreeFactory(t)
	m := NewManager(factory)

	team := createAndCancel(t, m, "issue-wt-002")

	if err := m.Disband(team.ID); err != nil {
		t.Fatalf("Disband failed: %v", err)
	}

	if len(factory.cleanedDirs) != 1 {
		t.Fatalf("expected 1 cleaned dir, got %d", len(factory.cleanedDirs))
	}
	if factory.cleanedDirs[0] != team.WorkDir {
		t.Errorf("expected cleaned dir %s, got %s", team.WorkDir, factory.cleanedDirs[0])
	}
}

func TestRestoreReusesWorktree(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "teams.toml")

	expectedWorkDir := "/path/to/data/worktrees/team-3"

	// Write a state file with work_dir
	state := teamState{
		NextID: 4,
		Teams: []teamEntry{
			{ID: 3, IssueID: "issue-wt-A", WorkDir: expectedWorkDir},
		},
	}
	f, err := os.Create(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	toml.NewEncoder(f).Encode(state)
	f.Close()

	factory := newMockWorktreeFactory(t)
	m := NewManagerWithState(factory, stateFile)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := &mockIssueChecker{finished: map[string]bool{}}
	if err := m.Restore(ctx, checker); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if m.Count() != 1 {
		t.Fatalf("expected 1 team, got %d", m.Count())
	}

	// PrepareWorktree should NOT have been called (restore reuses existing dir)
	if len(factory.preparedDirs) != 0 {
		t.Errorf("expected 0 prepared dirs on restore, got %d", len(factory.preparedDirs))
	}

	// Verify the WorkDir was restored
	m.mu.Lock()
	team := m.teams[3]
	m.mu.Unlock()

	if team.WorkDir != expectedWorkDir {
		t.Errorf("expected WorkDir %s, got %s", expectedWorkDir, team.WorkDir)
	}
}

// --- Announce start tests ---

func TestCreateAnnouncesStart(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory)

	team := createAndCancel(t, m, "issue-announce")

	for _, ag := range []*agent.Agent{team.Architect, team.Engineer, team.Reviewer} {
		msgs, err := ag.ChatLog.Poll("PM")
		if err != nil {
			t.Fatalf("Poll failed for %s: %v", ag.ID.String(), err)
		}
		if len(msgs) != 1 {
			t.Errorf("expected 1 announce message for %s, got %d", ag.ID.String(), len(msgs))
			continue
		}
		if msgs[0].Sender != ag.ID.String() {
			t.Errorf("expected sender %s, got %s", ag.ID.String(), msgs[0].Sender)
		}
		if !strings.Contains(msgs[0].Body, "作業を開始します") {
			t.Errorf("expected announce body to contain '作業を開始します', got %q", msgs[0].Body)
		}
		if !strings.Contains(msgs[0].Body, "issue-announce") {
			t.Errorf("expected announce body to contain issue ID, got %q", msgs[0].Body)
		}
	}
}

func TestRestoreAnnouncesStart(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "teams.toml")

	state := teamState{
		NextID: 2,
		Teams: []teamEntry{
			{ID: 1, IssueID: "issue-restore-announce"},
		},
	}
	f, err := os.Create(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	toml.NewEncoder(f).Encode(state)
	f.Close()

	factory := newMockFactory(t)
	m := NewManagerWithState(factory, stateFile)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := &mockIssueChecker{finished: map[string]bool{}}
	if err := m.Restore(ctx, checker); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	m.mu.Lock()
	team := m.teams[1]
	m.mu.Unlock()

	if team == nil {
		t.Fatal("expected team 1 to exist after restore")
	}

	for _, ag := range []*agent.Agent{team.Architect, team.Engineer, team.Reviewer} {
		msgs, err := ag.ChatLog.Poll("PM")
		if err != nil {
			t.Fatalf("Poll failed for %s: %v", ag.ID.String(), err)
		}
		if len(msgs) != 1 {
			t.Errorf("expected 1 announce message for %s, got %d", ag.ID.String(), len(msgs))
			continue
		}
		if msgs[0].Sender != ag.ID.String() {
			t.Errorf("expected sender %s, got %s", ag.ID.String(), msgs[0].Sender)
		}
		if !strings.Contains(msgs[0].Body, "作業を開始します") {
			t.Errorf("expected announce body to contain '作業を開始します', got %q", msgs[0].Body)
		}
		if !strings.Contains(msgs[0].Body, "issue-restore-announce") {
			t.Errorf("expected announce body to contain issue ID, got %q", msgs[0].Body)
		}
	}
}


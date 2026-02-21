package team

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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

func (m *mockFactory) CreateTeamAgents(teamNum int, issueID string) (architect, engineer, reviewer *agent.Agent, err error) {
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

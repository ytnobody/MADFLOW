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

func (m *mockFactory) CreateTeamAgents(teamNum int, issueID string) (engineer *agent.Agent, err error) {
	if m.shouldFail {
		return nil, fmt.Errorf("factory error")
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

	return makeAgent(agent.RoleEngineer),
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
	m := NewManager(newMockFactory(t), 0)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 teams, got %d", m.Count())
	}
}

func TestCreate(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0)

	team := createAndCancel(t, m, "issue-001")

	if team.ID != 1 {
		t.Errorf("expected team ID 1, got %d", team.ID)
	}
	if team.IssueID != "issue-001" {
		t.Errorf("expected issue ID issue-001, got %s", team.IssueID)
	}
	if team.Engineer == nil {
		t.Error("expected all three agents to be non-nil")
	}
}

func TestCreateIncrementsID(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0)

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
	m := NewManager(factory, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Create(ctx, "issue-001")
	if err == nil {
		t.Fatal("expected error from factory, got nil")
	}
}

func TestCount(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0)

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
	m := NewManager(factory, 0)

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
	m := NewManager(factory, 0)

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
	m := NewManager(newMockFactory(t), 0)

	err := m.Disband(999)
	if err == nil {
		t.Fatal("expected error for non-existent team, got nil")
	}
}

func TestDisbandByIssue(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0)

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
	m := NewManager(newMockFactory(t), 0)

	err := m.DisbandByIssue("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestCreateRespectsMaxTeams(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 2)

	createAndCancel(t, m, "issue-001")
	createAndCancel(t, m, "issue-002")

	// 3rd team should fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Create(ctx, "issue-003")
	if err == nil {
		t.Fatal("expected error when exceeding max teams, got nil")
	}
	if !strings.Contains(err.Error(), "maximum") {
		t.Errorf("expected error to mention 'maximum', got %q", err.Error())
	}

	// Count should still be 2
	if m.Count() != 2 {
		t.Errorf("expected 2 teams, got %d", m.Count())
	}
}

func TestCreateAfterDisbandAllowsNew(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 2)

	createAndCancel(t, m, "issue-001")
	t2 := createAndCancel(t, m, "issue-002")

	// Disband one team
	if err := m.Disband(t2.ID); err != nil {
		t.Fatalf("Disband failed: %v", err)
	}

	// Now creating a new team should succeed
	t3 := createAndCancel(t, m, "issue-003")
	if t3 == nil {
		t.Fatal("expected team to be created after disband")
	}
	if m.Count() != 2 {
		t.Errorf("expected 2 teams, got %d", m.Count())
	}
}

func TestDefaultMaxTeams(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0) // 0 means use default

	// Should allow up to DefaultMaxTeams (4)
	for i := 1; i <= DefaultMaxTeams; i++ {
		createAndCancel(t, m, fmt.Sprintf("issue-%03d", i))
	}
	if m.Count() != DefaultMaxTeams {
		t.Errorf("expected %d teams, got %d", DefaultMaxTeams, m.Count())
	}

	// Next one should fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Create(ctx, "issue-over-limit")
	if err == nil {
		t.Fatal("expected error when exceeding default max teams")
	}
}

func TestCreateAnnouncesStart(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0)

	team := createAndCancel(t, m, "issue-announce")

	for _, ag := range []*agent.Agent{team.Engineer} {
		msgs, err := ag.ChatLog.Poll("superintendent")
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

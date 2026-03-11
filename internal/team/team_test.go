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

	team, err := mgr.Create(ctx, issueID, "")
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

	_, err := m.Create(ctx, "issue-001", "")
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

func TestHasIssue(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0)

	// No teams yet
	if m.HasIssue("issue-001") {
		t.Error("expected HasIssue to return false for empty manager")
	}

	createAndCancel(t, m, "issue-001")
	createAndCancel(t, m, "issue-002")

	if !m.HasIssue("issue-001") {
		t.Error("expected HasIssue to return true for issue-001")
	}
	if !m.HasIssue("issue-002") {
		t.Error("expected HasIssue to return true for issue-002")
	}
	if m.HasIssue("issue-999") {
		t.Error("expected HasIssue to return false for non-existent issue")
	}

	// After disbanding, HasIssue should return false
	_, _ = m.DisbandByIssue("issue-001")
	if m.HasIssue("issue-001") {
		t.Error("expected HasIssue to return false after disbanding issue-001")
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

	if _, err := m.DisbandByIssue("issue-001"); err != nil {
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

	_, err := m.DisbandByIssue("nonexistent")
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
	_, err := m.Create(ctx, "issue-003", "")
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
	_, err := m.Create(ctx, "issue-over-limit", "")
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

// TestAnnounceStartSendsDirectAssignmentToEngineer は、イシューが割り当てられた場合に
// 正しいエンジニアIDへ直接割り当てメッセージが送信されることを確認する。
// これはエンジニア応答問題の修正（MADFLOW-077）のためのテスト。
func TestAnnounceStartSendsDirectAssignmentToEngineer(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0)

	team := createAndCancel(t, m, "issue-direct-assign")

	// エンジニアへの直接割り当てメッセージが送信されているか確認
	engineerID := team.Engineer.ID.String()
	msgs, err := team.Engineer.ChatLog.Poll(engineerID)
	if err != nil {
		t.Fatalf("Poll failed for engineer %s: %v", engineerID, err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 direct assignment message for %s, got %d", engineerID, len(msgs))
		return
	}
	if msgs[0].Sender != "superintendent" {
		t.Errorf("expected sender superintendent, got %s", msgs[0].Sender)
	}
	if !strings.Contains(msgs[0].Body, "issue-direct-assign") {
		t.Errorf("expected assignment body to contain issue ID, got %q", msgs[0].Body)
	}
	if !strings.Contains(msgs[0].Body, "実装をお願いします") {
		t.Errorf("expected assignment body to contain '実装をお願いします', got %q", msgs[0].Body)
	}
}

// TestAnnounceStartStandbyNoDirectAssignment は、スタンバイ状態（イシューなし）の場合に
// エンジニアへの直接割り当てメッセージが送信されないことを確認する。
func TestAnnounceStartStandbyNoDirectAssignment(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 0)

	// イシューなしでスタンバイチームを作成
	team := createAndCancel(t, m, "")

	// エンジニアへの直接割り当てメッセージが送信されていないことを確認
	engineerID := team.Engineer.ID.String()
	msgs, err := team.Engineer.ChatLog.Poll(engineerID)
	if err != nil {
		t.Fatalf("Poll failed for engineer %s: %v", engineerID, err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 direct assignment messages for standby team, got %d: %v", len(msgs), msgs)
	}
}

// TestAssignIdle は、スタンバイチームにイシューをアサインできることを確認する。
func TestAssignIdle(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 2)

	// 2つのスタンバイチームを作成（イシューなし）
	t1 := createAndCancel(t, m, "")
	t2 := createAndCancel(t, m, "")

	_ = t2 // suppress unused warning

	// スタンバイチームにイシューをアサイン
	assigned, ok := m.AssignIdle("issue-idle-01", "Idle Test Issue")
	if !ok {
		t.Fatal("expected AssignIdle to return true when idle team is available")
	}
	if assigned == nil {
		t.Fatal("expected non-nil team from AssignIdle")
	}
	if assigned.IssueID != "issue-idle-01" {
		t.Errorf("expected IssueID issue-idle-01, got %s", assigned.IssueID)
	}
	if assigned.IssueTitle != "Idle Test Issue" {
		t.Errorf("expected IssueTitle 'Idle Test Issue', got %s", assigned.IssueTitle)
	}

	// アサイン後はそのイシューが HasIssue で見えること
	if !m.HasIssue("issue-idle-01") {
		t.Error("expected HasIssue to return true after AssignIdle")
	}

	// t1のチームにアサインされた可能性もあるので、アサインされたチームIDがt1またはt2のものであることを確認
	if assigned.ID != t1.ID && assigned.ID != t2.ID {
		t.Errorf("expected assigned team ID to be %d or %d, got %d", t1.ID, t2.ID, assigned.ID)
	}

	// もう一つ別のイシューをアサイン（もう一つのスタンバイチームに入るはず）
	assigned2, ok2 := m.AssignIdle("issue-idle-02", "Idle Test Issue 2")
	if !ok2 {
		t.Fatal("expected second AssignIdle to succeed with remaining idle team")
	}
	if assigned2.IssueID != "issue-idle-02" {
		t.Errorf("expected IssueID issue-idle-02, got %s", assigned2.IssueID)
	}

	// 3つ目はスタンバイがないので失敗するはず
	_, ok3 := m.AssignIdle("issue-idle-03", "No Idle")
	if ok3 {
		t.Error("expected AssignIdle to return false when no idle teams are available")
	}
}

// TestAssignIdleReturnsFalseWhenAllBusy は、全チームがビジー時にAssignIdleがfalseを返すことを確認する。
func TestAssignIdleReturnsFalseWhenAllBusy(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 2)

	// 全チームにイシューをアサイン
	createAndCancel(t, m, "issue-busy-01")
	createAndCancel(t, m, "issue-busy-02")

	// スタンバイチームなし → false
	_, ok := m.AssignIdle("issue-new", "New Issue")
	if ok {
		t.Error("expected AssignIdle to return false when all teams are busy")
	}
}

// TestAssignIdleReturnsFalseWhenNoTeams は、チームが存在しない時にAssignIdleがfalseを返すことを確認する。
func TestAssignIdleReturnsFalseWhenNoTeams(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 4)

	_, ok := m.AssignIdle("issue-001", "Issue")
	if ok {
		t.Error("expected AssignIdle to return false when no teams exist")
	}
}

// TestFullAndCap verifies the Full() and Cap() methods.
func TestFullAndCap(t *testing.T) {
	factory := newMockFactory(t)
	m := NewManager(factory, 2)

	if m.Cap() != 2 {
		t.Errorf("expected Cap()=2, got %d", m.Cap())
	}

	// Initially not full
	if m.Full() {
		t.Error("expected Full()=false on empty manager")
	}

	// Add first team — still not full
	createAndCancel(t, m, "issue-001")
	if m.Full() {
		t.Error("expected Full()=false with 1 team (max=2)")
	}

	// Add second team — now full
	createAndCancel(t, m, "issue-002")
	if !m.Full() {
		t.Error("expected Full()=true with 2 teams (max=2)")
	}

	// Disband one team — no longer full
	if _, err := m.DisbandByIssue("issue-001"); err != nil {
		t.Fatalf("DisbandByIssue failed: %v", err)
	}
	if m.Full() {
		t.Error("expected Full()=false after disbanding a team")
	}
}

// TestCapDefaultMaxTeams verifies that Cap() returns DefaultMaxTeams when maxTeams=0.
func TestCapDefaultMaxTeams(t *testing.T) {
	m := NewManager(newMockFactory(t), 0)
	if m.Cap() != DefaultMaxTeams {
		t.Errorf("expected Cap()=%d, got %d", DefaultMaxTeams, m.Cap())
	}
}

package team

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ytnobody/madflow/internal/agent"
	"github.com/ytnobody/madflow/internal/chatlog"
)

// announceStart は各エージェントの作業開始をチャットログに報告する。
// イシューが割り当てられている場合は、正しいエンジニアIDに直接割り当てメッセージも送信する。
// これにより、監督が誤ったエンジニアIDに送信してもエンジニアが確実に作業を受け取れる。
func announceStart(team *Team) {
	// 監督にチーム開始を通知（監督が正しいエンジニアIDを知るために必要）
	line := chatlog.FormatMessage(
		"superintendent",
		team.Engineer.ID.String(),
		fmt.Sprintf("チーム %d の %s として作業を開始します。イシュー: %s", team.ID, team.Engineer.ID.Role, team.IssueID),
	)
	appendLine(team.Engineer.ChatLog.Path(), line)

	// イシューが割り当てられている場合、正しいエンジニアIDに直接割り当てメッセージを送信する。
	// 監督がTEAM_CREATE送信と同タイミングでエンジニアに割り当てメッセージを送ると、
	// announceStartより前に誤ったエンジニアIDへ送信されてしまう競合状態を回避するため。
	if team.IssueID != "" {
		assignLine := chatlog.FormatMessage(
			team.Engineer.ID.String(), // 正しいエンジニアIDに直接送信
			"superintendent",
			fmt.Sprintf("イシュー %s の実装をお願いします。あなたにアサインしました。", team.IssueID),
		)
		appendLine(team.Engineer.ChatLog.Path(), assignLine)
	}
}

// appendLine はチャットログファイルに1行追記する。
func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[team] announce: open %s: %v", path, err)
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// Team represents a task force team (engineer).
type Team struct {
	ID       int
	IssueID  string
	Engineer *agent.Agent
	cancel   context.CancelFunc
}

// DefaultMaxTeams is the default maximum number of concurrent teams.
const DefaultMaxTeams = 4

// Manager manages the lifecycle of task force teams.
type Manager struct {
	mu            sync.Mutex
	teams         map[int]*Team
	pendingIssues map[string]bool // issues currently being created (not yet in teams)
	pendingCount  int             // number of teams being created (not yet in teams)
	nextID        int
	maxTeams      int
	factory       TeamFactory
}

// TeamFactory creates agents for a team. Provided by the orchestrator.
type TeamFactory interface {
	CreateTeamAgents(teamNum int, issueID string) (engineer *agent.Agent, err error)
}

func NewManager(factory TeamFactory, maxTeams int) *Manager {
	if maxTeams <= 0 {
		maxTeams = DefaultMaxTeams
	}
	return &Manager{
		teams:         make(map[int]*Team),
		pendingIssues: make(map[string]bool),
		nextID:        1,
		maxTeams:      maxTeams,
		factory:       factory,
	}
}

// Create creates and starts a new team for the given issue.
func (m *Manager) Create(ctx context.Context, issueID string) (*Team, error) {
	m.mu.Lock()
	// Count both active teams and teams being created to prevent maxTeams bypass.
	totalSlots := len(m.teams) + m.pendingCount
	if totalSlots >= m.maxTeams {
		m.mu.Unlock()
		return nil, fmt.Errorf("maximum number of concurrent teams reached (%d)", m.maxTeams)
	}
	// Check if this issue is already being created (pending) to prevent duplicates.
	if issueID != "" && m.pendingIssues[issueID] {
		m.mu.Unlock()
		return nil, fmt.Errorf("team creation already in progress for issue %s", issueID)
	}
	teamNum := m.nextID
	m.nextID++
	// Mark this slot and issue as pending before releasing the lock.
	m.pendingCount++
	if issueID != "" {
		m.pendingIssues[issueID] = true
	}
	m.mu.Unlock()

	// Clean up pending state if CreateTeamAgents fails.
	engineer, err := m.factory.CreateTeamAgents(teamNum, issueID)
	if err != nil {
		m.mu.Lock()
		m.pendingCount--
		delete(m.pendingIssues, issueID)
		m.mu.Unlock()
		return nil, fmt.Errorf("create team agents: %w", err)
	}

	teamCtx, cancel := context.WithCancel(ctx)

	team := &Team{
		ID:       teamNum,
		IssueID:  issueID,
		Engineer: engineer,
		cancel:   cancel,
	}

	// Move from pending to active.
	m.mu.Lock()
	m.teams[teamNum] = team
	m.pendingCount--
	delete(m.pendingIssues, issueID)
	m.mu.Unlock()

	// Start the engineer agent with restart on unexpected exit.
	// Watch is created once outside the restart loop so that messages
	// arriving during the restart delay are buffered and not lost.
	go func() {
		msgCh := engineer.ChatLog.Watch(teamCtx, engineer.ID.String())
		for {
			err := engineer.Run(teamCtx, msgCh)
			if teamCtx.Err() != nil {
				return
			}
			log.Printf("[team-%d] engineer exited: %v, restarting in 5s", teamNum, err)
			select {
			case <-teamCtx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()

	// Wait for the engineer agent to complete initial startup
	select {
	case <-engineer.Ready():
	case <-ctx.Done():
		// Context cancelled, but engineer should signal Ready very soon.
		select {
		case <-engineer.Ready():
		case <-time.After(30 * time.Second):
			log.Printf("[team-%d] timed out waiting for engineer to be ready", teamNum)
			return team, nil
		}
	}

	announceStart(team)

	log.Printf("[team-%d] created for issue %s (engineer ready)", teamNum, issueID)
	return team, nil
}

// Disband stops all agents in a team and removes it.
func (m *Manager) Disband(teamNum int) error {
	m.mu.Lock()
	team, ok := m.teams[teamNum]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("team %d not found", teamNum)
	}
	delete(m.teams, teamNum)
	m.mu.Unlock()

	team.cancel()
	log.Printf("[team-%d] disbanded (issue %s)", teamNum, team.IssueID)
	return nil
}

// DisbandByIssue finds and disbands the team assigned to the given issue.
func (m *Manager) DisbandByIssue(issueID string) error {
	m.mu.Lock()
	var targetNum int
	var found bool
	for num, t := range m.teams {
		if t.IssueID == issueID {
			targetNum = num
			found = true
			break
		}
	}
	m.mu.Unlock()

	if !found {
		return fmt.Errorf("no team found for issue %s", issueID)
	}
	return m.Disband(targetNum)
}

// List returns a snapshot of all active teams.
func (m *Manager) List() []TeamInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	infos := make([]TeamInfo, 0, len(m.teams))
	for _, t := range m.teams {
		infos = append(infos, TeamInfo{
			ID:      t.ID,
			IssueID: t.IssueID,
		})
	}
	return infos
}

// HasIssue returns true if any active or pending team is assigned to the given issue.
func (m *Manager) HasIssue(issueID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Check teams being created (not yet in m.teams).
	if m.pendingIssues[issueID] {
		return true
	}
	for _, t := range m.teams {
		if t.IssueID == issueID {
			return true
		}
	}
	return false
}

// Count returns the number of active and pending teams.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.teams) + m.pendingCount
}

// TeamInfo is a read-only snapshot of a team's state.
type TeamInfo struct {
	ID      int
	IssueID string
}

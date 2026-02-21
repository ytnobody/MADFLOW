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
func announceStart(team *Team) {
	line := chatlog.FormatMessage(
		"",
		team.Engineer.ID.String(),
		fmt.Sprintf("チーム %d の %s として作業を開始します。イシュー: %s", team.ID, team.Engineer.ID.Role, team.IssueID),
	)
	appendLine(team.Engineer.ChatLog.Path(), line)
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
	ID        int
	IssueID   string
	Engineer  *agent.Agent
	cancel    context.CancelFunc
}

// DefaultMaxTeams is the default maximum number of concurrent teams.
const DefaultMaxTeams = 4

// Manager manages the lifecycle of task force teams.
type Manager struct {
	mu       sync.Mutex
	teams    map[int]*Team
	nextID   int
	maxTeams int
	factory  TeamFactory
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
		teams:    make(map[int]*Team),
		nextID:   1,
		maxTeams: maxTeams,
		factory:  factory,
	}
}

// Create creates and starts a new team for the given issue.
func (m *Manager) Create(ctx context.Context, issueID string) (*Team, error) {
	m.mu.Lock()
	if len(m.teams) >= m.maxTeams {
		m.mu.Unlock()
		return nil, fmt.Errorf("maximum number of concurrent teams reached (%d)", m.maxTeams)
	}
	teamNum := m.nextID
	m.nextID++
	m.mu.Unlock()

	engineer, err := m.factory.CreateTeamAgents(teamNum, issueID)
	if err != nil {
		return nil, fmt.Errorf("create team agents: %w", err)
	}

	teamCtx, cancel := context.WithCancel(ctx)

	team := &Team{
		ID:        teamNum,
		IssueID:   issueID,
		Engineer:  engineer,
		cancel:    cancel,
	}

	m.mu.Lock()
	m.teams[teamNum] = team
	m.mu.Unlock()

	// Start the engineer agent
	go func() {
		if err := engineer.Run(teamCtx); err != nil && teamCtx.Err() == nil {
			log.Printf("[team-%d] engineer stopped: %v", teamNum, err)
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

// Count returns the number of active teams.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.teams)
}

// TeamInfo is a read-only snapshot of a team's state.
type TeamInfo struct {
	ID      int
	IssueID string
}

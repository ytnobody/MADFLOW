package team

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/ytnobody/madflow/internal/agent"
)

// Team represents a task force team (architect + engineer + reviewer).
type Team struct {
	ID        int
	IssueID   string
	Architect *agent.Agent
	Engineer  *agent.Agent
	Reviewer  *agent.Agent
	cancel    context.CancelFunc
}

// Manager manages the lifecycle of task force teams.
type Manager struct {
	mu      sync.Mutex
	teams   map[int]*Team
	nextID  int
	factory TeamFactory
}

// TeamFactory creates agents for a team. Provided by the orchestrator.
type TeamFactory interface {
	CreateTeamAgents(teamNum int, issueID string) (architect, engineer, reviewer *agent.Agent, err error)
}

func NewManager(factory TeamFactory) *Manager {
	return &Manager{
		teams:   make(map[int]*Team),
		nextID:  1,
		factory: factory,
	}
}

// Create creates and starts a new team for the given issue.
func (m *Manager) Create(ctx context.Context, issueID string) (*Team, error) {
	m.mu.Lock()
	teamNum := m.nextID
	m.nextID++
	m.mu.Unlock()

	architect, engineer, reviewer, err := m.factory.CreateTeamAgents(teamNum, issueID)
	if err != nil {
		return nil, fmt.Errorf("create team agents: %w", err)
	}

	teamCtx, cancel := context.WithCancel(ctx)

	team := &Team{
		ID:        teamNum,
		IssueID:   issueID,
		Architect: architect,
		Engineer:  engineer,
		Reviewer:  reviewer,
		cancel:    cancel,
	}

	m.mu.Lock()
	m.teams[teamNum] = team
	m.mu.Unlock()

	// Start all three agents
	go func() {
		if err := architect.Run(teamCtx); err != nil && teamCtx.Err() == nil {
			log.Printf("[team-%d] architect stopped: %v", teamNum, err)
		}
	}()
	go func() {
		if err := engineer.Run(teamCtx); err != nil && teamCtx.Err() == nil {
			log.Printf("[team-%d] engineer stopped: %v", teamNum, err)
		}
	}()
	go func() {
		if err := reviewer.Run(teamCtx); err != nil && teamCtx.Err() == nil {
			log.Printf("[team-%d] reviewer stopped: %v", teamNum, err)
		}
	}()

	log.Printf("[team-%d] created for issue %s", teamNum, issueID)
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

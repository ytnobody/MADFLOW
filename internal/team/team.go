package team

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ytnobody/madflow/internal/agent"
)

// announceStart は各エージェントの作業開始をチャットログに報告する。
func announceStart(team *Team) {
	for _, ag := range []*agent.Agent{team.Architect, team.Engineer, team.Reviewer} {
		ag.ChatLog.Append(
			"PM",
			ag.ID.String(),
			fmt.Sprintf("チーム %d の %s として作業を開始します。イシュー: %s", team.ID, ag.ID.Role, team.IssueID),
		)
	}
}

// Team represents a task force team (architect + engineer + reviewer).
type Team struct {
	ID        int
	IssueID   string
	WorkDir   string
	Architect *agent.Agent
	Engineer  *agent.Agent
	Reviewer  *agent.Agent
	cancel    context.CancelFunc
}

// teamState is the TOML-serializable state of the Manager.
type teamState struct {
	NextID int         `toml:"next_id"`
	Teams  []teamEntry `toml:"teams"`
}

// teamEntry is a single team in the persisted state.
type teamEntry struct {
	ID      int    `toml:"id"`
	IssueID string `toml:"issue_id"`
	WorkDir string `toml:"work_dir,omitempty"`
}

// IssueChecker checks whether an issue has been finished.
// Used during Restore to skip completed issues.
type IssueChecker interface {
	IsFinished(issueID string) bool
}

// Manager manages the lifecycle of task force teams.
type Manager struct {
	mu        sync.Mutex
	teams     map[int]*Team
	nextID    int
	factory   TeamFactory
	stateFile string // empty means no persistence
}

// TeamFactory creates agents for a team. Provided by the orchestrator.
type TeamFactory interface {
	CreateTeamAgents(teamNum int, issueID string, workDir string) (architect, engineer, reviewer *agent.Agent, err error)
}

// WorktreeProvider optionally provides worktree lifecycle management.
// If the factory also implements this interface, worktrees are managed automatically.
type WorktreeProvider interface {
	PrepareWorktree(teamNum int, issueID string) (workDir string, err error)
	CleanupWorktree(workDir string) error
}

func NewManager(factory TeamFactory) *Manager {
	return &Manager{
		teams:   make(map[int]*Team),
		nextID:  1,
		factory: factory,
	}
}

// NewManagerWithState creates a Manager that persists state to stateFile.
func NewManagerWithState(factory TeamFactory, stateFile string) *Manager {
	return &Manager{
		teams:     make(map[int]*Team),
		nextID:    1,
		factory:   factory,
		stateFile: stateFile,
	}
}

// save persists the current team state to disk atomically.
// Must be called with m.mu held.
func (m *Manager) save() {
	if m.stateFile == "" {
		return
	}

	state := teamState{NextID: m.nextID}
	for _, t := range m.teams {
		state.Teams = append(state.Teams, teamEntry{
			ID:      t.ID,
			IssueID: t.IssueID,
			WorkDir: t.WorkDir,
		})
	}

	// Atomic write: temp file + rename
	dir := filepath.Dir(m.stateFile)
	tmp, err := os.CreateTemp(dir, "teams-*.toml.tmp")
	if err != nil {
		log.Printf("[team] save state: create temp: %v", err)
		return
	}
	tmpName := tmp.Name()

	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(state); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		log.Printf("[team] save state: encode: %v", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		log.Printf("[team] save state: close: %v", err)
		return
	}
	if err := os.Rename(tmpName, m.stateFile); err != nil {
		os.Remove(tmpName)
		log.Printf("[team] save state: rename: %v", err)
		return
	}
}

// Restore loads team state from the state file and recreates teams.
// Teams whose issues are finished (per checker) are skipped.
func (m *Manager) Restore(ctx context.Context, checker IssueChecker) error {
	if m.stateFile == "" {
		return nil
	}

	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read state file: %w", err)
	}

	var state teamState
	if err := toml.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse state file: %w", err)
	}

	m.mu.Lock()
	if state.NextID > m.nextID {
		m.nextID = state.NextID
	}
	m.mu.Unlock()

	for _, entry := range state.Teams {
		if checker != nil && checker.IsFinished(entry.IssueID) {
			log.Printf("[team] restore: skipping finished issue %s (team %d)", entry.IssueID, entry.ID)
			continue
		}

		architect, engineer, reviewer, err := m.factory.CreateTeamAgents(entry.ID, entry.IssueID, entry.WorkDir)
		if err != nil {
			log.Printf("[team] restore: create agents for team %d failed: %v", entry.ID, err)
			continue
		}

		teamCtx, cancel := context.WithCancel(ctx)
		t := &Team{
			ID:        entry.ID,
			IssueID:   entry.IssueID,
			WorkDir:   entry.WorkDir,
			Architect: architect,
			Engineer:  engineer,
			Reviewer:  reviewer,
			cancel:    cancel,
		}

		m.mu.Lock()
		m.teams[entry.ID] = t
		m.mu.Unlock()

		m.launchWithRestart(teamCtx, entry.ID, architect)
		m.launchWithRestart(teamCtx, entry.ID, engineer)
		m.launchWithRestart(teamCtx, entry.ID, reviewer)

		announceStart(t)

		log.Printf("[team-%d] restored for issue %s", entry.ID, entry.IssueID)
	}

	// Re-save to reflect any skipped teams
	m.mu.Lock()
	m.save()
	m.mu.Unlock()

	return nil
}

// launchWithRestart starts an agent in a goroutine with automatic restart on failure.
func (m *Manager) launchWithRestart(ctx context.Context, teamNum int, ag *agent.Agent) {
	go func() {
		for {
			err := ag.Run(ctx)
			if ctx.Err() != nil {
				return
			}
			log.Printf("[team-%d] %s exited: %v, restarting in 5s", teamNum, ag.ID.String(), err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

// Create creates and starts a new team for the given issue.
func (m *Manager) Create(ctx context.Context, issueID string) (*Team, error) {
	m.mu.Lock()
	teamNum := m.nextID
	m.nextID++
	m.mu.Unlock()

	var workDir string
	if wp, ok := m.factory.(WorktreeProvider); ok {
		wd, err := wp.PrepareWorktree(teamNum, issueID)
		if err != nil {
			return nil, fmt.Errorf("prepare worktree: %w", err)
		}
		workDir = wd
	}

	architect, engineer, reviewer, err := m.factory.CreateTeamAgents(teamNum, issueID, workDir)
	if err != nil {
		return nil, fmt.Errorf("create team agents: %w", err)
	}

	teamCtx, cancel := context.WithCancel(ctx)

	team := &Team{
		ID:        teamNum,
		IssueID:   issueID,
		WorkDir:   workDir,
		Architect: architect,
		Engineer:  engineer,
		Reviewer:  reviewer,
		cancel:    cancel,
	}

	m.mu.Lock()
	m.teams[teamNum] = team
	m.save()
	m.mu.Unlock()

	// Start all three agents with restart
	m.launchWithRestart(teamCtx, teamNum, architect)
	m.launchWithRestart(teamCtx, teamNum, engineer)
	m.launchWithRestart(teamCtx, teamNum, reviewer)

	announceStart(team)

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
	m.save()
	m.mu.Unlock()

	team.cancel()

	if team.WorkDir != "" {
		if wp, ok := m.factory.(WorktreeProvider); ok {
			if err := wp.CleanupWorktree(team.WorkDir); err != nil {
				log.Printf("[team-%d] cleanup worktree failed: %v", teamNum, err)
			}
		}
	}

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

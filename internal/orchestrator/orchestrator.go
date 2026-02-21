package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ytnobody/madflow/internal/agent"
	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/config"
	"github.com/ytnobody/madflow/internal/git"
	"github.com/ytnobody/madflow/internal/github"
	"github.com/ytnobody/madflow/internal/issue"
	"github.com/ytnobody/madflow/internal/team"
)

// Orchestrator manages the lifecycle of all agents and subsystems.
type Orchestrator struct {
	cfg       *config.Config
	dataDir   string
	promptDir string

	store    *issue.Store
	chatLog  *chatlog.ChatLog
	teams    *team.Manager
	repos    map[string]*git.Repo // name -> repo
	dormancy *agent.Dormancy

	residentAgents []*agent.Agent
	mu             sync.Mutex
}

// New creates a new Orchestrator.
func New(cfg *config.Config, dataDir, promptDir string) *Orchestrator {
	issuesDir := filepath.Join(dataDir, "issues")
	chatLogPath := filepath.Join(dataDir, "chatlog.txt")

	repos := make(map[string]*git.Repo, len(cfg.Project.Repos))
	for _, r := range cfg.Project.Repos {
		repos[r.Name] = git.NewRepo(r.Path)
	}

	orc := &Orchestrator{
		cfg:       cfg,
		dataDir:   dataDir,
		promptDir: promptDir,
		store:     issue.NewStore(issuesDir),
		chatLog:   chatlog.New(chatLogPath),
		repos:     repos,
		dormancy:  agent.NewDormancy(),
	}

	orc.teams = team.NewManager(orc, cfg.Agent.MaxTeams)
	return orc
}

// Run starts all subsystems and blocks until ctx is cancelled.
func (o *Orchestrator) Run(ctx context.Context) error {
	// Ensure data directories exist
	for _, sub := range []string{"issues", "memos"} {
		os.MkdirAll(filepath.Join(o.dataDir, sub), 0755)
	}

	// Ensure chatlog file exists
	if _, err := os.Stat(o.chatLog.Path()); os.IsNotExist(err) {
		os.WriteFile(o.chatLog.Path(), nil, 0644)
	}

	log.Println("[orchestrator] starting")

	var wg sync.WaitGroup

	// Start all agents (teams + residents) concurrently
	if err := o.startResidentAgents(ctx, &wg); err != nil {
		return fmt.Errorf("start resident agents: %w", err)
	}
	o.startAllTeams(ctx)

	// Wait for all resident agents to complete their initial startup
	if err := o.waitForAgentsReady(ctx); err != nil {
		return fmt.Errorf("wait for agents ready: %w", err)
	}

	// Start GitHub sync if configured
	if o.cfg.GitHub != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runGitHubSync(ctx)
		}()
	}

	// Start chatlog cleanup goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.runChatlogCleanup(ctx)
	}()

	// Watch chatlog for orchestrator commands
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.watchCommands(ctx)
	}()

	log.Println("[orchestrator] all subsystems started")

	// Wait for context cancellation
	<-ctx.Done()
	log.Println("[orchestrator] shutting down")
	wg.Wait()
	log.Println("[orchestrator] stopped")
	return ctx.Err()
}

// startResidentAgents starts the superintendent, PM, and release manager.
func (o *Orchestrator) startResidentAgents(ctx context.Context, wg *sync.WaitGroup) error {
	resetInterval := time.Duration(o.cfg.Agent.ContextResetMinutes) * time.Minute

	residents := []struct {
		role  agent.Role
		model string
	}{
		{agent.RoleSuperintendent, o.cfg.Agent.Models.Superintendent},
		{agent.RolePM, o.cfg.Agent.Models.PM},
		{agent.RoleReleaseManager, o.cfg.Agent.Models.ReleaseManager},
	}

	for _, r := range residents {
		vars := agent.PromptVars{
			AgentID:       string(r.role),
			ChatLogPath:   o.chatLog.Path(),
			IssuesDir:     filepath.Join(o.dataDir, "issues"),
			DevelopBranch: o.cfg.Branches.Develop,
			MainBranch:    o.cfg.Branches.Main,
			FeaturePrefix: o.cfg.Branches.FeaturePrefix,
		}

		systemPrompt, err := agent.LoadPrompt(o.promptDir, r.role, vars)
		if err != nil {
			return fmt.Errorf("load prompt for %s: %w", r.role, err)
		}

		ag := agent.NewAgent(agent.AgentConfig{
			ID:            agent.AgentID{Role: r.role},
			Role:          r.role,
			SystemPrompt:  systemPrompt,
			Model:         r.model,
			WorkDir:       o.firstRepoPath(),
			ChatLogPath:   o.chatLog.Path(),
			MemosDir:      filepath.Join(o.dataDir, "memos"),
			ResetInterval: resetInterval,
			Dormancy:      o.dormancy,
		})

		o.mu.Lock()
		o.residentAgents = append(o.residentAgents, ag)
		o.mu.Unlock()

		wg.Add(1)
		go func(a *agent.Agent) {
			defer wg.Done()
			o.runAgentWithRestart(ctx, a)
		}(ag)
	}

	return nil
}

// waitForAgentsReady blocks until all resident agents have completed
// their initial startup (first prompt sent) or ctx is cancelled.
func (o *Orchestrator) waitForAgentsReady(ctx context.Context) error {
	o.mu.Lock()
	agents := make([]*agent.Agent, len(o.residentAgents))
	copy(agents, o.residentAgents)
	o.mu.Unlock()

	for _, ag := range agents {
		select {
		case <-ag.Ready():
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	log.Println("[orchestrator] all resident agents ready")
	return nil
}

// startAllTeams unconditionally creates maxTeams teams at startup in parallel.
// If open/in-progress issues exist, they are assigned to teams.
// Remaining slots start as standby teams ready to receive work.
// Returns after all teams are fully operational.
func (o *Orchestrator) startAllTeams(ctx context.Context) {
	maxTeams := o.cfg.Agent.MaxTeams
	if maxTeams <= 0 {
		maxTeams = team.DefaultMaxTeams
	}

	// Collect assignable issues
	var assignable []*issue.Issue
	allIssues, err := o.store.List(issue.StatusFilter{})
	if err != nil {
		log.Printf("[orchestrator] start teams: list issues: %v", err)
	} else {
		for _, iss := range allIssues {
			if iss.Status == issue.StatusOpen || iss.Status == issue.StatusInProgress {
				assignable = append(assignable, iss)
			}
		}
	}

	// Launch all teams concurrently
	var twg sync.WaitGroup
	for i := 0; i < maxTeams; i++ {
		idx := i
		var issueID string
		if idx < len(assignable) {
			issueID = assignable[idx].ID
		}

		twg.Add(1)
		go func() {
			defer twg.Done()

			t, err := o.teams.Create(ctx, issueID)
			if err != nil {
				log.Printf("[orchestrator] start teams: team %d: %v", idx+1, err)
				return
			}

			if idx < len(assignable) {
				o.mu.Lock()
				assignable[idx].AssignedTeam = t.ID
				assignable[idx].Status = issue.StatusInProgress
				o.store.Update(assignable[idx])
				o.mu.Unlock()
				log.Printf("[orchestrator] started team %d for issue %s", t.ID, issueID)
			} else {
				log.Printf("[orchestrator] started team %d (standby)", t.ID)
			}
		}()
	}
	twg.Wait()

	log.Printf("[orchestrator] started %d teams", o.teams.Count())
}

// runAgentWithRestart runs an agent and restarts it if it exits unexpectedly.
func (o *Orchestrator) runAgentWithRestart(ctx context.Context, ag *agent.Agent) {
	for {
		err := ag.Run(ctx)
		if ctx.Err() != nil {
			return // Normal shutdown
		}
		log.Printf("[orchestrator] agent %s exited: %v, restarting in 5s", ag.ID.String(), err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// watchCommands monitors the chatlog for messages addressed to "orchestrator".
func (o *Orchestrator) watchCommands(ctx context.Context) {
	msgCh := o.chatLog.Watch(ctx, "orchestrator")

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			o.handleCommand(ctx, msg)
		}
	}
}

// handleCommand processes orchestrator commands from the chatlog.
func (o *Orchestrator) handleCommand(ctx context.Context, msg chatlog.Message) {
	body := strings.TrimSpace(msg.Body)

	switch {
	case strings.HasPrefix(body, "TEAM_CREATE"):
		o.handleTeamCreate(ctx, body)
	case strings.HasPrefix(body, "TEAM_DISBAND"):
		o.handleTeamDisband(body)
	case strings.HasPrefix(body, "RELEASE"):
		o.handleRelease(body)
	default:
		log.Printf("[orchestrator] unknown command from %s: %s", msg.Sender, body)
	}
}

// handleTeamCreate creates a new team for an issue.
// Expected format: TEAM_CREATE issue-id
func (o *Orchestrator) handleTeamCreate(ctx context.Context, body string) {
	parts := strings.Fields(body)
	if len(parts) < 2 {
		log.Printf("[orchestrator] TEAM_CREATE missing issue ID")
		return
	}
	issueID := parts[1]

	t, err := o.teams.Create(ctx, issueID)
	if err != nil {
		log.Printf("[orchestrator] TEAM_CREATE failed for %s: %v", issueID, err)
		return
	}

	// Update issue with assigned team
	iss, err := o.store.Get(issueID)
	if err == nil {
		iss.AssignedTeam = t.ID
		iss.Status = issue.StatusInProgress
		o.store.Update(iss)
	}

	log.Printf("[orchestrator] team %d created for issue %s", t.ID, issueID)
}

// handleTeamDisband disbands the team for an issue.
// Expected format: TEAM_DISBAND issue-id
func (o *Orchestrator) handleTeamDisband(body string) {
	parts := strings.Fields(body)
	if len(parts) < 2 {
		log.Printf("[orchestrator] TEAM_DISBAND missing issue ID")
		return
	}
	issueID := parts[1]

	if err := o.teams.DisbandByIssue(issueID); err != nil {
		log.Printf("[orchestrator] TEAM_DISBAND failed for %s: %v", issueID, err)
		return
	}

	log.Printf("[orchestrator] team disbanded for issue %s", issueID)
}

// handleRelease triggers a develop -> main merge.
// Expected format: RELEASE
func (o *Orchestrator) handleRelease(_ string) {
	log.Println("[orchestrator] release requested")
	for name, repo := range o.repos {
		if err := repo.Checkout(o.cfg.Branches.Main); err != nil {
			log.Printf("[orchestrator] release: checkout %s on %s failed: %v", o.cfg.Branches.Main, name, err)
			continue
		}
		ok, err := repo.Merge(o.cfg.Branches.Develop)
		if err != nil {
			log.Printf("[orchestrator] release: merge %s on %s failed: %v", o.cfg.Branches.Develop, name, err)
			continue
		}
		if !ok {
			log.Printf("[orchestrator] release: merge conflict on %s", name)
			continue
		}
		log.Printf("[orchestrator] release: merged %s -> %s on %s", o.cfg.Branches.Develop, o.cfg.Branches.Main, name)
	}
}

// runChatlogCleanup periodically truncates old chatlog entries.
func (o *Orchestrator) runChatlogCleanup(ctx context.Context) {
	maxLines := o.cfg.Agent.ChatlogMaxLines
	if maxLines <= 0 {
		maxLines = 500
	}

	interval := time.Duration(o.cfg.Agent.ContextResetMinutes) * time.Minute
	if interval <= 0 {
		interval = 8 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := o.chatLog.Truncate(maxLines); err != nil {
				log.Printf("[orchestrator] chatlog cleanup failed: %v", err)
			}
		}
	}
}

// runGitHubSync starts the GitHub issue sync loop.
func (o *Orchestrator) runGitHubSync(ctx context.Context) {
	gh := o.cfg.GitHub
	interval := time.Duration(gh.SyncIntervalMinutes) * time.Minute
	syncer := github.NewSyncer(o.store, gh.Owner, gh.Repos, interval)
	if err := syncer.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[orchestrator] github sync stopped: %v", err)
	}
}

// CreateTeamAgents implements team.TeamFactory.
func (o *Orchestrator) CreateTeamAgents(teamNum int, issueID string) (architect, engineer, reviewer *agent.Agent, err error) {
	resetInterval := time.Duration(o.cfg.Agent.ContextResetMinutes) * time.Minute
	teamNumStr := fmt.Sprintf("%d", teamNum)

	// Load the issue for context
	iss, issErr := o.store.Get(issueID)
	var originalTask string
	if issErr == nil {
		originalTask = fmt.Sprintf("Issue #%s: %s\n\n%s", iss.ID, iss.Title, iss.Body)
		if iss.Acceptance != "" {
			originalTask += "\n\n## 完了条件\n" + iss.Acceptance
		}
	}

	roles := []struct {
		role  agent.Role
		model string
	}{
		{agent.RoleArchitect, o.cfg.Agent.Models.Architect},
		{agent.RoleEngineer, o.cfg.Agent.Models.Engineer},
		{agent.RoleReviewer, o.cfg.Agent.Models.Reviewer},
	}

	agents := make([]*agent.Agent, 3)
	for i, r := range roles {
		vars := agent.PromptVars{
			AgentID:       fmt.Sprintf("%s-%d", r.role, teamNum),
			ChatLogPath:   o.chatLog.Path(),
			IssuesDir:     filepath.Join(o.dataDir, "issues"),
			DevelopBranch: o.cfg.Branches.Develop,
			MainBranch:    o.cfg.Branches.Main,
			FeaturePrefix: o.cfg.Branches.FeaturePrefix,
			TeamNum:       teamNumStr,
		}

		systemPrompt, err := agent.LoadPrompt(o.promptDir, r.role, vars)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load prompt for %s: %w", r.role, err)
		}

		agents[i] = agent.NewAgent(agent.AgentConfig{
			ID:            agent.AgentID{Role: r.role, TeamNum: teamNum},
			Role:          r.role,
			SystemPrompt:  systemPrompt,
			Model:         r.model,
			WorkDir:       o.firstRepoPath(),
			ChatLogPath:   o.chatLog.Path(),
			MemosDir:      filepath.Join(o.dataDir, "memos"),
			ResetInterval: resetInterval,
			OriginalTask:  originalTask,
			Dormancy:      o.dormancy,
		})
	}

	return agents[0], agents[1], agents[2], nil
}

// Teams returns the team manager for external access.
func (o *Orchestrator) Teams() *team.Manager {
	return o.teams
}

// Store returns the issue store for external access.
func (o *Orchestrator) Store() *issue.Store {
	return o.store
}

// ChatLogPath returns the chatlog file path.
func (o *Orchestrator) ChatLogPath() string {
	return o.chatLog.Path()
}

// HandleCommandForTest exposes handleCommand for integration testing.
func (o *Orchestrator) HandleCommandForTest(ctx context.Context, msg chatlog.Message) {
	o.handleCommand(ctx, msg)
}

func (o *Orchestrator) firstRepoPath() string {
	if len(o.cfg.Project.Repos) > 0 {
		return o.cfg.Project.Repos[0].Path
	}
	return "."
}

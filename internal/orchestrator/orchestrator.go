package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ytnobody/madflow/internal/agent"
	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/config"
	"github.com/ytnobody/madflow/internal/git"
	"github.com/ytnobody/madflow/internal/github"
	"github.com/ytnobody/madflow/internal/issue"
	"github.com/ytnobody/madflow/internal/lessons"
	"github.com/ytnobody/madflow/internal/team"
)

// Orchestrator manages the lifecycle of all agents and subsystems.
type Orchestrator struct {
	cfg        *config.Config
	cfgMu      sync.RWMutex // protects cfg for hot-reload
	configPath string       // path to madflow.toml for hot-reload watcher
	dataDir    string
	promptDir  string

	store          *issue.Store
	chatLog        *chatlog.ChatLog
	teams          *team.Manager
	repos          map[string]*git.Repo // name -> repo
	dormancy       *agent.Dormancy
	throttle       *agent.Throttle
	idleDetector   *github.IdleDetector // shared idle state for GitHub polling
	lessonsManager *lessons.Manager     // manages failure lessons for superintendent

	// patrolResetCh receives a signal when the superintendent reports PATROL_COMPLETE,
	// allowing runIssuePatrol to reset the interval timer immediately.
	patrolResetCh chan struct{}

	residentAgents []*agent.Agent
	mu             sync.Mutex

	// wg tracks in-flight goroutines spawned by handleTeamCreate so tests (and
	// graceful shutdown) can wait for all async work to complete before tearing
	// down the working directory.
	wg sync.WaitGroup
}

// New creates a new Orchestrator.
func New(cfg *config.Config, dataDir, promptDir string) *Orchestrator {
	issuesDir := filepath.Join(dataDir, "issues")
	chatLogPath := filepath.Join(dataDir, "chatlog.txt")

	repos := make(map[string]*git.Repo, len(cfg.Project.Repos))
	for _, r := range cfg.Project.Repos {
		repos[r.Name] = git.NewRepo(r.Path)
	}

	idleDetector := github.NewIdleDetector()
	if cfg.GitHub != nil && cfg.GitHub.IdleThresholdMinutes > 0 {
		idleDetector.SetIdleThreshold(time.Duration(cfg.GitHub.IdleThresholdMinutes) * time.Minute)
	}
	if cfg.GitHub != nil && cfg.GitHub.DormancyThresholdMinutes > 0 {
		idleDetector.SetDormancyThreshold(time.Duration(cfg.GitHub.DormancyThresholdMinutes) * time.Minute)
	}

	probeInterval := time.Duration(cfg.Agent.DormancyProbeMinutes) * time.Minute

	featurePrefix := cfg.Branches.FeaturePrefix
	if featurePrefix == "" {
		featurePrefix = "feature/issue-"
	}

	orc := &Orchestrator{
		cfg:           cfg,
		dataDir:       dataDir,
		promptDir:     promptDir,
		store:         issue.NewStore(issuesDir),
		chatLog:       chatlog.New(chatLogPath),
		repos:         repos,
		dormancy:      agent.NewDormancy(probeInterval),
		throttle:      agent.NewThrottle(cfg.Agent.GeminiRPM),
		idleDetector:  idleDetector,
		patrolResetCh: make(chan struct{}, 1),
		lessonsManager: &lessons.Manager{
			DataDir:       dataDir,
			FeaturePrefix: featurePrefix,
		},
	}

	orc.teams = team.NewManager(orc, cfg.Agent.MaxTeams)
	return orc
}

// WithConfigPath enables hot-reload of the config file during Run.
// Call this before Run when you want changes to madflow.toml to take effect
// without restarting the process.
func (o *Orchestrator) WithConfigPath(path string) *Orchestrator {
	o.configPath = path
	return o
}

// Config returns the current active configuration.
// Safe to call from multiple goroutines.
func (o *Orchestrator) Config() *config.Config {
	o.cfgMu.RLock()
	defer o.cfgMu.RUnlock()
	return o.cfg
}

// Run starts all subsystems and blocks until ctx is cancelled.
func (o *Orchestrator) Run(ctx context.Context) error {
	// Ensure data directories exist
	for _, sub := range []string{"issues", "memos"} {
		os.MkdirAll(filepath.Join(o.dataDir, sub), 0700)
	}

	// Truncate chatlog to start with a clean slate. Stale messages from
	// previous runs confuse the superintendent (e.g. referencing engineers
	// like engineer-4 that no longer exist, causing phantom TEAM_CREATE).
	os.WriteFile(o.chatLog.Path(), nil, 0600)

	log.Println("[orchestrator] starting")

	// Remove closed issues left over from previous runs so the
	// superintendent does not waste iterations cleaning them up.
	o.pruneClosedIssues()

	// Clean up stale worktrees from previous runs to prevent conflicts
	// when new teams are created with the same team-N directory names.
	o.cleanStaleWorktrees()

	// Ensure the main repo is on the develop branch. A previous engineer
	// may have left it on a feature branch.
	o.ensureDevelopBranch()

	var wg sync.WaitGroup

	// Start resident agents (superintendent) immediately — no need to wait for
	// GitHub sync first. The superintendent can warm up its context while the
	// initial sync runs in the background.
	if err := o.startResidentAgents(ctx, &wg); err != nil {
		return fmt.Errorf("start resident agents: %w", err)
	}

	// Run initial GitHub sync concurrently with the superintendent startup.
	// We must still complete sync before assigning issues to teams, so we
	// wait for it to finish before calling startAllTeams.
	// WithSkipComments(true) makes this sync fast (one API call per repo
	// instead of one call per repo + one per issue), reducing startup lag
	// from minutes to seconds for repositories with many issues.
	if o.cfg.GitHub != nil {
		o.initialGitHubSync()
	}

	o.startAllTeams(ctx)

	// Start watching chatlog for orchestrator commands IMMEDIATELY after teams
	// are launched — before waitForAgentsReady() — so that TEAM_CREATE messages
	// written by the superintendent during its initial prompt processing (via
	// Bash tool-calls) are not missed.
	//
	// Root-cause of the local-001 regression: watchCommands() used to start
	// after waitForAgentsReady(). chatlog.Watch() records the file offset at
	// call time; any TEAM_CREATE written before that point was skipped forever.
	// Moving watchCommands here ensures offset=0 (empty chatlog) and all
	// subsequent writes are observed correctly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.watchCommands(ctx)
	}()

	// If context was cancelled during team startup (e.g. Ctrl+C or SIGTERM
	// while agents were initialising), skip the ready-wait and go straight
	// to the graceful shutdown path so we don't surface a confusing
	// "wait for agents ready: context canceled" error.
	if ctx.Err() != nil {
		log.Println("[orchestrator] shutting down (cancelled during startup)")
		wg.Wait()
		log.Println("[orchestrator] stopped")
		return nil
	}

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

		// Start event watcher for real-time updates
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runEventWatcher(ctx)
		}()
	}

	// Start chatlog cleanup goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.runChatlogCleanup(ctx)
	}()

	// Start worktree cleanup goroutine if configured
	if o.cfg.Agent.WorktreeCleanupIntervalMinutes > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runWorktreeCleanup(ctx)
		}()
	}

	// Start branch cleanup goroutine if configured
	if o.cfg.Branches.CleanupIntervalMinutes > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runBranchCleanup(ctx)
		}()
	}

	// Start merged worktree cleanup goroutine if configured
	if o.cfg.Agent.MergedWorktreeCleanupIntervalMinutes > 0 {
		wg.Go(func() {
			o.runMergedWorktreeCleanup(ctx)
		})
	}

	// Start main branch check goroutine
	if o.cfg.Agent.MainCheckIntervalHours > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runMainCheck(ctx)
		}()
	}

	// Start document consistency check goroutine
	if o.cfg.Agent.DocCheckIntervalHours > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runDocCheck(ctx)
		}()
	}

	// Start issue patrol goroutine to periodically prompt the superintendent
	// to check for new issues. Without this, the superintendent only reacts
	// to chatlog messages and may stop patrolling during long idle periods.
	if o.cfg.Agent.IssuePatrolIntervalMinutes > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runIssuePatrol(ctx)
		}()
	}

	// Start config hot-reload watcher if a config path is set
	if o.configPath != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runConfigWatcher(ctx)
		}()
	}

	log.Println("[orchestrator] all subsystems started")

	// Wait for context cancellation
	<-ctx.Done()
	log.Println("[orchestrator] shutting down")
	wg.Wait()
	log.Println("[orchestrator] stopped")
	return ctx.Err()
}

// pruneClosedIssues removes all closed issue files from the store so
// the superintendent does not spend iterations deleting them one by one.
func (o *Orchestrator) pruneClosedIssues() {
	all, err := o.store.List(issue.StatusFilter{})
	if err != nil {
		log.Printf("[orchestrator] pruneClosedIssues: list: %v", err)
		return
	}
	for _, iss := range all {
		if iss.Status == issue.StatusClosed {
			if err := o.store.Delete(iss.ID); err != nil {
				log.Printf("[orchestrator] pruneClosedIssues: delete %s: %v", iss.ID, err)
			} else {
				log.Printf("[orchestrator] pruned closed issue %s", iss.ID)
			}
		}
	}
}

// cleanStaleWorktrees removes leftover .worktrees/team-* directories from
// previous runs. Without this, new team creation can collide with stale
// worktrees that still reference old branches.
func (o *Orchestrator) cleanStaleWorktrees() {
	for name, repo := range o.repos {
		removed := repo.CleanWorktrees("team-")
		if len(removed) > 0 {
			log.Printf("[orchestrator] cleaned %d stale worktree(s) in %s: %v", len(removed), name, removed)
		}
	}
}

// cleanTeamWorktrees removes worktrees for a specific team number.
// This is called when a team is disbanded to free up disk space and
// prevent stale worktrees from accumulating.
func (o *Orchestrator) cleanTeamWorktrees(teamNum int) {
	prefix := fmt.Sprintf("team-%d", teamNum)
	for name, repo := range o.repos {
		removed := repo.CleanWorktrees(prefix)
		if len(removed) > 0 {
			log.Printf("[orchestrator] cleaned %d worktree(s) for team %d in %s: %v", len(removed), teamNum, name, removed)
		}
	}
}

// appendOrLog appends a message to the chatlog, logging a warning if the write fails.
// This prevents silent message loss when the chatlog file is inaccessible.
func (o *Orchestrator) appendOrLog(recipient, sender, body string) {
	if err := o.chatLog.Append(recipient, sender, body); err != nil {
		log.Printf("[orchestrator] WARNING: failed to write chatlog (recipient=%s): %v — message: %s", recipient, err, body)
	}
}

// ensureDevelopBranch ensures the main repo is on the develop branch at startup.
// This prevents issues where a previous engineer left the repo on a feature branch.
func (o *Orchestrator) ensureDevelopBranch() {
	for name, repo := range o.repos {
		branch, err := repo.CurrentBranch()
		if err != nil {
			log.Printf("[orchestrator] ensureDevelopBranch: %s: %v", name, err)
			continue
		}
		develop := o.cfg.Branches.Develop
		if branch != develop {
			log.Printf("[orchestrator] repo %s is on branch %q, switching to %q", name, branch, develop)
			if err := repo.Checkout(develop); err != nil {
				log.Printf("[orchestrator] ensureDevelopBranch: checkout %s on %s failed: %v", develop, name, err)
			}
		}
	}
}

// startResidentAgents starts the superintendent.
func (o *Orchestrator) startResidentAgents(ctx context.Context, wg *sync.WaitGroup) error {
	resetInterval := time.Duration(o.cfg.Agent.ContextResetMinutes) * time.Minute
	bashTimeout := time.Duration(o.cfg.Agent.BashTimeoutMinutes) * time.Minute

	residents := []struct {
		role  agent.Role
		model string
	}{
		{agent.RoleSuperintendent, o.cfg.Agent.Models.Superintendent},
	}

	for _, r := range residents {
		vars := agent.PromptVars{
			AgentID:       string(r.role),
			ChatLogPath:   o.chatLog.Path(),
			IssuesDir:     filepath.Join(o.dataDir, "issues"),
			DevelopBranch: o.cfg.Branches.Develop,
			MainBranch:    o.cfg.Branches.Main,
			FeaturePrefix: o.cfg.Branches.FeaturePrefix,
			GhLogin:       o.cfg.GhLogin,
		}

		systemPrompt, err := agent.LoadPrompt(o.promptDir, r.role, vars)
		if err != nil {
			return fmt.Errorf("load prompt for %s: %w", r.role, err)
		}
		if o.cfg.Agent.ExtraPrompt != "" {
			systemPrompt += "\n\n" + o.cfg.Agent.ExtraPrompt
		}

		agentCfg := agent.AgentConfig{
			ID:            agent.AgentID{Role: r.role},
			Role:          r.role,
			SystemPrompt:  systemPrompt,
			Model:         r.model,
			WorkDir:       o.firstRepoPath(),
			ChatLogPath:   o.chatLog.Path(),
			MemosDir:      filepath.Join(o.dataDir, "memos"),
			ResetInterval: resetInterval,
			BashTimeout:   bashTimeout,
			Language:      o.cfg.Agent.Language,
			Dormancy:      o.dormancy,
		}
		if strings.HasPrefix(r.model, "gemini-") {
			agentCfg.Throttle = o.throttle
		}
		ag := agent.NewAgent(agentCfg)

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

	// Collect assignable issues (excluding those pending approval).
	var assignable []*issue.Issue
	allIssues, err := o.store.List(issue.StatusFilter{})
	if err != nil {
		log.Printf("[orchestrator] start teams: list issues: %v", err)
	} else {
		for _, iss := range allIssues {
			if iss.Status == issue.StatusOpen || iss.Status == issue.StatusInProgress {
				if iss.PendingApproval {
					log.Printf("[orchestrator] skipping issue %s (pending approval)", iss.ID)
					continue
				}
				// Clear stale team assignments from previous runs so the
				// issue gets correctly assigned to a newly created team.
				if iss.Status == issue.StatusInProgress && iss.AssignedTeam != 0 {
					log.Printf("[orchestrator] resetting stale AssignedTeam=%d on issue %s", iss.AssignedTeam, iss.ID)
					iss.AssignedTeam = 0
					o.store.Update(iss)
				}
				assignable = append(assignable, iss)
			}
		}
	}

	// Mark assignable issues as in_progress immediately (before async team
	// creation) to prevent the superintendent from sending a duplicate
	// TEAM_CREATE during the window where the team is being created.
	for i := 0; i < len(assignable) && i < maxTeams; i++ {
		assignable[i].Status = issue.StatusInProgress
		o.store.Update(assignable[i])
	}

	// Launch all teams concurrently (fire-and-forget so startup is not
	// blocked waiting for the first LLM response from each engineer).
	for i := 0; i < maxTeams; i++ {
		idx := i
		var issueID, issueTitle string
		if idx < len(assignable) {
			issueID = assignable[idx].ID
			issueTitle = assignable[idx].Title
		}

		go func() {
			t, err := o.teams.Create(ctx, issueID, issueTitle)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[orchestrator] start teams: team %d: %v", idx+1, err)
				}
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

	log.Printf("[orchestrator] launched %d teams (async)", maxTeams)
}

// runAgentWithRestart runs an agent and restarts it if it exits unexpectedly.
// Watch is created once outside the restart loop so that messages arriving
// during the restart delay are buffered in the channel and not lost.
func (o *Orchestrator) runAgentWithRestart(ctx context.Context, ag *agent.Agent) {
	msgCh := ag.ChatLog.Watch(ctx, ag.ID.String())
	for {
		err := ag.Run(ctx, msgCh)
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
	case strings.HasPrefix(body, "WAKE_GITHUB"):
		o.handleWakeGitHub()
	case strings.HasPrefix(body, "PATROL_COMPLETE"):
		o.handlePatrolComplete()
	default:
		log.Printf("[orchestrator] unknown command from %s: %s", msg.Sender, body)
	}
}

// handlePatrolComplete handles the PATROL_COMPLETE command from the superintendent.
// It signals runIssuePatrol to reset the interval timer, so that the next reminder
// is issued N minutes after patrol completion rather than after the last scheduled tick.
func (o *Orchestrator) handlePatrolComplete() {
	log.Println("[orchestrator] PATROL_COMPLETE received: resetting patrol timer")
	// Non-blocking send: if the channel already has a pending signal, we don't need to add another.
	select {
	case o.patrolResetCh <- struct{}{}:
	default:
	}
}

// handleWakeGitHub wakes the GitHub polling subsystem from dormancy.
// This is useful when the system has stopped polling due to a long idle period
// and an operator wants to force an immediate sync.
func (o *Orchestrator) handleWakeGitHub() {
	if o.idleDetector == nil {
		log.Println("[orchestrator] WAKE_GITHUB: no idle detector configured")
		return
	}
	o.idleDetector.Wake()
	log.Println("[orchestrator] WAKE_GITHUB: GitHub polling resumed")
}

// issueIDRe matches the valid portion of an issue ID.
// Issue IDs consist of ASCII alphanumeric characters and hyphens only
// (e.g. "gh-121", "local-001"). Any trailing characters outside this set
// (e.g. Japanese text appended to retry messages like "gh-121（2回目）") are
// stripped by normalizeIssueID before the ID is used for store lookups.
var issueIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*`)

// normalizeIssueID extracts the valid issue ID prefix from s.
// It strips any trailing characters that do not match the issue ID character
// set (ASCII alphanumeric + hyphen). Returns an empty string when s contains
// no valid issue ID characters at all.
func normalizeIssueID(s string) string {
	return issueIDRe.FindString(s)
}

// handleTeamCreate creates a new team for an issue.
// Expected format: TEAM_CREATE issue-id
//
// The expensive Create call is run in a goroutine so the watchCommands loop
// is not blocked while waiting for the LLM to respond (which can take 10+ min).
// Pre-validation checks are synchronous and fast.
func (o *Orchestrator) handleTeamCreate(ctx context.Context, body string) {
	parts := strings.Fields(body)
	if len(parts) < 2 {
		log.Printf("[orchestrator] TEAM_CREATE missing issue ID")
		return
	}

	// Normalize the issue ID: strip any non-ID characters that the superintendent
	// may append when retrying (e.g. "gh-121（2回目の要求）。チームアサインをお願いします。").
	// Issue IDs consist solely of ASCII alphanumeric characters and hyphens.
	issueID := normalizeIssueID(parts[1])
	if issueID == "" {
		log.Printf("[orchestrator] TEAM_CREATE: could not extract valid issue ID from %q", parts[1])
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE は拒否されました: 有効なイシューIDを抽出できませんでした (%q)", parts[1]))
		return
	}
	if issueID != parts[1] {
		log.Printf("[orchestrator] TEAM_CREATE: normalized issue ID %q -> %q (stripped extra text)", parts[1], issueID)
	}

	existingIss, err := o.store.Get(issueID)
	if err != nil {
		log.Printf("[orchestrator] TEAM_CREATE rejected: issue %q not found: %v", issueID, err)
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s は拒否されました: イシューが見つかりません", issueID))
		return
	}

	// Reject team creation for issues that are already closed or resolved.
	if existingIss.Status == issue.StatusClosed || existingIss.Status == issue.StatusResolved {
		log.Printf("[orchestrator] TEAM_CREATE rejected: issue %s is %s", issueID, existingIss.Status)
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s は拒否されました: イシューのステータスが %s です", issueID, existingIss.Status))
		return
	}

	// Reject if issue is already assigned to a team.
	if existingIss.AssignedTeam > 0 {
		log.Printf("[orchestrator] TEAM_CREATE rejected: issue %s already assigned to team %d", issueID, existingIss.AssignedTeam)
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s は拒否されました: 既にチーム %d にアサイン済みです", issueID, existingIss.AssignedTeam))
		return
	}

	// Reject if an active or pending team is already working on this issue
	// (covers both the race window where AssignedTeam is not yet updated
	// and the window where Create() is still in progress).
	if o.teams.HasIssue(issueID) {
		log.Printf("[orchestrator] TEAM_CREATE rejected: active/pending team already exists for issue %s", issueID)
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s は拒否されました: 既にアクティブまたは作成中のチームが存在します", issueID))
		return
	}

	issueTitle := existingIss.Title

	// Before creating a new team, try to reuse an existing idle standby team.
	// When all maxTeams slots are occupied by standby teams (IssueID == ""),
	// calling Create() would fail with "maximum teams reached". Instead, we
	// assign the issue directly to one of the idle teams and notify its engineer.
	if idleTeam, ok := o.teams.AssignIdle(issueID, issueTitle); ok {
		log.Printf("[orchestrator] TEAM_CREATE %s: reusing idle team %d", issueID, idleTeam.ID)

		// Update the issue to reflect the new assignment.
		existingIss.AssignedTeam = idleTeam.ID
		existingIss.Status = issue.StatusInProgress
		if updErr := o.store.Update(existingIss); updErr != nil {
			log.Printf("[orchestrator] TEAM_CREATE: failed to update issue %s assignment: %v", issueID, updErr)
		}

		// Notify the idle team's engineer about the new assignment via chatlog.
		engineerID := idleTeam.Engineer.ID.String()
		o.appendOrLog(engineerID, "superintendent",
			fmt.Sprintf("イシュー %s の実装をお願いします。あなたにアサインしました。", issueID))

		// Notify superintendent that the assignment was completed.
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s: アイドルチーム %d (%s) にアサインしました", issueID, idleTeam.ID, engineerID))
		return
	}

	// No idle team available.
	// Check if we are already at the maximum team capacity.  If so, we cannot
	// create a new team and must wait until an existing team is disbanded.
	// Reject early (before marking in_progress) so the issue stays open and
	// the superintendent can retry after a team slot becomes available.
	if o.teams.Full() {
		log.Printf("[orchestrator] TEAM_CREATE %s: rejected — at max_teams capacity (%d)", issueID, o.teams.Cap())
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s は保留されました: チームが上限 (max_teams=%d) に達しています。既存チームが解放されるまで待機してください。",
				issueID, o.teams.Cap()))
		return
	}

	// Capacity is available — create a new team.
	// Mark issue as in_progress immediately to prevent the superintendent
	// from sending duplicate TEAM_CREATE commands while the team is being created.
	existingIss.Status = issue.StatusInProgress
	if err := o.store.Update(existingIss); err != nil {
		log.Printf("[orchestrator] TEAM_CREATE: failed to update issue %s status: %v", issueID, err)
	}

	log.Printf("[orchestrator] TEAM_CREATE %s: starting async team creation", issueID)

	// Send immediate ACK to superintendent so they know the command was received.
	// This prevents the superintendent from assuming the orchestrator is unresponsive
	// and retrying or falling back to direct implementation prematurely.
	o.appendOrLog("superintendent", "orchestrator",
		fmt.Sprintf("TEAM_CREATE %s: 受信しました。チーム作成を開始します。", issueID))

	// Use a context detached from the parent so that a shutdown signal does not
	// cancel the in-flight team creation.  The goroutine will still respect its
	// own internal timeouts, but it won't be killed by the orchestrator's
	// context being cancelled during a graceful restart.
	createCtx := context.WithoutCancel(ctx)

	// Run the expensive Create call in a goroutine to avoid blocking
	// the watchCommands loop (Create can take 10+ minutes waiting for LLM).
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		t, err := o.teams.Create(createCtx, issueID, issueTitle)
		if err != nil {
			log.Printf("[orchestrator] TEAM_CREATE failed for %s: %v", issueID, err)
			o.appendOrLog("superintendent", "orchestrator",
				fmt.Sprintf("TEAM_CREATE %s に失敗しました: %v", issueID, err))

			// Reset the issue status back to "open" so the superintendent can
			// retry TEAM_CREATE instead of getting stuck with in_progress forever.
			if iss, getErr := o.store.Get(issueID); getErr == nil {
				iss.Status = issue.StatusOpen
				if updErr := o.store.Update(iss); updErr != nil {
					log.Printf("[orchestrator] TEAM_CREATE: failed to reset issue %s status: %v", issueID, updErr)
				}
			}
			return
		}

		// Update issue with assigned team
		iss, err := o.store.Get(issueID)
		if err == nil {
			iss.AssignedTeam = t.ID
			iss.Status = issue.StatusInProgress
			if updErr := o.store.Update(iss); updErr != nil {
				log.Printf("[orchestrator] TEAM_CREATE: failed to update issue %s assignment: %v", issueID, updErr)
			}
		}

		log.Printf("[orchestrator] team %d created for issue %s", t.ID, issueID)
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s: チーム %d を作成しました", issueID, t.ID))
	}()
}

// Wait blocks until all in-flight asynchronous goroutines (e.g. team creation)
// have completed.  It is primarily used in tests to ensure that background work
// finishes before the test's TempDir is removed.
func (o *Orchestrator) Wait() {
	o.wg.Wait()
}

// handleTeamDisband disbands the team for an issue and cleans up its worktrees.
// Expected format: TEAM_DISBAND issue-id
func (o *Orchestrator) handleTeamDisband(body string) {
	parts := strings.Fields(body)
	if len(parts) < 2 {
		log.Printf("[orchestrator] TEAM_DISBAND missing issue ID")
		return
	}
	issueID := parts[1]

	teamNum, err := o.teams.DisbandByIssue(issueID)
	if err != nil {
		log.Printf("[orchestrator] TEAM_DISBAND failed for %s: %v", issueID, err)
		return
	}

	o.cleanTeamWorktrees(teamNum)
	log.Printf("[orchestrator] team %d disbanded for issue %s (worktrees cleaned)", teamNum, issueID)
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

// initialGitHubSync performs a one-shot GitHub sync to reflect closed issues
// before teams are started.  This prevents stale open/in_progress issues from
// being assigned to teams at startup.
// Comment sync is intentionally skipped (WithSkipComments) to keep this fast:
// the primary goal here is issue status (open/closed) — comments are fetched
// by the subsequent runGitHubSync loop.
func (o *Orchestrator) initialGitHubSync() {
	gh := o.cfg.GitHub
	botPatterns := o.compileBotPatterns()
	syncer := github.NewSyncer(o.store, gh.Owner, gh.Repos, 0).
		WithAuthorizedUsers(o.cfg.AuthorizedUsers).
		WithGhLogin(o.ghLogin()).
		WithBotCommentPatterns(botPatterns).
		WithSkipComments(true)
	if err := syncer.SyncOnce(); err != nil {
		log.Printf("[orchestrator] initial github sync failed: %v", err)
	} else {
		log.Println("[orchestrator] initial github sync completed")
	}
}

// compileBotPatterns compiles the bot_comment_patterns from the GitHub config.
// If any pattern is invalid it is logged and skipped; the valid ones are returned.
func (o *Orchestrator) compileBotPatterns() []*regexp.Regexp {
	cfg := o.Config()
	if cfg.GitHub == nil || len(cfg.GitHub.BotCommentPatterns) == 0 {
		log.Println("[orchestrator] bot_comment_patterns not configured: all comments will be forwarded to superintendent")
		return nil
	}
	patterns, err := github.CompileBotPatterns(cfg.GitHub.BotCommentPatterns)
	if err != nil {
		log.Printf("[orchestrator] invalid bot_comment_patterns (using patterns compiled so far): %v", err)
	}
	log.Printf("[orchestrator] bot_comment_patterns loaded: %d pattern(s) %v", len(patterns), cfg.GitHub.BotCommentPatterns)
	return patterns
}

// handleGitHubEvent processes a single GitHub event callback from the event watcher.
// It is extracted from runEventWatcher for testability.
func (o *Orchestrator) handleGitHubEvent(eventType github.EventType, issueID string, comment *issue.Comment) {
	switch eventType {
	case github.EventTypeIssues:
		// Notify superintendent about new/updated issue
		o.appendOrLog("superintendent", "orchestrator",
			fmt.Sprintf("GitHub Issue updated: %s", issueID))
	case github.EventTypePullRequest:
		o.handlePRMerged(issueID)
	case github.EventTypeIssueComment:
		if comment == nil {
			return
		}
		// Skip notifications for bot-generated comments (e.g. agent status
		// updates) to avoid flooding chatlog with non-human traffic.
		if comment.IsBot {
			return
		}
		// Skip notifications for closed or resolved issues to avoid delayed-notification spam.
		iss, err := o.store.Get(issueID)
		if err != nil || iss.Status == issue.StatusClosed || iss.Status == issue.StatusResolved {
			return
		}
		// Notify superintendent and the assigned team's engineer
		msg := fmt.Sprintf("New comment on %s by @%s: %s", issueID, comment.Author, comment.Body)

		o.appendOrLog("superintendent", "orchestrator", msg)

		// If the issue is assigned to a team, also notify the team engineer
		if iss.AssignedTeam > 0 {
			engineerID := fmt.Sprintf("engineer-%d", iss.AssignedTeam)
			o.appendOrLog(engineerID, "orchestrator", msg)
		}
	}
}

// handlePRMerged closes a GitHub issue and updates local state when its linked PR is merged.
func (o *Orchestrator) handlePRMerged(issueID string) {
	iss, err := o.store.Get(issueID)
	if err != nil {
		log.Printf("[orchestrator] PR merged: issue %s not found: %v", issueID, err)
		return
	}

	if iss.Status == issue.StatusClosed {
		log.Printf("[orchestrator] PR merged: issue %s already closed, skipping", issueID)
		return
	}

	// Close the GitHub issue via gh CLI (only for GitHub-synced issues with a URL)
	// Also score the issue and generate a lesson for the Superintendent.
	if iss.URL != "" {
		owner, repo, number, err := github.ParseID(issueID)
		if err == nil {
			o.closeGitHubIssue(owner, repo, number)
			// Score instruction quality and generate a lesson asynchronously so
			// that the gh CLI calls don't block the merge handler.
			go func() {
				if err := o.lessonsManager.ProcessMergedIssue(issueID, owner, repo, number); err != nil {
					log.Printf("[orchestrator] lessons: ProcessMergedIssue(%s) failed: %v", issueID, err)
				}
			}()
		} else {
			log.Printf("[orchestrator] PR merged: cannot parse issue ID %s for gh close: %v", issueID, err)
		}
	}

	// Update local issue status
	iss.Status = issue.StatusClosed
	if err := o.store.Update(iss); err != nil {
		log.Printf("[orchestrator] PR merged: update issue %s failed: %v", issueID, err)
	}

	// Disband the assigned team and clean up its worktrees
	if iss.AssignedTeam > 0 {
		if teamNum, err := o.teams.DisbandByIssue(issueID); err != nil {
			log.Printf("[orchestrator] PR merged: disband team for %s failed: %v", issueID, err)
		} else {
			o.cleanTeamWorktrees(teamNum)
		}
	}

	// Notify superintendent
	o.appendOrLog("superintendent", "orchestrator",
		fmt.Sprintf("PR merged for issue %s. Issue auto-closed and team disbanded.", issueID))

	log.Printf("[orchestrator] PR merged: issue %s closed, team disbanded", issueID)
}

// closeGitHubIssue closes an issue on GitHub via gh CLI.
func (o *Orchestrator) closeGitHubIssue(owner, repo string, number int) {
	fullRepo := fmt.Sprintf("%s/%s", owner, repo)
	cmd := exec.Command("gh", "issue", "close", fmt.Sprintf("%d", number), "-R", fullRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[orchestrator] gh issue close %s#%d failed: %v (output: %s)", fullRepo, number, err, string(out))
	} else {
		log.Printf("[orchestrator] gh issue close %s#%d succeeded", fullRepo, number)
	}
}

// runEventWatcher starts the GitHub Events API watcher for real-time updates.
func (o *Orchestrator) runEventWatcher(ctx context.Context) {
	gh := o.cfg.GitHub
	interval := time.Duration(gh.EventPollSeconds) * time.Second

	botPatterns := o.compileBotPatterns()

	idleInterval := time.Duration(gh.IdlePollMinutes) * time.Minute
	watcher := github.NewEventWatcher(o.store, gh.Owner, gh.Repos, interval, o.handleGitHubEvent).
		WithIdleDetector(o.idleDetector, idleInterval).
		WithAuthorizedUsers(o.cfg.AuthorizedUsers).
		WithBotCommentPatterns(botPatterns)
	if err := watcher.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[orchestrator] event watcher stopped: %v", err)
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
	idleInterval := time.Duration(gh.IdlePollMinutes) * time.Minute
	botPatterns := o.compileBotPatterns()
	syncer := github.NewSyncer(o.store, gh.Owner, gh.Repos, interval).
		WithIdleDetector(o.idleDetector, idleInterval).
		WithAuthorizedUsers(o.cfg.AuthorizedUsers).
		WithGhLogin(o.ghLogin()).
		WithBotCommentPatterns(botPatterns)
	if err := syncer.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[orchestrator] github sync stopped: %v", err)
	}
}

// ghLogin returns the GitHub login to use for assignee-based issue filtering.
func (o *Orchestrator) ghLogin() string {
	return o.cfg.GhLogin
}

// CreateTeamAgents implements team.TeamFactory.
func (o *Orchestrator) CreateTeamAgents(teamNum int, issueID string) (engineer *agent.Agent, err error) {
	resetInterval := time.Duration(o.cfg.Agent.ContextResetMinutes) * time.Minute
	bashTimeout := time.Duration(o.cfg.Agent.BashTimeoutMinutes) * time.Minute
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
		{agent.RoleEngineer, o.cfg.Agent.Models.Engineer},
	}

	agents := make([]*agent.Agent, 1)
	for i, r := range roles {
		vars := agent.PromptVars{
			AgentID:       fmt.Sprintf("%s-%d", r.role, teamNum),
			ChatLogPath:   o.chatLog.Path(),
			IssuesDir:     filepath.Join(o.dataDir, "issues"),
			DevelopBranch: o.cfg.Branches.Develop,
			MainBranch:    o.cfg.Branches.Main,
			FeaturePrefix: o.cfg.Branches.FeaturePrefix,
			TeamNum:       teamNumStr,
			RepoPath:      o.firstRepoPath(),
			GhLogin:       o.cfg.GhLogin,
		}

		systemPrompt, err := agent.LoadPrompt(o.promptDir, r.role, vars)
		if err != nil {
			return nil, fmt.Errorf("load prompt for %s: %w", r.role, err)
		}
		if o.cfg.Agent.ExtraPrompt != "" {
			systemPrompt += "\n\n" + o.cfg.Agent.ExtraPrompt
		}

		agentCfg := agent.AgentConfig{
			ID:            agent.AgentID{Role: r.role, TeamNum: teamNum},
			Role:          r.role,
			SystemPrompt:  systemPrompt,
			Model:         r.model,
			WorkDir:       o.firstRepoPath(),
			ChatLogPath:   o.chatLog.Path(),
			MemosDir:      filepath.Join(o.dataDir, "memos"),
			ResetInterval: resetInterval,
			BashTimeout:   bashTimeout,
			OriginalTask:  originalTask,
			Language:      o.cfg.Agent.Language,
			Dormancy:      o.dormancy,
		}
		if strings.HasPrefix(r.model, "gemini-") {
			agentCfg.Throttle = o.throttle
		}
		agents[i] = agent.NewAgent(agentCfg)
	}

	return agents[0], nil
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

// mainCheckPrompt is the message sent to the superintendent for periodic main branch checks.
const mainCheckPrompt = `定期メインブランチ動作確認の時間です。

以下の手順でmainブランチを確認してください：

1. mainブランチをチェックアウトして最新状態に更新
2. ビルドエラーがないか確認（go build ./...）
3. テストが通るか確認（go test ./...）
4. 最近マージされた変更に潜在的な不具合・改善点がないかコードレビュー

問題が見つかった場合は、GitHub Issueを作成してください。
特に問題がなければ、その旨をチャットログに記録してください。`

// runMainCheck periodically prompts the superintendent to verify the main branch.
func (o *Orchestrator) runMainCheck(ctx context.Context) {
	interval := time.Duration(o.cfg.Agent.MainCheckIntervalHours) * time.Hour
	log.Printf("[main-check] started (interval: %v)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[main-check] stopped")
			return
		case <-ticker.C:
			log.Println("[main-check] sending main branch check request to superintendent")
			o.appendOrLog("superintendent", "orchestrator", mainCheckPrompt)
		}
	}
}

// docCheckPrompt is the message sent to the superintendent for periodic doc consistency checks.
const docCheckPrompt = `定期ドキュメント整合性確認の時間です。

以下の手順でドキュメントとコードの整合性を確認してください：

1. README.md の内容と現在のコード構成・機能を比較する
2. docs/ ディレクトリ配下のドキュメント（存在する場合）を確認する
3. コマンドの使い方・設定項目・アーキテクチャ説明が現状と一致しているか確認する
4. 差異が見つかった場合：
   - feature ブランチを作成してドキュメントを修正する
   - 修正内容を GitHub Pull Request として作成する（base: develop）
   - PR の説明に差異の内容と修正理由を記載する
5. 差異が見つからない場合は、その旨をチャットログに記録する

注意: コードを修正するのではなく、ドキュメントをコードの現状に合わせて修正してください。`

// runDocCheck periodically prompts the superintendent to check doc/code consistency.
func (o *Orchestrator) runDocCheck(ctx context.Context) {
	interval := time.Duration(o.cfg.Agent.DocCheckIntervalHours) * time.Hour
	log.Printf("[doc-check] started (interval: %v)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[doc-check] stopped")
			return
		case <-ticker.C:
			log.Println("[doc-check] sending doc consistency check request to superintendent")
			o.appendOrLog("superintendent", "orchestrator", docCheckPrompt)
		}
	}
}

// issuePatrolPrompt is the message sent to the superintendent for periodic issue patrol.
const issuePatrolPrompt = `定期イシュー巡回の時間です。

以下の手順で新規イシューを確認してください：

1. イシューディレクトリを確認し、status="open" または status="in_progress" かつ assigned_team=0 のイシューがないか確認する
2. 該当イシューがあれば、チーム編成を要求する（TEAM_CREATE）
   - オーケストレーターが「チームが上限に達しています」と応答した場合は、チームが解放されるまで待機し、次の巡回で再試行してください
   - TEAM_CREATE が保留された場合はイシューのステータスが open のまま維持されます（in_progress にはなりません）
3. 進行中のチーム（assigned_team > 0）の状況をチャットログから確認する
4. resolved 状態のイシューがあればクローズ手続きを行う

特に未割り当てのイシューがないか注意してください。`

// issueStateFingerprint computes a stable string representing the current set of
// open and in-progress issues. This is used by runIssuePatrol to detect whether
// the issue state has changed since the last patrol reminder.
func (o *Orchestrator) issueStateFingerprint() string {
	all, err := o.store.List(issue.StatusFilter{})
	if err != nil {
		return ""
	}
	var ids []string
	for _, iss := range all {
		if iss.Status == issue.StatusOpen || iss.Status == issue.StatusInProgress {
			ids = append(ids, iss.ID)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// runIssuePatrol periodically prompts the superintendent to check for new issues.
// This prevents the superintendent from becoming idle during long periods without
// chatlog messages, which was reported as GitHub Issue #155.
//
// The reminder is suppressed when the issue state (set of open/in-progress issues)
// has not changed since the last reminder, preventing chatlog bloat during idle periods.
// The patrol timer is also reset whenever the superintendent sends a message,
// so reminders are deferred when the superintendent is already active.
// When the superintendent sends PATROL_COMPLETE, the interval timer is reset so
// the next reminder fires N minutes after patrol completion.
func (o *Orchestrator) runIssuePatrol(ctx context.Context) {
	interval := time.Duration(o.cfg.Agent.IssuePatrolIntervalMinutes) * time.Minute
	log.Printf("[issue-patrol] started (interval: %v)", interval)

	// Record chatlog size at startup to detect subsequent activity.
	var lastSize int64
	if info, err := os.Stat(o.chatLog.Path()); err == nil {
		lastSize = info.Size()
	}

	// Watch all chatlog messages so we can detect superintendent activity and
	// reset the patrol timer (Proposal 3).
	allMsgCh := o.chatLog.WatchAll(ctx)

	timer := time.NewTimer(interval)
	defer timer.Stop()

	// Track the issue state at the time of the last sent reminder to detect changes.
	lastSentFingerprint := ""

	for {
		select {
		case <-ctx.Done():
			log.Println("[issue-patrol] stopped")
			return

		case <-o.patrolResetCh:
			// Superintendent reported PATROL_COMPLETE: reset the timer so the next
			// reminder fires N minutes after patrol completion rather than after the
			// last scheduled tick.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(interval)
			log.Println("[issue-patrol] timer reset after PATROL_COMPLETE")

		case msg, ok := <-allMsgCh:
			if !ok {
				return
			}
			// Whenever the superintendent sends a message it is actively working;
			// reset the patrol timer so we don't interrupt it with a reminder.
			if msg.Sender == "superintendent" {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(interval)
				log.Printf("[issue-patrol] timer reset: superintendent is active")
			}

		case <-timer.C:
			// Skip the reminder if the issue state hasn't changed and chatlog
			// hasn't grown since the last patrol — no new activity to patrol.
			current := o.issueStateFingerprint()
			info, err := os.Stat(o.chatLog.Path())
			if err == nil {
				currentSize := info.Size()
				if currentSize <= lastSize && current == lastSentFingerprint {
					log.Println("[issue-patrol] no activity since last patrol, skipping reminder")
					timer.Reset(interval)
					break
				}
				lastSize = currentSize
			}

			log.Println("[issue-patrol] sending issue patrol request to superintendent")
			// Prepend any accumulated lessons so the Superintendent can reference
			// past failure patterns when writing new issue instructions.
			patrolMsg := o.lessonsManager.InjectLessons() + issuePatrolPrompt
			o.appendOrLog("superintendent", "orchestrator", patrolMsg)
			lastSentFingerprint = current
			timer.Reset(interval)
		}
	}
}

// runBranchCleanup periodically deletes merged feature branches from all repos.
func (o *Orchestrator) runBranchCleanup(ctx context.Context) {
	interval := time.Duration(o.cfg.Branches.CleanupIntervalMinutes) * time.Minute
	log.Printf("[branch-cleanup] started (interval: %v)", interval)

	protected := []string{o.cfg.Branches.Main, o.cfg.Branches.Develop}
	featurePrefix := o.cfg.Branches.FeaturePrefix

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[branch-cleanup] stopped")
			return
		case <-ticker.C:
			for name, repo := range o.repos {
				cleaner := git.NewBranchCleaner(repo, protected, featurePrefix)
				deleted, err := cleaner.CleanMergedBranches(o.cfg.Branches.Develop)
				if err != nil {
					log.Printf("[branch-cleanup] %s: %v", name, err)
					continue
				}
				if len(deleted) > 0 {
					log.Printf("[branch-cleanup] %s: deleted %d merged branches: %v", name, len(deleted), deleted)
				}
			}
		}
	}
}

// runWorktreeCleanup periodically removes orphaned git worktrees that are
// not associated with any active team. This prevents disk space accumulation
// from worktrees left behind by crashed or improperly cleaned up teams.
func (o *Orchestrator) runWorktreeCleanup(ctx context.Context) {
	interval := time.Duration(o.cfg.Agent.WorktreeCleanupIntervalMinutes) * time.Minute
	log.Printf("[worktree-cleanup] started (interval: %v)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[worktree-cleanup] stopped")
			return
		case <-ticker.C:
			// Build set of active worktree relative paths.
			// Legacy style: "team-N"; namespaced style: "{ghLogin}/issue-{id}".
			activePaths := make(map[string]bool)
			for _, info := range o.teams.List() {
				activePaths[fmt.Sprintf("team-%d", info.ID)] = true
			}

			o.cfgMu.RLock()
			ghLogin := o.cfg.GhLogin
			o.cfgMu.RUnlock()

			for name, repo := range o.repos {
				removed := repo.CleanOrphanedWorktrees(ghLogin, activePaths)
				if len(removed) > 0 {
					log.Printf("[worktree-cleanup] %s: removed %d orphaned worktree(s): %v", name, len(removed), removed)
				}
			}
		}
	}
}

// runMergedWorktreeCleanup periodically removes worktrees whose associated
// GitHub PRs have been merged or closed. It scans .worktrees/{ghLogin}/ for
// each configured repo, checks PR state via the gh CLI, and removes the
// worktree, local branch, and remote branch for merged/closed PRs.
//
// Remote branch deletion failures are non-fatal: they are logged and the
// cleanup will be retried on the next interval.
//
// This goroutine does not block the main polling loop.
func (o *Orchestrator) runMergedWorktreeCleanup(ctx context.Context) {
	o.cfgMu.RLock()
	interval := time.Duration(o.cfg.Agent.MergedWorktreeCleanupIntervalMinutes) * time.Minute
	o.cfgMu.RUnlock()

	log.Printf("[merged-worktree-cleanup] started (interval: %v)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[merged-worktree-cleanup] stopped")
			return
		case <-ticker.C:
			o.cfgMu.RLock()
			gh := o.cfg.GitHub
			ghLogin := o.cfg.GhLogin
			o.cfgMu.RUnlock()

			if gh == nil || ghLogin == "" {
				// GitHub integration not configured or login not resolved.
				continue
			}

			for _, repoName := range gh.Repos {
				for name, repo := range o.repos {
					removed, err := repo.CleanMergedPRWorktrees(gh.Owner, repoName, ghLogin)
					if err != nil {
						log.Printf("[merged-worktree-cleanup] %s: error scanning worktrees: %v", name, err)
						continue
					}
					if len(removed) > 0 {
						log.Printf("[merged-worktree-cleanup] %s: removed %d merged/closed worktree(s): %v", name, len(removed), removed)
					}
				}
			}
		}
	}
}

// runConfigWatcher watches the madflow.toml config file for changes.
// When a valid new config is detected, it is atomically applied to the
// orchestrator (safe for concurrent reads via Config()).
// Fields that affect already-running goroutines (e.g. poll intervals, model
// names) will take effect on the next relevant cycle automatically because
// those goroutines read cfg through Config().
func (o *Orchestrator) runConfigWatcher(ctx context.Context) {
	w := config.NewWatcher(o.configPath)
	log.Printf("[config-watcher] watching %s for changes", o.configPath)

	cfgCh := w.Watch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case newCfg, ok := <-cfgCh:
			if !ok {
				return
			}
			o.cfgMu.Lock()
			o.cfg = newCfg
			o.cfgMu.Unlock()
			// Propagate max_teams changes to the team manager so that
			// hot-reload updates take effect without restarting the process.
			o.teams.SetMaxTeams(newCfg.Agent.MaxTeams)
			log.Println("[config-watcher] active config updated")
		}
	}
}

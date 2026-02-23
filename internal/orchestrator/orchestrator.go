package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
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
	cfg        *config.Config
	cfgMu      sync.RWMutex // protects cfg for hot-reload
	configPath string       // path to madflow.toml for hot-reload watcher
	dataDir    string
	promptDir  string

	store        *issue.Store
	chatLog      *chatlog.ChatLog
	teams        *team.Manager
	repos        map[string]*git.Repo // name -> repo
	dormancy     *agent.Dormancy
	idleDetector *github.IdleDetector // shared idle state for GitHub polling

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

	idleDetector := github.NewIdleDetector()
	if cfg.GitHub != nil && cfg.GitHub.IdleThresholdMinutes > 0 {
		idleDetector.SetIdleThreshold(time.Duration(cfg.GitHub.IdleThresholdMinutes) * time.Minute)
	}
	if cfg.GitHub != nil && cfg.GitHub.DormancyThresholdMinutes > 0 {
		idleDetector.SetDormancyThreshold(time.Duration(cfg.GitHub.DormancyThresholdMinutes) * time.Minute)
	}

	orc := &Orchestrator{
		cfg:          cfg,
		dataDir:      dataDir,
		promptDir:    promptDir,
		store:        issue.NewStore(issuesDir),
		chatLog:      chatlog.New(chatLogPath),
		repos:        repos,
		dormancy:     agent.NewDormancy(),
		idleDetector: idleDetector,
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

	// Start branch cleanup goroutine if configured
	if o.cfg.Branches.CleanupIntervalMinutes > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.runBranchCleanup(ctx)
		}()
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

	// Watch chatlog for orchestrator commands
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.watchCommands(ctx)
	}()

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

// startResidentAgents starts the superintendent.
func (o *Orchestrator) startResidentAgents(ctx context.Context, wg *sync.WaitGroup) error {
	resetInterval := time.Duration(o.cfg.Agent.ContextResetMinutes) * time.Minute

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
		}

		systemPrompt, err := agent.LoadPrompt(o.promptDir, r.role, vars)
		if err != nil {
			return fmt.Errorf("load prompt for %s: %w", r.role, err)
		}
		if o.cfg.Agent.ExtraPrompt != "" {
			systemPrompt += "\n\n" + o.cfg.Agent.ExtraPrompt
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
	case strings.HasPrefix(body, "WAKE_GITHUB"):
		o.handleWakeGitHub()
	default:
		log.Printf("[orchestrator] unknown command from %s: %s", msg.Sender, body)
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

// handleTeamCreate creates a new team for an issue.
// Expected format: TEAM_CREATE issue-id
func (o *Orchestrator) handleTeamCreate(ctx context.Context, body string) {
	parts := strings.Fields(body)
	if len(parts) < 2 {
		log.Printf("[orchestrator] TEAM_CREATE missing issue ID")
		return
	}
	issueID := parts[1]

	// Validate that the issue exists in the store to reject malformed IDs
	// (e.g. "issueID（2回目）extra text" from retried TEAM_CREATE messages).
	if _, err := o.store.Get(issueID); err != nil {
		log.Printf("[orchestrator] TEAM_CREATE rejected: issue %q not found: %v", issueID, err)
		o.chatLog.Append("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s は拒否されました: イシューが見つかりません", issueID))
		return
	}

	t, err := o.teams.Create(ctx, issueID)
	if err != nil {
		log.Printf("[orchestrator] TEAM_CREATE failed for %s: %v", issueID, err)
		o.chatLog.Append("superintendent", "orchestrator",
			fmt.Sprintf("TEAM_CREATE %s に失敗しました: %v", issueID, err))
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
	o.chatLog.Append("superintendent", "orchestrator",
		fmt.Sprintf("TEAM_CREATE %s: チーム %d を作成しました", issueID, t.ID))
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

// compileBotPatterns compiles the bot_comment_patterns from the GitHub config.
// If any pattern is invalid it is logged and skipped; the valid ones are returned.
func (o *Orchestrator) compileBotPatterns() []*regexp.Regexp {
	cfg := o.Config()
	if cfg.GitHub == nil || len(cfg.GitHub.BotCommentPatterns) == 0 {
		return nil
	}
	patterns, err := github.CompileBotPatterns(cfg.GitHub.BotCommentPatterns)
	if err != nil {
		log.Printf("[orchestrator] invalid bot_comment_patterns (using patterns compiled so far): %v", err)
	}
	return patterns
}

// runEventWatcher starts the GitHub Events API watcher for real-time updates.
func (o *Orchestrator) runEventWatcher(ctx context.Context) {
	gh := o.cfg.GitHub
	interval := time.Duration(gh.EventPollSeconds) * time.Second

	botPatterns := o.compileBotPatterns()

	callback := func(eventType github.EventType, issueID string, comment *issue.Comment) {
		switch eventType {
		case github.EventTypeIssues:
			// Notify superintendent about new/updated issue
			o.chatLog.Append("superintendent", "orchestrator",
				fmt.Sprintf("GitHub Issue updated: %s", issueID))
		case github.EventTypeIssueComment:
			if comment == nil {
				return
			}
			// Skip notifications for bot-generated comments (e.g. agent status
			// updates) to avoid flooding chatlog with non-human traffic.
			if comment.IsBot {
				return
			}
			// Skip notifications for closed issues to avoid delayed-notification spam.
			iss, err := o.store.Get(issueID)
			if err != nil || iss.Status == issue.StatusClosed {
				return
			}
			// Notify superintendent and the assigned team's engineer
			msg := fmt.Sprintf("New comment on %s by @%s: %s", issueID, comment.Author, comment.Body)

			o.chatLog.Append("superintendent", "orchestrator", msg)

			// If the issue is assigned to a team, also notify the team engineer
			if iss.AssignedTeam > 0 {
				engineerID := fmt.Sprintf("engineer-%d", iss.AssignedTeam)
				o.chatLog.Append(engineerID, "orchestrator", msg)
			}
		}
	}

	idleInterval := time.Duration(gh.IdlePollMinutes) * time.Minute
	watcher := github.NewEventWatcher(o.store, gh.Owner, gh.Repos, interval, callback).
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
		WithBotCommentPatterns(botPatterns)
	if err := syncer.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[orchestrator] github sync stopped: %v", err)
	}
}

// CreateTeamAgents implements team.TeamFactory.
func (o *Orchestrator) CreateTeamAgents(teamNum int, issueID string) (engineer *agent.Agent, err error) {
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
		}

		systemPrompt, err := agent.LoadPrompt(o.promptDir, r.role, vars)
		if err != nil {
			return nil, fmt.Errorf("load prompt for %s: %w", r.role, err)
		}
		if o.cfg.Agent.ExtraPrompt != "" {
			systemPrompt += "\n\n" + o.cfg.Agent.ExtraPrompt
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
			o.chatLog.Append("superintendent", "orchestrator", mainCheckPrompt)
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
			o.chatLog.Append("superintendent", "orchestrator", docCheckPrompt)
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
			log.Println("[config-watcher] active config updated")
		}
	}
}

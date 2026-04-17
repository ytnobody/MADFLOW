package config

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Project    ProjectConfig `toml:"project"`
	Agent      AgentConfig   `toml:"agent"`
	Branches   BranchConfig  `toml:"branches"`
	GitHub     *GitHubConfig `toml:"github,omitempty"`
	PromptsDir string        `toml:"prompts_dir,omitempty"`
	// AuthorizedUsers is a list of GitHub user logins that are allowed to create
	// issues, PRs, and comments that MADFLOW will process.
	//
	// Deprecated: This field no longer needs to be set manually. When [github]
	// integration is enabled and this field is empty, MADFLOW automatically
	// detects the authenticated GitHub CLI user via `gh api user --jq '.login'`
	// at startup and uses that login as the sole authorized user.
	//
	// If explicitly set, the specified values take priority over auto-detection.
	// Leaving it empty with GitHub integration active and `gh` not authenticated
	// will cause MADFLOW to deny all incoming GitHub events.
	AuthorizedUsers []string `toml:"authorized_users,omitempty"`
	// GhLogin is the GitHub login name of the authenticated user, auto-detected
	// at startup via `gh api user --jq '.login'`. It is a runtime-only field
	// (not read from TOML) and is used to namespace branch names and worktree
	// paths per user (e.g. "madflow/{gh_login}/issue-{id}").
	// Empty if the GitHub CLI is unavailable or not authenticated.
	GhLogin string `toml:"-"`
}

type ProjectConfig struct {
	Name  string       `toml:"name"`
	Repos []RepoConfig `toml:"repos"`
}

type RepoConfig struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

type AgentConfig struct {
	ContextResetMinutes int         `toml:"context_reset_minutes"`
	MaxTeams            int         `toml:"max_teams"`
	ChatlogMaxLines     int         `toml:"chatlog_max_lines"`
	Models              ModelConfig `toml:"models"`
	// MainCheckIntervalHours specifies how often the superintendent is prompted to
	// check the main branch for bugs and improvement opportunities.
	// 0 disables the periodic check. Defaults to 6 hours.
	MainCheckIntervalHours int `toml:"main_check_interval_hours"`
	// DocCheckIntervalHours specifies how often the superintendent is prompted to
	// check that documentation is consistent with the current codebase and create
	// fix PRs when discrepancies are found.
	// 0 triggers the default of 24 hours.
	DocCheckIntervalHours int `toml:"doc_check_interval_hours"`
	// GeminiRPM is the requests-per-minute limit for the Gemini API.
	// All Gemini agents share a single sliding-window throttle based on this value.
	// Defaults to 10.
	GeminiRPM int `toml:"gemini_rpm"`
	// DormancyProbeMinutes is the initial interval between rate-limit recovery probes
	// when all agents enter dormancy due to a rate limit error.
	// The interval doubles after each failed probe (exponential backoff), up to 5 minutes.
	// Shorter values help recover faster from transient rate limits (useful for Gemini).
	// Defaults to 3 minutes.
	DormancyProbeMinutes int `toml:"dormancy_probe_minutes"`
	// BashTimeoutMinutes is the maximum time a single bash command is allowed
	// to run before being killed. This prevents agents from hanging indefinitely
	// on commands that never finish. Defaults to 5 minutes.
	BashTimeoutMinutes int `toml:"bash_timeout_minutes"`
	// IssuePatrolIntervalMinutes specifies how often the orchestrator sends a
	// periodic issue-patrol reminder to the superintendent. The reminder is
	// suppressed when the issue state (set of open/in-progress issues) has not
	// changed since the last reminder, preventing chatlog bloat during idle periods.
	// Sending "PATROL_COMPLETE" to the orchestrator resets the timer so the next
	// reminder is issued N minutes after patrol completion rather than after the
	// last scheduled tick.
	// 0 triggers the default of 20 minutes. Set to -1 to disable.
	IssuePatrolIntervalMinutes int `toml:"issue_patrol_interval_minutes"`
	// WorktreeCleanupIntervalMinutes specifies how often to check for and remove
	// orphaned git worktrees (those not associated with any active team).
	// 0 (default) disables periodic worktree cleanup.
	WorktreeCleanupIntervalMinutes int `toml:"worktree_cleanup_interval_minutes"`
	// MergedWorktreeCleanupIntervalMinutes specifies how often to scan worktrees
	// under .worktrees/{ghLogin}/ and remove those whose associated GitHub PRs have
	// been merged or closed. The removal includes the worktree, local branch, and
	// remote branch. 0 (default) disables this cleanup.
	MergedWorktreeCleanupIntervalMinutes int `toml:"merged_worktree_cleanup_interval_minutes"`
	// ExtraPrompt is appended to the system prompt of every agent.
	// Use this to inject project-specific instructions that apply to all agents.
	ExtraPrompt string `toml:"extra_prompt"`
	// Language specifies the language for agent messages (e.g. "en", "ja").
	// Defaults to "en". This controls the language of internal agent
	// communication messages such as chatlog prompts and initial instructions.
	Language string `toml:"language"`
}

type ModelConfig struct {
	Superintendent string `toml:"superintendent"`
	Engineer       string `toml:"engineer"`
}

type BranchConfig struct {
	Main          string `toml:"main"`
	Develop       string `toml:"develop"`
	FeaturePrefix string `toml:"feature_prefix"`
	// CleanupIntervalMinutes specifies how often to delete merged feature branches
	// from all configured repos. 0 (default) disables branch cleanup.
	CleanupIntervalMinutes int `toml:"cleanup_interval_minutes"`
}

type GitHubConfig struct {
	Owner               string   `toml:"owner"`
	Repos               []string `toml:"repos"`
	SyncIntervalMinutes int      `toml:"sync_interval_minutes"`
	EventPollSeconds    int      `toml:"event_poll_seconds"`
	// IdlePollMinutes specifies how often to poll when no open issues are found.
	// Defaults to 15 minutes. Must be greater than EventPollSeconds/60 to have any effect.
	IdlePollMinutes int `toml:"idle_poll_minutes"`
	// IdleThresholdMinutes is how long there must be no open issues before
	// entering idle mode. Defaults to 5 minutes.
	IdleThresholdMinutes int `toml:"idle_threshold_minutes"`
	// DormancyThresholdMinutes is how long there must be no open issues (measured from
	// when issues first disappeared) before entering dormancy and completely stopping
	// GitHub API polling. 0 (default) disables dormancy. Should be larger than
	// IdleThresholdMinutes to form a natural progression: active → idle → dormant.
	// Dormancy can be exited via the WAKE_GITHUB orchestrator command.
	DormancyThresholdMinutes int `toml:"dormancy_threshold_minutes"`
	// BotCommentPatterns is a list of regular-expression patterns used to detect
	// bot-generated issue comments by their body content.  A comment whose body
	// matches any of these patterns is marked with IsBot=true, which suppresses
	// unnecessary chatlog notifications inside MADFLOW.
	//
	// This is useful when MADFLOW agents share a GitHub account with a human
	// owner (the common single-account setup): bot-posted status updates can be
	// detected by their predictable format rather than by account name.
	//
	// Example: ["^\\*\\*\\["] matches all MADFLOW agent status comments that
	// start with **[実装開始]**, **[実装完了]**, **[質問]**, etc.
	BotCommentPatterns []string `toml:"bot_comment_patterns,omitempty"`
}

// RawConfig holds config fields exactly as parsed from TOML, without defaults or validation.
// Use ParseConfig to convert a RawConfig into a validated Config.
type RawConfig struct {
	Project         ProjectConfig `toml:"project"`
	Agent           AgentConfig   `toml:"agent"`
	Branches        BranchConfig  `toml:"branches"`
	GitHub          *GitHubConfig `toml:"github,omitempty"`
	PromptsDir      string        `toml:"prompts_dir,omitempty"`
	AuthorizedUsers []string      `toml:"authorized_users,omitempty"`
}

// ParseConfig parses TOML bytes into a validated Config.
// This is a pure function: it performs no I/O and no filesystem access.
// All defaults are applied and structural validation is enforced.
// Runtime fields such as GhLogin are left empty; call Load to populate those.
func ParseConfig(data []byte) (*Config, error) {
	var raw RawConfig
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg := &Config{
		Project:         raw.Project,
		Agent:           raw.Agent,
		Branches:        raw.Branches,
		GitHub:          raw.GitHub,
		PromptsDir:      raw.PromptsDir,
		AuthorizedUsers: raw.AuthorizedUsers,
	}

	setDefaults(cfg)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

// Load reads the config file at path, parses and validates it via ParseConfig,
// then applies I/O side effects (gh CLI login detection, authorized user auto-detection).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, err
	}

	// I/O stage: resolve runtime fields that depend on external processes.
	applyGhLogin(cfg)
	warnDefaults(cfg)
	autoPopulateAuthorizedUsers(cfg)

	return cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Agent.ContextResetMinutes == 0 {
		cfg.Agent.ContextResetMinutes = 15
	}
	if cfg.Agent.Models.Superintendent == "" {
		cfg.Agent.Models.Superintendent = "claude-sonnet-4-6"
	}
	if cfg.Agent.Models.Engineer == "" {
		cfg.Agent.Models.Engineer = "claude-haiku-4-5"
	}
	if cfg.Agent.MaxTeams == 0 {
		cfg.Agent.MaxTeams = 4
	}
	if cfg.Agent.ChatlogMaxLines == 0 {
		cfg.Agent.ChatlogMaxLines = 500
	}
	if cfg.Agent.MainCheckIntervalHours == 0 {
		cfg.Agent.MainCheckIntervalHours = 6
	}
	if cfg.Agent.DocCheckIntervalHours == 0 {
		cfg.Agent.DocCheckIntervalHours = 24
	}
	if cfg.Agent.GeminiRPM == 0 {
		cfg.Agent.GeminiRPM = 10
	}
	if cfg.Agent.DormancyProbeMinutes == 0 {
		cfg.Agent.DormancyProbeMinutes = 3
	}
	if cfg.Agent.BashTimeoutMinutes == 0 {
		cfg.Agent.BashTimeoutMinutes = 5
	}
	if cfg.Agent.IssuePatrolIntervalMinutes == 0 {
		cfg.Agent.IssuePatrolIntervalMinutes = 20
	}
	if cfg.Agent.Language == "" {
		cfg.Agent.Language = "en"
	}
	if cfg.Branches.Main == "" {
		cfg.Branches.Main = "main"
	}
	if cfg.Branches.Develop == "" {
		cfg.Branches.Develop = "develop"
	}
	// FeaturePrefix default is applied after GhLogin is resolved in applyGhLogin.
	// Leave it empty here so applyGhLogin can detect whether the user set it explicitly.
	if cfg.GitHub != nil && cfg.GitHub.SyncIntervalMinutes == 0 {
		cfg.GitHub.SyncIntervalMinutes = 15
	}
	if cfg.GitHub != nil && cfg.GitHub.EventPollSeconds == 0 {
		cfg.GitHub.EventPollSeconds = 60
	}
	if cfg.GitHub != nil && cfg.GitHub.IdlePollMinutes == 0 {
		cfg.GitHub.IdlePollMinutes = 15
	}
	if cfg.GitHub != nil && cfg.GitHub.IdleThresholdMinutes == 0 {
		cfg.GitHub.IdleThresholdMinutes = 5
	}
	// DormancyThresholdMinutes intentionally has no default (0 = disabled).
	// Users must opt-in by setting a positive value in their config.
}

func warnDefaults(cfg *Config) {
	if cfg.Agent.ContextResetMinutes < 10 {
		log.Printf("[config] WARNING: context_reset_minutes=%d is below 10; short intervals cause redundant completions — consider 15+", cfg.Agent.ContextResetMinutes)
	}
}

func validate(cfg *Config) error {
	if cfg.Project.Name == "" {
		return fmt.Errorf("project.name is required")
	}
	if len(cfg.Project.Repos) == 0 {
		return fmt.Errorf("at least one project.repos entry is required")
	}
	for i, r := range cfg.Project.Repos {
		if r.Name == "" {
			return fmt.Errorf("project.repos[%d].name is required", i)
		}
		if r.Path == "" {
			return fmt.Errorf("project.repos[%d].path is required", i)
		}
	}
	return nil
}

// resolveGitHubLogin calls the GitHub CLI to get the currently authenticated
// user's login name. Returns an empty string if the CLI is unavailable or the
// user is not authenticated.
func resolveGitHubLogin() string {
	cmd := exec.Command("gh", "api", "user", "--jq", ".login")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// applyGhLogin resolves the GitHub login and applies it to cfg.GhLogin and,
// when FeaturePrefix was not explicitly set in the config file, sets the
// namespaced default: "madflow/{gh_login}/issue-".
// Falls back to "feature/issue-" when the GitHub CLI is unavailable.
func applyGhLogin(cfg *Config) {
	cfg.GhLogin = resolveGitHubLogin()
	if cfg.Branches.FeaturePrefix == "" {
		if cfg.GhLogin != "" {
			cfg.Branches.FeaturePrefix = "madflow/" + cfg.GhLogin + "/issue-"
		} else {
			cfg.Branches.FeaturePrefix = "feature/issue-"
		}
	}
}

// autoPopulateAuthorizedUsers uses the already-resolved cfg.GhLogin to populate
// cfg.AuthorizedUsers when GitHub integration is enabled but no authorized
// users are configured. A warning is logged when detection fails.
func autoPopulateAuthorizedUsers(cfg *Config) {
	if cfg.GitHub == nil || len(cfg.AuthorizedUsers) > 0 {
		// GitHub integration disabled, or already explicitly configured.
		return
	}
	if cfg.GhLogin == "" {
		log.Printf("[config] WARNING: github integration is enabled but authorized_users is not set and `gh api user` returned no login; all GitHub events will be denied. Set authorized_users in madflow.toml or ensure `gh` is authenticated.")
		return
	}
	log.Printf("[config] auto-detected GitHub login %q; using as authorized_users", cfg.GhLogin)
	cfg.AuthorizedUsers = []string{cfg.GhLogin}
}

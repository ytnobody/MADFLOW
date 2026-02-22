package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Project    ProjectConfig  `toml:"project"`
	Agent      AgentConfig    `toml:"agent"`
	Branches   BranchConfig   `toml:"branches"`
	GitHub     *GitHubConfig  `toml:"github,omitempty"`
	PromptsDir string         `toml:"prompts_dir,omitempty"`
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
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	setDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Agent.ContextResetMinutes == 0 {
		cfg.Agent.ContextResetMinutes = 8
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
	if cfg.Branches.Main == "" {
		cfg.Branches.Main = "main"
	}
	if cfg.Branches.Develop == "" {
		cfg.Branches.Develop = "develop"
	}
	if cfg.Branches.FeaturePrefix == "" {
		cfg.Branches.FeaturePrefix = "feature/issue-"
	}
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

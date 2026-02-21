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
	Models              ModelConfig `toml:"models"`
}

type ModelConfig struct {
	Superintendent string `toml:"superintendent"`
	PM             string `toml:"pm"`
	Architect      string `toml:"architect"`
	Engineer       string `toml:"engineer"`
	Reviewer       string `toml:"reviewer"`
	ReleaseManager string `toml:"release_manager"`
}

type BranchConfig struct {
	Main          string `toml:"main"`
	Develop       string `toml:"develop"`
	FeaturePrefix string `toml:"feature_prefix"`
}

type GitHubConfig struct {
	Owner                string   `toml:"owner"`
	Repos                []string `toml:"repos"`
	SyncIntervalMinutes  int      `toml:"sync_interval_minutes"`
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
		cfg.Agent.Models.Superintendent = "claude-opus-4-6"
	}
	if cfg.Agent.Models.PM == "" {
		cfg.Agent.Models.PM = "claude-sonnet-4-6"
	}
	if cfg.Agent.Models.Architect == "" {
		cfg.Agent.Models.Architect = "claude-opus-4-6"
	}
	if cfg.Agent.Models.Engineer == "" {
		cfg.Agent.Models.Engineer = "claude-sonnet-4-6"
	}
	if cfg.Agent.Models.Reviewer == "" {
		cfg.Agent.Models.Reviewer = "claude-sonnet-4-6"
	}
	if cfg.Agent.Models.ReleaseManager == "" {
		cfg.Agent.Models.ReleaseManager = "claude-haiku-4-5"
	}
	if cfg.Agent.MaxTeams == 0 {
		cfg.Agent.MaxTeams = 4
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
		cfg.GitHub.SyncIntervalMinutes = 5
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

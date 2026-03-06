<p align="center">
  <img src="https://github.com/user-attachments/assets/77200c22-452b-459e-84b5-88ab0dc80703" alt="MADFLOW Logo" width="400">
</p>

# MADFLOW

MADFLOW (Multi-Agent Development Flow) is a development framework where multiple AI agents collaborate as a team to autonomously advance software development.

## Features

- **Simple 2-agent structure**: Consists of only a Superintendent and Engineers
- **Centralized Superintendent management**: The Superintendent oversees PM, design, review, and merging
- **Autonomous task management**: AI agents handle everything from issue creation to implementation, review, and merging
- **Context reset functionality**: Automatic refresh mechanism to prevent AI performance degradation
- **Git/GitHub integration**: Automatically manages branch strategy and issue synchronization

## Requirements

- Go 1.25 or higher
- Git
- One of the following (depending on the backend you use):
  - [Claude Code](https://claude.com/claude-code) (`claude` command) - when using the Claude CLI backend
  - [gemini-cli](https://github.com/google-gemini/gemini-cli) (`gemini-cli` command) - when using Gemini models
  - `ANTHROPIC_API_KEY` environment variable - when using the Anthropic API key backend (no additional installation required)
- GitHub CLI (`gh`) (when using GitHub Issue synchronization)

## Installation

### Using go install

```bash
go install github.com/ytnobody/madflow/cmd/madflow@latest
```

### Downloading a binary from GitHub Releases

Download the binary for your OS and architecture from [GitHub Releases](https://github.com/ytnobody/madflow/releases/latest).

```bash
# Example for Linux (amd64)
curl -L https://github.com/ytnobody/madflow/releases/latest/download/madflow-linux-amd64 -o madflow
chmod +x madflow
sudo mv madflow /usr/local/bin/
```

After installation, you can also upgrade to the latest version with the `madflow upgrade` command.

## Quick Start

### 1. Initialize the project

```bash
cd your-project
madflow init
```

A `madflow.toml` file will be generated. Edit the configuration as needed.

### 2. Start the agents

```bash
madflow start
```

## Configuration

Configuration is managed via `madflow.toml` in the project root.

```toml
[project]
name = "my-app"

[[project.repos]]
name = "main"
path = "."

[agent]
context_reset_minutes = 8

[agent.models]
superintendent = "claude-opus-4-6"
engineer = "claude-sonnet-4-6"
# Gemini models are also supported:
# superintendent = "gemini-2.0-flash-exp"
# engineer = "gemini-2.5-pro"
# Anthropic API key method (requires ANTHROPIC_API_KEY environment variable):
# superintendent = "anthropic/claude-sonnet-4-6"
# engineer = "anthropic/claude-haiku-4-5"

[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"
```

### GitHub Issue Sync (Optional)

```toml
[github]
owner = "myorg"
repos = ["my-app"]
sync_interval_minutes = 5
```

## Command Reference

| Command | Description |
|---------|-------------|
| `madflow init` | Initialize the project |
| `madflow start` | Start all agents |
| `madflow use <preset>` | Switch model preset |
| `madflow version` | Display the current version |
| `madflow upgrade` | Upgrade madflow to the latest version |

## Model Presets

You can switch the models in use with the `madflow use <preset>` command.

| Preset | superintendent | engineer | Notes |
|--------|---------------|----------|-------|
| `claude` | claude-sonnet-4-6 | claude-sonnet-4-6 | Claude CLI (requires Pro/Max) |
| `claude-cheap` | claude-sonnet-4-6 | claude-haiku-4-5 | Claude CLI cost-reduced version |
| `gemini` | gemini-2.5-pro | gemini-2.5-pro | Gemini CLI (requires gemini-cli) |
| `gemini-cheap` | gemini-2.5-flash | gemini-2.5-flash | Gemini fast & low-cost version |
| `hybrid` | claude-sonnet-4-6 | gemini-2.5-pro | Hybrid configuration |
| `hybrid-cheap` | claude-sonnet-4-6 | gemini-2.5-flash | Hybrid low-cost version |
| `claude-api-standard` | anthropic/claude-sonnet-4-6 | anthropic/claude-haiku-4-5 | **Anthropic API key method** |
| `claude-api-cheap` | anthropic/claude-haiku-4-5 | anthropic/claude-haiku-4-5 | **Anthropic API key method - cheapest** |

### How to Use the Anthropic API Key Method

The `claude-api-*` presets call Anthropic's API directly using `ANTHROPIC_API_KEY` instead of the Claude Code CLI.

**Benefits:**
- No Claude Code Pro/Max subscription required
- Predictable costs with pay-as-you-go pricing
- Independent from Anthropic policy change risks

**Setup:**

```bash
# 1. Set the Anthropic API key as an environment variable
export ANTHROPIC_API_KEY="sk-ant-..."

# 2. Switch to an API key preset
madflow use claude-api-standard   # standard quality
# or
madflow use claude-api-cheap      # lowest cost

# 3. Start
madflow start
```

### Cost Comparison (Reference)

| Method | Estimated monthly cost | Notes |
|--------|------------------------|-------|
| Claude Max (5x) | ¥15,000 | Fixed subscription fee |
| `claude-api-standard` | ¥3,000–8,000 | Pay-as-you-go, varies by usage |
| `claude-api-cheap` | ¥1,000–3,000 | Uses Haiku model |
| `hybrid-cheap` | Within gemini-cli free tier | Gemini Flash has a free tier |

> ※ API pricing is an estimate as of February 2026. For actual pricing, refer to the [Anthropic official website](https://www.anthropic.com/pricing).

## Architecture

For detailed specifications, refer to [SPEC.md](./SPEC.md). For the implementation plan, refer to [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md).

## License

MIT License

## Development Workflow

In the MADFLOW framework, multiple agents work in parallel on the same repository, which frequently causes branch conflicts. To avoid this, each engineer must use `git worktree` to isolate their own working directory.

### Setting Up `git worktree`

When working on a new issue, create a new worktree with the following command:

```bash
# Example: for issue local-002
git worktree add -b feature/issue-local-002 ../madflow-worktree/local-002 develop
```

### Development Cycle

1. **Create worktree**: Use the command above to create a worktree for each issue.
2. **Implementation**: Move to the created worktree directory and implement, then commit.
3. **Create Pull Request**: Use GitHub CLI (`gh pr create`) to create a Pull Request.
4. **Remove worktree**: Once the Pull Request is merged, remove the no-longer-needed worktree.

```bash
# Run from the main working directory
git worktree remove ../madflow-worktree/local-002
```

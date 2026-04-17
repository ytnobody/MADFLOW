# Claude Opus 4.7 Preset Specification

## Overview

Add `claude-opus` and `claude-api-opus` presets to the `madflow use` command, enabling agents to use Claude Opus 4.7 (`claude-opus-4-7`). Also update the default `madflow.toml` template to use Opus 4.7 for the superintendent agent.

## Motivation

Claude Opus 4.7 is the latest Opus model with 1M context support, making it suitable for complex, high-risk tasks handled by the superintendent agent.

## New Presets

### `claude-opus`

Uses Claude Code CLI routing (`claude` command).

| Agent          | Model             |
|----------------|-------------------|
| superintendent | `claude-opus-4-7` |
| engineer       | `claude-sonnet-4-6` |

### `claude-api-opus`

Uses Anthropic API direct routing (`anthropic/` prefix).

| Agent          | Model                        |
|----------------|------------------------------|
| superintendent | `anthropic/claude-opus-4-7`  |
| engineer       | `anthropic/claude-sonnet-4-6` |

## Default Configuration Update

- `madflow.toml` (project root): superintendent updated to `claude-opus-4-7`
- `cmd/madflow/main.go` init template: superintendent updated to `claude-opus-4-7`
- `internal/config/config.go` fallback default: unchanged (remains `claude-sonnet-4-6` for cost reasons)

## Routing Logic

- `claude-opus-4-7` is handled by the default Claude Code CLI path in `internal/agent/agent.go` — no routing changes needed.
- `anthropic/claude-opus-4-7` is handled by the `strings.HasPrefix(cfg.Model, "anthropic/")` branch — no routing changes needed.

## Out of Scope

- `internal/lessons/lessons.go`: `lessonsMgmtModel` remains `claude-haiku-4-5`
- `agent.context_reset_minutes`: unchanged, to be revisited in a separate issue after observing behavior with 1M context

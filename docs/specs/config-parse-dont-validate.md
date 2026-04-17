# Config: Parse, Don't Validate

## Overview

The `internal/config` package is refactored to clearly separate the pure parsing/validation stage from I/O side effects.

## Problem (Before)

`Load()` mixes pure operations and I/O in a single sequence:

```
setDefaults → applyGhLogin (exec.Command) → warnDefaults → autoPopulate → validate
```

Issues:
- I/O (`gh` command execution) happens in the middle of parsing
- Invalid values pass through intermediate stages before `validate()` is called at the end
- Testing requires mocking filesystem and the `gh` CLI

## Solution (After)

Separate the process into two distinct stages:

### Stage 1: Pure Parsing (no I/O)

`ParseConfig(data []byte) (*Config, error)` performs:
1. TOML deserialization into `RawConfig`
2. Default application (pure function)
3. Structural validation (pure function)

Returns a `*Config` with `GhLogin` and `AuthorizedUsers` left empty (not yet resolved).

### Stage 2: I/O Effects

`applyEffects(*Config)` (called internally by `Load`) performs:
1. `applyGhLogin` — executes `gh api user` to resolve the authenticated user
2. `warnDefaults` — emits log warnings for suspicious configuration
3. `autoPopulateAuthorizedUsers` — populates `AuthorizedUsers` using the resolved login

### Public API

```go
// RawConfig holds config fields exactly as parsed from TOML, without defaults or validation.
type RawConfig struct { ... }

// ParseConfig parses TOML bytes into a validated Config.
// This is a pure function: it performs no I/O and no file access.
// The returned Config has all defaults applied and passes structural validation.
// Runtime fields such as GhLogin and AuthorizedUsers (auto-detected) are left empty;
// call Load() to populate those.
func ParseConfig(data []byte) (*Config, error)

// Load reads the config file at path, calls ParseConfig, then applies I/O side effects.
func Load(path string) (*Config, error)
```

## Type Definitions

### RawConfig

```go
type RawConfig struct {
    Project         ProjectConfig `toml:"project"`
    Agent           AgentConfig   `toml:"agent"`
    Branches        BranchConfig  `toml:"branches"`
    GitHub          *GitHubConfig `toml:"github,omitempty"`
    PromptsDir      string        `toml:"prompts_dir,omitempty"`
    AuthorizedUsers []string      `toml:"authorized_users,omitempty"`
}
```

`RawConfig` represents the TOML document as-is, with no defaults applied. Fields may be zero values.

### Config (ValidConfig)

`Config` is the validated, fully-normalized config. After `ParseConfig` returns, all defaults are applied and structural invariants hold. The `GhLogin` runtime field is populated by `Load` via the I/O stage.

## Behavior

### ParseConfig

- Returns an error if TOML is malformed
- Returns an error if required fields are missing (`project.name`, `project.repos`)
- Returns an error if repo entries have empty `name` or `path`
- Applies all defaults (context_reset_minutes, models, max_teams, etc.)
- Does NOT call `gh`, read files, or produce log output

### Load

- Reads the file at `path`; returns error on read failure
- Calls `ParseConfig` for pure parsing/validation
- Applies I/O effects in order: `applyGhLogin` → `warnDefaults` → `autoPopulateAuthorizedUsers`

## Test Strategy

- `TestParseConfig_*` tests exercise pure parsing without any filesystem or `gh` dependency
- Existing `TestLoad_*` tests continue to verify the full pipeline (they tolerate `gh` being unavailable)
- Edge cases: missing required fields, malformed TOML, GitHub config without authorized_users

## Backward Compatibility

- `Load(path string) (*Config, error)` signature is unchanged
- `Config` struct is unchanged
- Callers that use `Load` are unaffected
- `ParseConfig` is a new addition to the public API

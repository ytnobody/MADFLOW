# Agent Backend Sum Type

## Overview

The `internal/agent` package is refactored to replace scattered string-prefix matching for backend selection with a `Backend` sum type and a single `ParseModel` function that converts model strings to typed backends.

## Problem (Before)

`NewAgent` in `agent.go` selects the backend using string prefix matching in a switch statement:

```go
switch {
case cfg.Model == "test":
    proc = &noopProcess{}
case strings.HasPrefix(cfg.Model, "gemini-"):
    proc = NewGeminiAPIProcess(...)
case strings.HasPrefix(cfg.Model, "anthropic/"):
    proc = NewAnthropicAPIProcess(...)
case strings.HasPrefix(cfg.Model, "copilot/"):
    proc = NewCopilotCLIProcess(...)
default:
    proc = NewClaudeStreamProcess(...)
}
```

Issues:
- String prefix logic is duplicated whenever a new backend needs to be detected
- New backends require changes across multiple call sites
- The relationship between model string and backend is implicit

## Solution (After)

### Types

```go
// Backend represents which AI backend an agent uses.
type Backend int

const (
    BackendClaudeCLI    Backend = iota // Default: Claude CLI (claude command)
    BackendAnthropicAPI                 // anthropic/ prefix → Anthropic API
    BackendGeminiAPI                    // gemini- prefix → Gemini API
    BackendCopilotCLI                   // copilot/ prefix → GitHub Copilot CLI
    BackendTest                         // "test" → no-op process for testing
)

// ModelID is the model identifier string used when calling the backend.
type ModelID string
```

### ParseModel Function

```go
// ParseModel parses a model string into a Backend and ModelID.
// This is the single point where model strings are converted to typed backends.
// Returns an error if the model string is invalid or empty.
func ParseModel(model string) (Backend, ModelID, error)
```

Parsing rules:
- `"test"` → `(BackendTest, "test", nil)`
- `strings.HasPrefix(model, "gemini-")` → `(BackendGeminiAPI, ModelID(model), nil)`
- `strings.HasPrefix(model, "anthropic/")` → `(BackendAnthropicAPI, ModelID(model), nil)`
- `strings.HasPrefix(model, "copilot/")` → `(BackendCopilotCLI, ModelID(model), nil)`
- `model == ""` → error: model string is required
- anything else → `(BackendClaudeCLI, ModelID(model), nil)`

### NewAgent Simplified

`NewAgent` calls `ParseModel` to select the backend, then delegates process creation to an internal `newProcessForBackend` helper:

```go
func NewAgent(cfg AgentConfig) *Agent {
    var proc Process
    if cfg.Process != nil {
        proc = cfg.Process
    } else {
        backend, modelID, err := ParseModel(cfg.Model)
        if err != nil {
            log.Printf("[agent] model parse error: %v, defaulting to ClaudeCLI", err)
            backend, modelID = BackendClaudeCLI, ModelID(cfg.Model)
        }
        proc = newProcessForBackend(backend, modelID, cfg)
    }
    ...
}
```

## File Organization

- New file: `internal/agent/backend.go` — `Backend` type, constants, `ParseModel`, `newProcessForBackend`
- Modified: `internal/agent/agent.go` — `NewAgent` simplified to use `ParseModel`
- New test file: `internal/agent/backend_test.go` — tests for `ParseModel`

## Test Strategy

`TestParseModel` with table-driven tests covering:
- Known prefixes: `gemini-2.5-pro`, `anthropic/claude-opus-4-7`, `copilot/gpt-4o`
- Claude CLI default: `claude-sonnet-4-6`, `claude-haiku-4-5`
- Test special case: `"test"`
- Empty string error case

## Backward Compatibility

- `NewAgent(cfg AgentConfig) *Agent` signature is unchanged
- `AgentConfig.Model` field is unchanged (still a string)
- `Backend` and `ParseModel` are new additions to the public API
- All existing tests pass without modification

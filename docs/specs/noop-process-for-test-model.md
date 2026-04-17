# Spec: noopProcess for model="test"

## Overview

When `model="test"` is configured in the agent, `NewAgent()` should use a
no-op `Process` implementation instead of trying to start a real Claude CLI
subprocess.

## Background

`NewAgent()` creates a `Process` based on the configured model string.
Previously, only two paths existed:
- Model prefixed with `"gemini-"` → `GeminiAPIProcess`
- Anything else → `ClaudeStreamProcess`

When integration tests configure `model="test"` (via `config.AgentConfig{Models: {Engineer: "test"}}`),
the agent falls through to `ClaudeStreamProcess`, which tries to invoke the
actual `claude` binary.  The `Send()` call blocks waiting for the CLI
response, preventing `markReady()` from being called.  This causes
`team.Manager.Create()` to hang at the `engineer.Ready()` wait, and
integration tests that poll for team/issue assignment time out.

## Design

Add a `"test"` model case to the switch in `NewAgent()`:

```go
case cfg.Model == "test":
    proc = &noopProcess{}
```

`noopProcess` is a minimal `Process` implementation that:
- `Send()` returns `("", nil)` immediately (no subprocess, no network call)
- `Reset()` returns `nil`
- `Close()` returns `nil`

This allows `agent.Run()` to call `sendWithRetry()` → `markReady()` without
any external I/O, so `engineer.Ready()` is signaled promptly in tests.

## Behaviour

| Config | Process used | Behaviour |
|--------|-------------|-----------|
| `model="test"` | `noopProcess` | `Send()` returns immediately |
| `model="gemini-*"` | `GeminiAPIProcess` | Real Gemini API call |
| anything else | `ClaudeStreamProcess` | Real Claude CLI invocation |

## Edge Cases

- `noopProcess` must not be used in production; it is only selected when
  `cfg.Model == "test"`.
- If a `Process` is supplied via `AgentConfig.Process`, it takes precedence
  over the model-based selection (existing behaviour, unchanged).

## Tests

`TestIssueToTeamCreateFlow` in `internal/integration` uses `model="test"` and
verifies that:
1. Team creation completes without timeout.
2. The issue's `AssignedTeam` field is updated within 5 seconds.
3. `waitForAssignment` returns the updated issue with `status=in_progress`.

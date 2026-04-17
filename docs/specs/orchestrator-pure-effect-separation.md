# Orchestrator Pure/Effect Separation

## Overview

Refactor `internal/orchestrator` to separate pure domain logic from effectful I/O and LLM calls.
The goal is to improve testability by making decision logic independently testable without needing
to mock the entire orchestrator infrastructure.

## Command Sum Type

### Problem

`handleCommand` uses `strings.HasPrefix` to dispatch on command strings:

```go
switch {
case strings.HasPrefix(body, "TEAM_CREATE"):
    o.handleTeamCreate(ctx, body)
case strings.HasPrefix(body, "TEAM_DISBAND"):
    ...
}
```

This is error-prone (prefix matching can be ambiguous) and hard to test without a full orchestrator.

### Solution

Introduce `type CommandType int` with `iota` constants and `ParseCommand(body string) Command`
to centralize command parsing.

```go
type CommandType int

const (
    CommandTeamCreate   CommandType = iota
    CommandTeamDisband
    CommandRelease
    CommandWakeGitHub
    CommandPatrolComplete
    CommandUnknown
)

type Command struct {
    Type CommandType
    Args []string
}

func ParseCommand(body string) Command
```

**Invariants**:
- `ParseCommand` is a pure function with no side effects
- If the body is empty or unrecognized, `Command{Type: CommandUnknown}` is returned
- `Args` contains all whitespace-separated tokens after the command keyword
- `CommandType.String()` returns a human-readable name for logging

## Team Assignment Decision

### Problem

`handleTeamCreate` (150+ lines) mixes issue validation, decision logic, and effects
(store updates, LLM team creation, chatlog writes) in one function. The rejection/assignment
decision logic cannot be tested without a running orchestrator.

### Solution

Extract the pure decision logic into `DecideTeamAssignment`:

```go
type TeamAssignDecisionType int

const (
    AssignDecisionReject    TeamAssignDecisionType = iota // issue ineligible
    AssignDecisionReuseIdle                               // assign to existing idle team
    AssignDecisionCreate                                  // create new team
    AssignDecisionDefer                                   // at capacity, retry later
)

type TeamAssignResult struct {
    Decision TeamAssignDecisionType
    Reason   string // human-readable explanation
}

func DecideTeamAssignment(iss issue.Issue, hasActiveTeam bool, hasIdleTeam bool, atCapacity bool) TeamAssignResult
```

**Decision matrix**:

| Condition                             | Decision        |
|---------------------------------------|-----------------|
| issue status is terminal              | Reject          |
| issue already has AssignedTeam > 0   | Reject          |
| active/pending team exists for issue  | Reject          |
| idle team available                   | ReuseIdle       |
| at max_teams capacity                 | Defer           |
| otherwise                             | Create          |

**Invariants**:
- `DecideTeamAssignment` is a pure function with no side effects
- The orchestrator is responsible for executing effects based on the returned decision
- Rejection reasons are human-readable for chatlog messages

## Impact

- `internal/orchestrator/command.go` (new file)
- `internal/orchestrator/command_test.go` (new file)
- `internal/orchestrator/decision.go` (new file)
- `internal/orchestrator/decision_test.go` (new file)
- `internal/orchestrator/orchestrator.go` (simplify handleCommand, handleTeamCreate)

## Acceptance Criteria

- `ParseCommand` correctly parses all recognized command strings
- `DecideTeamAssignment` returns correct decisions for all input combinations
- `handleCommand` uses `ParseCommand` instead of `strings.HasPrefix` switches
- `handleTeamCreate` uses `DecideTeamAssignment` for decision logic
- All existing tests pass
- `orchestrator.go` line count decreases from 1468

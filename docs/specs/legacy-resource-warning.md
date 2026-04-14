# Legacy Resource Warning Specification

## Overview

When `madflow start` is executed, the system detects old-format branches and worktrees
and displays a warning message. No automatic migration is performed.

## Background

Issue #235 changed the branch naming convention and worktree path format to include the
GitHub login name for namespace separation:

| Resource  | Old Format (Legacy)                          | New Format                                       |
|-----------|----------------------------------------------|--------------------------------------------------|
| Branch    | `feature/issue-{number}`                     | `madflow/{gh_login}/issue-{number}`              |
| Worktree  | `.madflow/worktrees/issue-{number}/`         | `.madflow/worktrees/{gh_login}/issue-{number}/`  |

## Detection Targets

### Legacy Worktrees

Directories under `.madflow/worktrees/` whose name matches the pattern `issue-{number}`
(i.e., a directory named `issue-` followed by one or more digits), and does **not** contain
a `/` separator (which would indicate the new `{gh_login}/issue-{number}` format).

Pattern: `issue-\d+`

### Legacy Branches

Local git branches whose name matches the pattern `feature/issue-{number}`
(i.e., `feature/issue-` followed by one or more digits).

Pattern: `feature/issue-\d+`

## Warning Format

### Legacy Worktree Warning

```
[WARN] Legacy worktree detected: .madflow/worktrees/issue-42/
       Please migrate manually or remove before continuing.
```

### Legacy Branch Warning

```
[WARN] Legacy branch detected: feature/issue-42
       Please migrate manually or remove before continuing.
```

## Behavior

- Warnings are printed to stderr during the startup process (`cmdStart`).
- The startup process **continues** after warnings are displayed (non-fatal).
- Warnings appear **before** the orchestrator starts.
- If no legacy resources are present, no warning is displayed.
- Each detected resource produces its own warning line.

## Implementation

### `internal/git/git.go`

Two new exported functions on `*Repo`:

- `DetectLegacyWorktrees(madflowDir string) []string`
  - Scans `<madflowDir>/worktrees/` for directories matching `issue-\d+`
  - Returns a slice of relative paths like `.madflow/worktrees/issue-42/`

- `DetectLegacyBranches() []string`
  - Lists local branches matching `feature/issue-\d+`
  - Returns a slice of branch names

### `cmd/madflow/main.go`

- `warnLegacyResources(repos []*git.Repo, madflowDirs []string)` — detects and prints warnings
- Called from `cmdStart()` before calling `orc.Run(ctx)`

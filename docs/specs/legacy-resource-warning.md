# Legacy Resource Cleanup Specification

## Overview

When `madflow start` is executed, the system detects old-format branches and worktrees
and handles them automatically:

- **Legacy branches** (`feature/issue-{number}`) are **automatically deleted** with a log message.
- **Legacy worktrees** (`.madflow/worktrees/issue-{number}/`) still emit a warning message
  asking for manual removal.

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

## Log Format

### Legacy Worktree Warning (unchanged)

```
[WARN] Legacy worktree detected: .madflow/worktrees/issue-42/
       Please migrate manually or remove before continuing.
```

### Legacy Branch Deletion Log

On successful deletion:
```
[INFO] Legacy branch deleted: feature/issue-42
```

On failure to delete:
```
[WARN] Failed to delete legacy branch feature/issue-42: <error>
```

## Behavior

- Startup continues regardless of branch deletion results (non-fatal).
- Deletion and any log messages appear **before** the orchestrator starts.
- If no legacy resources are present, no output is produced.
- Each resource produces its own log line.
- Legacy branches are deleted with `git branch -d` (safe delete, merged-only) to avoid removing unmerged work.

## Implementation

### `internal/git/legacy.go`

New exported method on `*Repo`:

- `DeleteLegacyBranches() (deleted []string, err error)`
  - Calls `DetectLegacyBranches()` to get the list of legacy branches
  - Deletes each branch with `git branch -d` (safe delete, merged-only)
  - Returns the names of successfully deleted branches and the first error encountered
  - Continues attempting to delete remaining branches even if one fails

### `cmd/madflow/main.go`

- `cleanupLegacyResources(repos []config.RepoConfig)` replaces `warnLegacyResources`
  - For each repo, calls `repo.DeleteLegacyBranches()` and prints `[INFO]` / `[WARN]` lines
  - For each repo, still calls `repo.DetectLegacyWorktrees()` and prints `[WARN]` lines
- Called from `cmdStart()` before calling `orc.Run(ctx)`

# Merged Worktree Auto-Cleanup

## Overview

Automatically clean up worktrees, local branches, and remote branches when their
associated GitHub PRs are in `merged` or `closed` state. This prevents stale
worktrees from accumulating on disk.

## Scope

Issue: ytnobody-MADFLOW-237
Requirement: §3.6

## Design

### Configuration (`internal/config/config.go`)

A new field `MergedWorktreeCleanupIntervalMinutes` is added to `AgentConfig`.

- Type: `int`
- TOML key: `merged_worktree_cleanup_interval_minutes`
- Default: `0` (disabled)
- Positive value: enables periodic cleanup at the specified interval

### Cleanup Logic (`internal/git/worktree_cleanup.go`)

#### `NamespacedWorktreeEntry`

Represents a worktree under `.worktrees/{ghLogin}/{subDir}`.

Fields:
- `SubDir string` — the sub-directory name (e.g. `issue-myorg-REPO-42`)
- `Path string` — full filesystem path to the worktree
- `BranchName string` — inferred git branch name (`madflow/{ghLogin}/{subDir}`)

#### `Repo.ListNamespacedWorktrees(ghLogin string) ([]NamespacedWorktreeEntry, error)`

- Lists all directories under `.worktrees/{ghLogin}/`
- Returns empty slice (no error) if the namespace directory does not exist
- Validates `ghLogin` using `ValidateSafeName`

#### `CheckPRState(owner, repo, branchName string) (string, error)`

- Runs `gh pr list --head {branchName} --state all --json state --repo {owner}/{repo}`
- Parses JSON array of `{ "state": "..." }` objects
- Returns the state of the first PR (`"merged"`, `"closed"`, `"OPEN"` → normalised to lower-case)
- Returns `""` (empty string, nil error) when no PR exists
- Returns error on `gh` CLI failures or JSON parse failures

#### `Repo.CleanMergedPRWorktrees(owner, repo, ghLogin string) ([]string, error)`

1. Runs `git worktree prune` to recover consistency from manually-deleted worktrees
2. Calls `ListNamespacedWorktrees(ghLogin)` to enumerate worktrees
3. For each entry:
   a. Calls `CheckPRState(owner, repo, entry.BranchName)` — skips on error (logged)
   b. If state is `""` (no PR): skips (work in progress)
   c. If state is not `merged` or `closed`: skips
   d. Removes worktree: `git worktree remove --force {path}`
   e. Deletes local branch (if exists): `git branch -D {branchName}`
   f. Deletes remote branch (best-effort): `git push origin --delete {branchName}` — failure is logged, not returned as error
4. Returns list of removed `SubDir` values

### Orchestrator Integration (`internal/orchestrator/orchestrator.go`)

#### `runMergedWorktreeCleanup(ctx context.Context)`

- Goroutine started in `Run()` when `cfg.Agent.MergedWorktreeCleanupIntervalMinutes > 0`
- Runs asynchronously to avoid blocking the main polling loop
- Each tick calls `repo.CleanMergedPRWorktrees(owner, repo, ghLogin)` for each repo
- Remote branch deletion failures are tolerated; cleanup is re-attempted next interval

## Behaviour

### Deletion Trigger

A worktree is deleted when:
- Its namespace directory (`.worktrees/{ghLogin}/{subDir}`) exists, AND
- `gh pr list --head madflow/{ghLogin}/{subDir} --state all` returns at least one PR, AND
- The most recent PR's state is `merged` or `closed`

### Skip Conditions

A worktree is NOT deleted when:
- No matching PR exists (the engineer is still working)
- The PR is still `open`
- The `gh` CLI call fails (logged, retried next interval)

### Non-Functional Requirements

- Cleanup goroutine is non-blocking (runs in a separate goroutine)
- Remote branch deletion failure does not abort cleanup of other worktrees
- `--force` flag ensures uncommitted changes do not prevent worktree removal
- Manual worktree deletion is handled by `git worktree prune`

## File Changes

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `MergedWorktreeCleanupIntervalMinutes int` to `AgentConfig` |
| `internal/git/worktree_cleanup.go` | New file: cleanup logic |
| `internal/git/worktree_cleanup_test.go` | New file: tests for cleanup logic |
| `internal/orchestrator/orchestrator.go` | Start `runMergedWorktreeCleanup` goroutine |
| `internal/orchestrator/orchestrator_test.go` | Tests for orchestrator integration |

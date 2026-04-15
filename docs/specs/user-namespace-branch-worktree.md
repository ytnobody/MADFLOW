# Spec: User-Namespaced Branch Names and Worktree Paths

## Overview

Branch names and worktree paths now include the GitHub login name, preventing resource
collisions when multiple users run MADFLOW against the same repository.

This implements §3.3 and §3.4 of the multi-user support requirements document.
It builds on §3.1/§3.2 (GitHub account auto-detection, issue #234).

## Background

Previously, all MADFLOW users shared the same branch namespace (`feature/issue-*`) and
worktree directory layout (`.worktrees/team-N/`). When two users worked on the same
repository, their branches and worktree directories would collide.

The fix is to prefix both branch names and worktree paths with the authenticated
GitHub login, creating a per-user namespace.

## Behavior

### Branch Naming (§3.3)

| | Format |
|---|---|
| Before | `feature/issue-{issueID}` |
| After  | `madflow/{gh_login}/issue-{issueID}` |

The `FeaturePrefix` field in `BranchConfig` defaults to `madflow/{gh_login}/issue-` when
the GitHub CLI is authenticated. If `gh api user` fails or returns no login, the prefix
falls back to `feature/issue-` for backward compatibility.

If `feature_prefix` is explicitly set in `madflow.toml`, that value is used as-is
(no auto-namespacing). This allows per-project override.

### Worktree Path (§3.4)

| | Path |
|---|---|
| Before | `{REPO_PATH}/.worktrees/team-{teamNum}/` |
| After  | `{REPO_PATH}/.worktrees/{gh_login}/issue-{issueID}/` |

Engineers are instructed in their system prompt to create worktrees at the new path.
The `{{GH_LOGIN}}` template variable is substituted from `cfg.GhLogin` when the agent
is created.

### GhLogin Field

A new `GhLogin string` field is added to `Config`. It is a runtime-only field (not a TOML
key) populated by `resolveGitHubLogin()` at load time. It represents the authenticated
GitHub CLI user and is used for:
1. Setting the `FeaturePrefix` default
2. Substituting `{{GH_LOGIN}}` in agent prompts

### Branch Name Validation

A new `ValidateSafeBranchName(name string) error` function is added to `internal/git`.
Unlike `ValidateSafeName`, it allows `/` as a namespace separator. Each component separated
by `/` is validated individually (no `..`, no backslash, no null byte, no empty component).

`PrepareWorktree` is updated to use `ValidateSafeBranchName` instead of `ValidateSafeName`.

### Worktree Cleanup

The orchestrator's worktree cleanup logic is updated to handle both legacy (`team-N`) and
new-style (`{gh_login}/issue-{issueID}`) worktree paths.

- **Startup cleanup** (`cleanStaleWorktrees`): cleans both `team-*` and `{gh_login}/` dirs
- **Team disband cleanup** (`cleanTeamWorktrees`): removes `{gh_login}/issue-{issueID}/` for new-style
- **Periodic cleanup** (`runWorktreeCleanup`): tracks active paths as `{gh_login}/issue-{issueID}`

`CleanOrphanedWorktrees` gains a `ghLogin string` parameter. When non-empty, it also
scans the `{ghLogin}/` namespace directory for orphaned worktrees.

## Affected Files

- `internal/config/config.go` — add `GhLogin` field; set `FeaturePrefix` after login detection
- `internal/agent/prompt.go` — add `GhLogin` to `PromptVars`; add `{{GH_LOGIN}}` substitution
- `internal/orchestrator/orchestrator.go` — pass `GhLogin` to prompts; update cleanup logic
- `internal/git/git.go` — add `ValidateSafeBranchName`; update `CleanOrphanedWorktrees`
- `prompts/engineer.md` — update worktree path and branch name templates
- `internal/config/config_test.go` — update FeaturePrefix default test
- `internal/git/git_test.go` — add `ValidateSafeBranchName` tests; update cleanup tests

## Backward Compatibility

- Existing configs with explicit `feature_prefix` continue to work unchanged.
- When `gh` CLI is unavailable, the prefix falls back to `feature/issue-`.
- Old-style `team-N` worktrees are still cleaned up by startup and orphan cleanup.

## Examples

### Branch Names

With `gh_login = "alice"` and `issueID = "myorg-REPO-42"`:
- Feature prefix: `madflow/alice/issue-`
- Branch name: `madflow/alice/issue-myorg-REPO-42`

### Worktree Paths

With `gh_login = "alice"` and `issueID = "myorg-REPO-42"`:
- Old: `/path/to/repo/.worktrees/team-1/`
- New: `/path/to/repo/.worktrees/alice/issue-myorg-REPO-42/`

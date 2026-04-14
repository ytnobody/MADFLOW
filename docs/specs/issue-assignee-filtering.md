# Issue Assignee Filtering Specification

## Overview

When GitHub Issues are synchronized, the Syncer filters issues based on the assignee field. Only issues assigned to the authenticated GitHub user (`gh_login`) are processed.

## Behavior

### Assignee-based filtering (§3.5)

During `syncRepo`, each fetched GitHub Issue is evaluated against the following rules:

1. **No assignees**: The issue has no assignees.
   - Action: Automatically assign `gh_login` using `gh issue edit {number} -R {owner}/{repo} --add-assignee {gh_login}`.
   - Outcome: Issue is processed (imported or updated).

2. **Assignee matches `gh_login`**: The issue is assigned to the authenticated user.
   - Outcome: Issue is processed normally.

3. **Assignee is another user**: The issue is assigned to a user other than `gh_login`.
   - Outcome: Issue is **skipped** (not imported or updated).
   - A log message is emitted: `[github-sync] skipping issue {id}: assigned to other users`.

### Fallback behavior

If `gh_login` is empty (e.g., GitHub integration is not configured, or auto-detection failed), assignee filtering is **disabled** and all issues are processed as before.

## API

### `Syncer.WithGhLogin(login string) *Syncer`

Sets the GitHub login for assignee-based filtering. When set to a non-empty value, the Syncer applies assignee filtering as described above.

### `Syncer.addAssignee(ctx context.Context, repo string, number int, login string) error`

Calls `gh issue edit {number} -R {owner}/{repo} --add-assignee {login}` to assign the given login to the issue.

## Fields

### `ghIssue.Assignees`

The `ghIssue` struct now includes an `Assignees` field populated from `gh issue list --json assignees`. Each assignee has a `Login` string field.

## Configuration

The `gh_login` value is derived from `cfg.AuthorizedUsers[0]` when GitHub integration is enabled. This value is auto-detected at startup via `gh api user --jq '.login'` (implemented in issue #234).

## Related

- §3.5 of the requirements specification
- Issue #234: GitHub account auto-detection (`GhLogin` prerequisite)

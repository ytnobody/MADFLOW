# Path Traversal Protection for Branch Names and Issue IDs

## Overview

When `filepath.Join()` is used to compose worktree directory paths from external input (branch names, issue IDs), a crafted value containing `..` or path separators could escape the intended `.worktrees/` directory. This spec defines the validation rules and implementation details for protecting against such path traversal attacks.

## Background

- **Affected code**: `internal/git/git.go` — `PrepareWorktree`, `AddWorktree`, `CleanWorktrees`, `CleanOrphanedWorktrees`
- **Input source**: Branch names and issue IDs are derived from GitHub issues (external input)
- **Risk**: A crafted issue ID such as `../../sensitive` could cause worktree operations to target paths outside `.worktrees/`
- **Reference**: `SECURITY_AUDIT_REPORT.md` section 3.3

## Validation Rules

A safe name (branch name component or issue ID) must satisfy all of the following:

1. **Non-empty**: Empty strings are rejected.
2. **No `..` sequences**: Strings containing `..` are rejected to prevent directory traversal.
3. **No path separators**: Strings containing `/` or `\` are rejected.
4. **No null bytes**: Strings containing `\x00` are rejected.

## Implementation

### Function: `ValidateSafeName(name string) error`

Located in `internal/git/git.go`. Returns a non-nil error if `name` violates any validation rule.

### Integration Points

`PrepareWorktree` validates its `featureBranch` argument using `ValidateSafeName` before proceeding.

Note: `CleanWorktrees` and `CleanOrphanedWorktrees` read directory names via `os.ReadDir`, which cannot return `..` entries; these functions are safe without additional validation. The validation at `PrepareWorktree` covers the write path where external input is used.

## Test Coverage

Tests in `internal/git/git_test.go` cover:
- Valid names that should pass (alphanumeric, hyphens, dots in non-traversal positions)
- Strings with `..` (rejected)
- Strings with `/` or `\` (rejected)
- Empty string (rejected)
- Null byte (rejected)
- Integration: `PrepareWorktree` rejects a crafted traversal branch name

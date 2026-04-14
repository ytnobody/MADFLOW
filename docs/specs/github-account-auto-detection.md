# Spec: GitHub Account Auto-Detection and authorized_users Deprecation

## Overview

MADFLOW now auto-detects the GitHub account at startup using `gh api user --jq '.login'`,
and the `authorized_users` configuration field is deprecated (no longer required).

This implements §3.1 and §3.2 of the requirements document for multi-user support.

## Background

Previously, users had to manually specify `authorized_users` in `madflow.toml` whenever
GitHub integration (`[github]` section) was enabled. This was error-prone and inconvenient.
The new behavior auto-populates `AuthorizedUsers` from the authenticated GitHub CLI user at startup.

## Behavior

### Startup Auto-Detection

When `config.Load()` is called and GitHub integration is enabled (`cfg.GitHub != nil`):

1. If `authorized_users` is explicitly set in the config file, use that value as-is (backward compatibility).
2. If `authorized_users` is **not** set (or empty), call `gh api user --jq '.login'` to get the
   currently authenticated GitHub CLI user and set `AuthorizedUsers = [<login>]`.
3. If the `gh` CLI call fails (not logged in, not installed), log a warning but proceed.
   In this case `AuthorizedUsers` remains empty, which causes `isAuthorized()` to deny all users.

### Config Validation Change

The validation that previously required `authorized_users` when `[github]` is configured is
**removed**. Missing `authorized_users` is now a valid configuration — MADFLOW will attempt
auto-detection at load time.

### `authorized_users` Field Deprecation

The `authorized_users` TOML field remains supported for backward compatibility but is no
longer required. New configurations should omit it; MADFLOW will auto-detect the GitHub login.

### `madflow init` Changes

`madflow init` no longer writes `authorized_users` to the generated `madflow.toml`.
The `--authorized-users` CLI flag is removed. Auto-detection happens at runtime via `config.Load()`.

## Affected Files

- `internal/config/config.go` — remove validation requirement; add `resolveGitHubLogin()` and auto-populate logic
- `internal/config/config_test.go` — update tests for new validation behavior
- `cmd/madflow/main.go` — remove `--authorized-users` flag and related helpers from `cmdInit`
- `cmd/madflow/main_test.go` — update tests for new `cmdInit` behavior
- `docs/specs/authorized-users-required.md` — mark as superseded
- `docs/specs/init-authorized-users.md` — mark as superseded

## Security Context

The auto-detected user is the GitHub account that `gh` CLI is authenticated with. This is
typically the repository owner, which preserves the security intent of `authorized_users`.

If the `gh` CLI is not available or not authenticated, `AuthorizedUsers` remains empty and
`isAuthorized()` will deny all users (the same behavior as an empty `authorized_users = []`
in the old config). Users can still explicitly set `authorized_users` to override.

## Backward Compatibility

- Existing configs with `authorized_users` set will continue to work unchanged.
- Existing configs without `authorized_users` but with `[github]` will no longer fail to load;
  instead, the login will be auto-detected.

## Examples

### No `authorized_users` in config (new behavior)

```toml
[project]
name = "myproject"

[[project.repos]]
name = "myrepo"
path = "/path/to/repo"

[github]
owner = "myorg"
repos = ["myrepo"]
```

At startup, MADFLOW calls `gh api user --jq '.login'` and sets `AuthorizedUsers = ["myusername"]`.

### Explicit `authorized_users` (backward compatible)

```toml
authorized_users = ["alice", "bob"]

[project]
name = "myproject"
...
```

`AuthorizedUsers = ["alice", "bob"]` — auto-detection is skipped.

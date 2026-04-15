# Spec: authorized_users Required Configuration

> **SUPERSEDED**: This spec is superseded by [github-account-auto-detection.md](./github-account-auto-detection.md).
> `authorized_users` is no longer required; MADFLOW auto-detects the GitHub login at startup.

## Overview

The `authorized_users` configuration field is **mandatory** when GitHub integration (`[github]` section) is enabled. When not set (or set to an empty list), MADFLOW must refuse to start and print a clear error message.

This prevents a critical security vulnerability where any GitHub user could submit issues to a public repository and have MADFLOW process them, potentially enabling prompt injection attacks that lead to arbitrary command execution.

## Behavior

### Config Validation

When `config.Load()` is called:

1. If `cfg.GitHub != nil` (GitHub integration is enabled) **and** `len(cfg.AuthorizedUsers) == 0` (no authorized users are configured), the function must return an error:
   ```
   validate config: authorized_users is required when github integration is enabled; set authorized_users to a list of GitHub logins allowed to interact with MADFLOW
   ```

2. If `cfg.GitHub == nil` (no GitHub integration), the `authorized_users` field is not required and may remain empty.

3. If `authorized_users` contains at least one entry, validation passes normally.

### `isAuthorized()` Behavior

The `isAuthorized(login string, authorizedUsers []string) bool` function in `internal/github/github.go`:

- **Before** (insecure): When `authorizedUsers` is empty, returns `true` (permit all).
- **After** (secure): When `authorizedUsers` is empty, returns `false` (deny all).

This change is defense-in-depth; the config validation above should prevent the empty-list case from ever reaching `isAuthorized` in production.

| `authorizedUsers` | `login`       | Result  |
|-------------------|---------------|---------|
| empty / nil       | (any)         | `false` |
| `["alice"]`       | `"alice"`     | `true`  |
| `["alice"]`       | `"bob"`       | `false` |
| `["alice", "bob"]`| `"bob"`       | `true`  |
| `["alice", "bob"]`| `"charlie"`   | `false` |

## Configuration Example

```toml
# Required when [github] section is present
authorized_users = ["myusername"]

[github]
owner = "myorg"
repos = ["myrepo"]
```

## Security Context

This change addresses the root attack vector for the following risks documented in `SECURITY_AUDIT_REPORT.md`:

- **MEDIUM** (direct fix): Default all-user trust (`github.go:155-165`)
- **CRITICAL** (attack path blocked): LLM response arbitrary command execution (`gemini_api.go:405`)
- **CRITICAL** (attack path blocked): Claude CLI `--dangerously-skip-permissions` compound risk (`claude.go:93`)
- **HIGH** (attack path blocked): Prompt injection via issue body (`prompt.go:70-74`)

By requiring explicit opt-in for trusted users, MADFLOW will not process issues from unknown users even if the configuration is incomplete.

## Affected Files

- `internal/config/config.go` — validation logic and field comment
- `internal/github/github.go` — `isAuthorized()` default behavior
- `internal/config/config_test.go` — updated/new tests for validation
- `internal/github/github_test.go` — updated test for `isAuthorized()` with empty list

# Spec: madflow init - authorized_users Configuration

> **SUPERSEDED**: This spec is superseded by [github-account-auto-detection.md](./github-account-auto-detection.md).
> `madflow init` no longer generates `authorized_users` in `madflow.toml`. Auto-detection occurs at startup via `config.Load()`.

## Overview

`madflow init` always includes `authorized_users` in the generated `madflow.toml`. The initial value is
auto-detected from the GitHub repository owner. If detection fails, an empty array is used.

## Behavior

### Auto-Detection of GitHub Owner

When `--authorized-users` is not explicitly provided, `madflow init` attempts to determine the
GitHub repository owner automatically using the following steps (in order):

1. **Parse git remote URL**: Run `git -C <repoPath> remote get-url origin` and extract the owner
   from a GitHub HTTPS (`https://github.com/owner/repo`) or SSH (`git@github.com:owner/repo`) URL.
2. **Authenticated GitHub user**: Run `gh api user --jq '.login'` to get the currently
   authenticated GitHub CLI user as a fallback.
3. **Empty array**: If neither method succeeds, `authorized_users = []` is written to the config.

### CLI Flag

`madflow init` accepts an explicit flag that overrides auto-detection:

- `--authorized-users <users>` — Comma-separated list of GitHub usernames (e.g., `--authorized-users alice,bob`).

When `--authorized-users` is provided, auto-detection is skipped entirely.

### Interactive Prompt

If `--authorized-users` is not provided **and** auto-detection fails **and** stdin is a terminal
(interactive session), `madflow init` prompts the user:

```
Enter authorized GitHub usernames (comma-separated, press Enter to skip):
```

- Each username is trimmed of whitespace.
- Empty strings after trimming are ignored.
- If the user presses Enter without input, `authorized_users = []` is written.

### Generated Config

`authorized_users` is **always** written to the generated `madflow.toml`, regardless of whether
users were detected, provided, or defaulted to an empty list.

**With detected/provided users:**

```toml
authorized_users = ["ytnobody"]

[project]
name = "myproject"
...
```

**With no users detected (empty array):**

```toml
authorized_users = []

[project]
name = "myproject"
...
```

## Examples

### Auto-Detection from Git Remote

```bash
cd /path/to/repo  # repo with git remote pointing to github.com/alice/myrepo
madflow init
```

Generated `madflow.toml` includes:
```toml
authorized_users = ["alice"]
```

### Via CLI Flag (overrides auto-detection)

```bash
madflow init --authorized-users alice,bob
```

Generated `madflow.toml` includes:
```toml
authorized_users = ["alice", "bob"]
```

### No Owner Detected (non-interactive, no remote)

```bash
madflow init  # no git remote, gh CLI not authenticated
```

Generated `madflow.toml` includes:
```toml
authorized_users = []
```

## Notes

- `authorized_users` is **required** at runtime when the `[github]` section is present
  (see `authorized-users-required.md`). Always generating the field makes the config
  ready for GitHub integration without manual editing.
- If `madflow.toml` already exists, `cmdInit` does not overwrite it, so this behavior
  only applies when creating a new config file.

## Affected Files

- `cmd/madflow/main.go` — add `detectGitHubOwner`, `extractOwnerFromGitHubURL`, update
  `buildAuthorizedUsersLine` to always emit the field, update `cmdInit` logic
- `cmd/madflow/main_test.go` — update and add tests

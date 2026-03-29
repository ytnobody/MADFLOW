# Spec: madflow init - authorized_users Configuration

## Overview

`madflow init` must prompt users to specify `authorized_users` during project initialization. This ensures that projects using GitHub integration have the required security configuration set from the start.

## Behavior

### CLI Flag

`madflow init` accepts a new flag:

- `--authorized-users <users>` — Comma-separated list of GitHub usernames to authorize (e.g., `--authorized-users alice,bob`).

### Interactive Prompt

If `--authorized-users` is not provided and stdin is a terminal (interactive session), `madflow init` prompts the user:

```
Enter authorized GitHub usernames (comma-separated, press Enter to skip):
```

- The user enters a comma-separated list of GitHub usernames (e.g., `alice, bob`).
- Each username is trimmed of whitespace.
- Empty strings after trimming are ignored.
- If the user presses Enter without input, the `authorized_users` field is omitted from the generated config.

### Non-Interactive Mode

If `--authorized-users` is not provided and stdin is NOT a terminal (piped/scripted input), no prompt is shown and `authorized_users` is omitted from the generated config.

### Generated Config

When `authorized_users` are specified (either via flag or interactive prompt):

```toml
authorized_users = ["alice", "bob"]

[project]
name = "myproject"
...
```

When no users are specified, the `authorized_users` field is omitted from the generated config.

## Examples

### Via CLI Flag

```bash
madflow init --authorized-users alice,bob
```

Generated `madflow.toml` includes:
```toml
authorized_users = ["alice", "bob"]
```

### Via Interactive Prompt

```
$ madflow init
Enter authorized GitHub usernames (comma-separated, press Enter to skip): alice, bob
Project 'myproject' initialized.
```

### Skip (empty input or non-interactive)

```
$ madflow init
Enter authorized GitHub usernames (comma-separated, press Enter to skip): [Enter]
Project 'myproject' initialized.
```

Config does not include `authorized_users`.

## Notes

- `authorized_users` is only **required** at runtime when the `[github]` section is present (see `authorized-users-required.md`). If not using GitHub integration, users may safely skip this.
- If `madflow.toml` already exists, `cmdInit` does not overwrite it, so the `authorized_users` prompt behavior only applies when creating a new config file.

## Affected Files

- `cmd/madflow/main.go` — parse `--authorized-users` flag, add interactive prompt, update config template
- `cmd/madflow/main_test.go` — add tests for new flag and generated config

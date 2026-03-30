# File Permission Restriction Specification

## Overview

MADFLOW stores sensitive information on disk, including GitHub issue content and LLM conversation logs (chatlog). When these files are created with world-readable permissions (`0755` for directories, `0644` for files), other users on the same host can read them, causing a potential information leak.

This spec defines the restrictive permission policy for all data directories and sensitive files.

## Affected Files

### Data Directories

All data directories managed by MADFLOW must be created with permission `0700` (owner read/write/execute only):

| Package | File | Location |
|---------|------|----------|
| `internal/orchestrator` | `orchestrator.go` | `Run()` — sub-directories (issues, memos) |
| `internal/project` | `project.go` | `Add()` — base dir and sub-directories |
| `internal/issue` | `issue.go` | `Store.Create()` — issues dir |
| `internal/reset` | `reset.go` | `SaveMemoWithLang()` — memos dir |

### Sensitive Files

All sensitive files (chatlog, memos) must be created/written with permission `0600` (owner read/write only):

| Package | File | Location |
|---------|------|----------|
| `internal/orchestrator` | `orchestrator.go` | `Run()` — chatlog truncate |
| `internal/chatlog` | `chatlog.go` | `Append()` — chatlog append |
| `internal/chatlog` | `chatlog.go` | `TruncateOldEntries()` — chatlog tmp write |
| `internal/agent` | `agent.go` | `rescueChatLogMessages()` — chatlog append |
| `internal/team` | `team.go` | `appendLine()` — chatlog append |
| `internal/reset` | `reset.go` | `SaveMemoWithLang()` — memo write |

## Permissions

| Resource Type | Old Permission | New Permission | Rationale |
|--------------|---------------|---------------|-----------|
| Data directories | `0755` | `0700` | Prevent other users from listing or accessing directory contents |
| Sensitive files | `0644` | `0600` | Prevent other users from reading chatlog and memo content |

## Edge Cases

- Existing files/directories are not retroactively `chmod`'d — only newly created files/directories use restrictive permissions. Operators should manually fix permissions on existing deployments.
- Test files in `_test.go` files are intentionally excluded from this change as they operate on temporary test directories and do not contain real sensitive data.

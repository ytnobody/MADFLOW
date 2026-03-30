# Log Sanitization Spec

## Overview

Stderr output from subprocesses is logged to assist with diagnostics. However, stderr may contain sensitive information such as API keys, bearer tokens, or other credentials. This spec describes the sanitization applied before any stderr content is logged.

## Patterns to Sanitize

The following patterns are recognized and masked:

| Pattern | Example | Masked Form |
|---------|---------|-------------|
| OpenAI / Anthropic-style secret keys (`sk-...`) | `sk-abc123...` | `sk-***` |
| Bearer tokens in Authorization headers | `Bearer eyJ...` | `Bearer ***` |
| Google / Gemini API keys (`AIza...`) | `AIzaSy...` | `AIza***` |
| `key=<value>` query parameters | `?key=myapikey` | `?key=***` |
| `api_key=<value>` query parameters | `api_key=secret` | `api_key=***` |
| `token=<value>` query/form parameters | `token=abc` | `token=***` |

## How Sanitization Works

A `SanitizeLog(s string) string` function in `internal/agent/sanitize.go` applies a series of regular expression replacements to an input string. Each regex captures the sensitive portion and replaces it with `***`. The function is applied to:

1. `internal/agent/gemini_api.go` `runBash()`: the stderr content appended to the result string (line 414).
2. `internal/agent/claude_stream.go` `Send()`: the stderr content passed to `log.Printf` (line 110).

## Edge Cases

- **Multi-line output**: The regexes are applied to the full string; they will match across lines if the pattern spans a line boundary, but in practice each sensitive value appears on a single line.
- **False positives**: Short words that happen to match (e.g., `token=abc`) will be masked. This is intentional — it is safer to over-mask than to leak credentials.
- **Already-masked strings**: Applying the sanitizer to an already-sanitized string is idempotent because `***` does not match the sensitive patterns.
- **Very long values**: The regexes use `[^\s&"']+` to capture the sensitive portion, so they stop at whitespace, `&`, `"`, or `'`. This avoids catastrophic backtracking.

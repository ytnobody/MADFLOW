# Anthropic API Key Preset Specification

## Overview

The `claude-api-standard` and `claude-api-cheap` presets allow MADFLOW to call the
Anthropic Messages API directly using `ANTHROPIC_API_KEY` instead of the Claude Code CLI.

This is useful when users do not have a Claude Pro/Max subscription and prefer to pay
per-request using the Anthropic API.

## Presets

| Preset              | Superintendent Model         | Engineer Model               |
|---------------------|------------------------------|------------------------------|
| `claude-api-standard` | `anthropic/claude-sonnet-4-6` | `anthropic/claude-haiku-4-5` |
| `claude-api-cheap`  | `anthropic/claude-haiku-4-5` | `anthropic/claude-haiku-4-5` |

Model strings use the `anthropic/` prefix to distinguish them from Claude CLI models.

## Backend: AnthropicAPIProcess

When a model string starts with `anthropic/`, a new `AnthropicAPIProcess` is created
instead of the default `ClaudeStreamProcess`.

### API Details

- Endpoint: `https://api.anthropic.com/v1/messages`
- Auth: `x-api-key: <ANTHROPIC_API_KEY>` header
- API Version: `anthropic-version: 2023-06-01`
- Environment variable: `ANTHROPIC_API_KEY`

### Agentic Loop

`AnthropicAPIProcess.Send()` implements an agentic loop similar to `GeminiAPIProcess`:

1. Send the prompt to the API
2. If the model uses `tool_use` blocks, execute them (bash only)
3. Feed the results back as `tool_result` user messages
4. Repeat until no tool calls or `anthropicMaxIter` reached

### Tool

Only the `bash` tool is exposed:

```json
{
  "name": "bash",
  "description": "Execute a bash command and return stdout/stderr.",
  "input_schema": {
    "type": "object",
    "properties": {
      "command": {"type": "string", "description": "The bash command to execute"}
    },
    "required": ["command"]
  }
}
```

### Error Handling

- Missing `ANTHROPIC_API_KEY`: return an error immediately
- HTTP 429 / rate-limit body: return `*RateLimitError` (triggers dormancy system)
- Other HTTP 4xx/5xx: return an error
- `stop_reason == "max_tokens"`: return partial text, no error
- Max iterations reached: return `*MaxIterationsError`

### Model Name Stripping

The `anthropic/` prefix is stripped before sending to the API.
Example: `anthropic/claude-sonnet-4-6` → API model `claude-sonnet-4-6`.

## Configuration (use command)

`madflow use claude-api-standard` writes the preset models to `madflow.toml`:

```toml
[agent.models]
superintendent = "anthropic/claude-sonnet-4-6"
engineer       = "anthropic/claude-haiku-4-5"
```

## formatPresets

The `formatPresets()` function in `use.go` includes both new presets in its output.

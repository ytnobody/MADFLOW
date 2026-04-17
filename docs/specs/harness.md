# Harness Auto-Generation Specification

## Overview

The `internal/harness/` package automatically generates harnesses (reproduction
tests, failure patterns, prompt improvement material) from agent failure logs.

While `internal/lessons/` produces human-readable lessons for the Superintendent,
the harness system produces **real code assets** to prevent regressions in CI.

## Design Philosophy

**Boundary**: What belongs where.

| Data | Location |
|---|---|
| Reproduction tests / harness body | Target repository git (e.g., `testdata/harness/` or `*_test.go`) |
| Failure pattern clusters / statistics | `~/.madflow/<project>/harness/patterns.json` |
| Raw harness material (prompt/output snapshots) | `~/.madflow/<project>/harness/cases/<id>/` |
| General prompt improvements | PR to MADFLOW `prompts/` |
| Project-specific prompt overrides | Target repo `.madflow/prompts/` |

## Responsibilities

The `harness` package is responsible for:

1. **Failure case extraction**: Extract failure events from `lessons.ScoringResult`
2. **Case persistence**: Save prompt/output/metadata to `~/.madflow/<project>/harness/cases/<id>/`
3. **Pattern classification**: Rule-based classification; accumulate stats in `patterns.json`
4. **Reproduction test proposal PR**: LLM generates a Go test draft; opens PR in target repo

## Data Structures

### FailureCase

A single captured failure event:

```go
type FailureCase struct {
    ID        string            // unique case ID: "<issueID>-<unix-nano>"
    IssueID   string            // source issue ID
    Timestamp time.Time         // when the case was captured
    Score     int               // scoring result (0-100)
    Failures  []lessons.Failure // individual failure items
    Pattern   string            // classified pattern key
    Prompt    string            // prompt snapshot (may be empty)
    Output    string            // output snapshot (may be empty)
}
```

### PatternStats

Aggregated statistics for a single pattern:

```go
type PatternStats struct {
    Pattern  string    // pattern key (matches ClassifyPattern output)
    Count    int       // total case count for this pattern
    LastSeen time.Time // timestamp of most recent case
}
```

### PatternsFile

Top-level structure of `patterns.json`:

```go
type PatternsFile struct {
    UpdatedAt time.Time               // last update time
    Patterns  map[string]PatternStats // keyed by pattern name
}
```

## File Layout

```
~/.madflow/<project>/harness/
  patterns.json                  ← aggregated pattern statistics
  cases/
    <caseID>/
      metadata.json              ← FailureCase (without Prompt/Output)
      prompt.txt                 ← prompt snapshot (may be absent)
      output.txt                 ← output snapshot (may be absent)
```

## Pattern Classification (Rule-Based)

Pattern keys are assigned based on the combination of detected failures.
Rules are applied in priority order:

| Pattern Key | Rule |
|---|---|
| `derived_issue` | Any failure with description matching "派生・修正Issue" |
| `clarification_needed` | Any failure with description matching "Clarification Needed" |
| `superintendent_direct_impl` | Any failure with description matching "Superintendentが直接実装" |
| `multiple_prs` | Any failure with description matching "PRが2本以上" |
| `multiple_failures` | 2 or more distinct failures not matching the above |
| `unknown` | No recognized failure description |

When multiple patterns match, the highest-risk one takes precedence.

## Reproduction Test Proposal

`ProposePR` performs the following steps:

1. Calls the Anthropic API with the failure case details to generate a Go test
   function that reproduces the failure scenario.
2. Writes the generated test to a file under `testdata/harness/` in the target
   repository's worktree (or opens/updates an existing file).
3. Creates a git commit and pushes a branch `harness/<caseID>` to the remote.
4. Opens a GitHub PR against `develop` with the generated test.

The generated test is a **draft** and may require manual adjustment before merging.

If `ANTHROPIC_API_KEY` is not set, a template-based test stub is generated
instead of calling the LLM.

## Integration with lessons Package

The `lessons.ScoringResult` type (already exported) is used as input to
`harness.Manager.ProcessScoringResult()`. No changes to the `lessons` package
API are required for the MVP.

## Data Flow

```
PR merged
  → handlePRMerged() [orchestrator]
    → lessons.Manager.ProcessMergedIssue()   ← existing
    → harness.Manager.ProcessScoringResult() ← NEW (called with same ScoringResult)
      → ClassifyPattern()
      → saveCase()        → ~/.madflow/<project>/harness/cases/<id>/
      → updatePatterns()  → ~/.madflow/<project>/harness/patterns.json

Harness proposal (manual or scheduled)
  → harness.Manager.ProposePR(caseID)
    → load case from disk
    → generate Go test via Anthropic API (or template fallback)
    → git commit & push branch harness/<caseID>
    → gh pr create → target repo
```

## Non-Scope (Future Issues)

- LLM-based failure clustering
- Automatic PR for MADFLOW-wide prompt improvements
- Harness self-evaluation / scoring

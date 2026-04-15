# Lessons Injection Specification

## Overview

The lessons system provides a feedback loop for the Superintendent by:
1. Scoring issue instruction quality when a PR is merged
2. Generating lessons from failures and persisting them to `.madflow/lessons.txt`
3. Maintaining at most 15 lessons (merging/trimming via LLM when exceeded)
4. Injecting lessons into the Superintendent's patrol prompt so they inform future issue instructions

## Scoring

When a PR is merged, the associated GitHub issue is scored starting from 100 points.
Deductions are applied for each detected failure:

| Failure | Deduction | Risk Level |
|---------|-----------|------------|
| Derived/fix issues created | -30 | 高 (High) |
| `[Clarification Needed]` comment exists | -20 | 中 (Medium) |
| Superintendent implemented directly | -20 | 中 (Medium) |
| 2 or more PRs created | -15 | 低 (Low) |

**Detection methods (using `gh` CLI):**
- Derived/fix issues: Search GitHub issues referencing the original issue number
- Clarification Needed: Scan issue comments for the `[Clarification Needed]` tag
- Direct implementation: Check PR body for "Superintendent implemented directly" or "Superintendentが直接実装"
- Multiple PRs: Count PRs for the `feature/issue-<issueID>` head branch

Scoring only applies to GitHub-synced issues (those with a `url` field). Local issues (`local-XXX`) are skipped.

## Lesson Generation

If the score is below 70, a lesson is generated using the Anthropic API:
- The lesson is a single line in Japanese describing what should have been done differently
- Format: `[危険度] 教訓テキスト` (e.g., `[高] バグ修正Issueは症状への対処だけでなく再発防止策まで含めること`)
- Risk level is determined by the highest-risk failure detected
- If `ANTHROPIC_API_KEY` is not set, a template-based fallback lesson is used

## Lesson Storage

- File: `<dataDir>/lessons.txt`
- Format: One lesson per line, each starting with `[高]`, `[中]`, or `[低]`
- Lessons are appended when generated

## Lessons Count Management (max 15)

When lessons exceed 15:
1. **Merge**: Call LLM to merge semantically similar lessons into one
2. **Trim**: If still over 15 after merging, delete lowest-risk lessons (oldest first for ties)

## Superintendent Prompt Injection

During issue patrol (`runIssuePatrol`), if `lessons.txt` is non-empty, the patrol message prepends all lessons so the Superintendent can reference them when writing issue instructions.

## Data Flow

```
PR merged
  → handlePRMerged() [orchestrator]
    → lessons.Manager.ProcessMergedIssue() [async goroutine]
      → ScoreIssue() [gh CLI calls]
      → if score < 70: GenerateLesson() [Anthropic API or fallback]
      → AppendLesson() [file write]
      → ManageLessonsCount() [LLM merge/trim if >15]

Issue patrol timer fires
  → runIssuePatrol()
    → Manager.InjectLessons() [file read]
    → prepend lessons to patrol message → superintendent
```

# Issue Granularity Judgment Specification

## Overview

When an engineer receives an issue assignment, they must evaluate whether the issue is scoped appropriately for a single implementation cycle. If the issue is too large (i.e., it requires multiple independent changes across different modules or features), the engineer should propose splitting it into sub-issues to the Superintendent before starting implementation.

## Purpose

- Prevent overly large PRs that are difficult to review
- Ensure each issue maps to a focused, reviewable unit of work
- Maintain clear traceability between issues and code changes

## Trigger Condition

After completing the Issue Review step (Step 1) and before creating a worktree (Step 2), the engineer evaluates the granularity of the issue.

## Granularity Judgment Criteria

### Split Required (Issue Too Large)

The engineer should propose sub-issue creation when the issue:

- Requires changes to **multiple independent features or modules**
- Contains **multiple logically separate concerns** that could be reviewed independently
- Has acceptance criteria that span **different system boundaries**
- Would result in a PR that is too large to review effectively

Examples:
- "Add authentication AND refactor the database layer"
- "Implement feature X AND fix bug Y in a different module"

### Proceed Without Splitting

The engineer should proceed directly to implementation when the issue:

- Represents a **single logical change** (one feature, one bug fix, one refactor)
- Is a **minor fix** with a clear, contained scope
- Follows an **existing pattern** that can be applied in one cycle
- Has already been reviewed and confirmed as appropriately scoped

Examples:
- "Fix the null pointer error in UserService"
- "Add logging to the payment module"
- "Update the engineer prompt to add step X"

## Sub-Issue Proposal Procedure

When splitting is determined to be necessary:

1. **Send a proposal to the Superintendent via the chat log**, including:
   - Why the issue is too large
   - Proposed split: how many sub-issues, and the scope of each
   - Which sub-issue should be implemented first (if there is a dependency)

2. **Wait for the Superintendent's approval** before starting large-scale implementation of the original issue.

3. **If the Superintendent approves the split**:
   - The Superintendent will create the sub-issues
   - Proceed with the assigned sub-issue(s)

4. **If the Superintendent determines no split is needed**:
   - Proceed with implementing the original issue as-is

## Inputs

- The issue file (`.toml`) containing `title`, `body`, and `status`
- The engineer's judgment of the scope based on the issue content

## Outputs

- Either: a sub-issue split proposal message in the chat log (and wait for approval)
- Or: immediate progression to Step 2 (Creating a Worktree)

## Edge Cases

- If the issue body is too vague to judge granularity, treat it as a potential ambiguity issue and ask the Superintendent for clarification (per the ambiguity handling flow)
- If an issue was already split by the Superintendent before assignment, no further splitting is needed

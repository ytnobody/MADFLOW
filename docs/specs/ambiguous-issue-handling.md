# Spec: Ambiguous Issue Handling for Engineers

## Overview

When an engineer receives an issue assignment, the issue body may not always clearly specify the intent or expected behavior. This spec defines how engineers should handle such cases to avoid incorrect implementations and wasted effort.

## Problem

Issues are sometimes written with:
- Vague or high-level descriptions that do not specify what exactly should change
- Missing details about edge cases or expected behavior
- Multiple possible interpretations of the requirement

If an engineer proceeds with implementation based on incorrect assumptions, the resulting code may not satisfy the actual requirement, leading to rework.

## Design

When reviewing an assigned issue (Step 1 of Implementation Flow), the engineer must assess whether the issue instructions are sufficiently clear before proceeding.

### Ambiguity Assessment Criteria

**Proceed directly (no clarification needed)** if:
- The change target (file, function, behavior) is clearly identifiable from the issue body
- The expected outcome is self-evident from context or prior related issues
- The issue includes design specs (e.g., in the `body` field) that fully describe the implementation
- The change is trivial and predictable (e.g., "add X to Y" with obvious X and Y)

**Ask for clarification** if:
- The issue body is too brief to understand what specifically should be changed
- Multiple valid interpretations exist and they lead to different implementations
- The scope of change is unclear (e.g., which files or components are affected)
- Business logic decisions are needed that are not within the engineer's authority to make unilaterally

### Clarification Flow

1. **Identify** the specific unclear points (not just a general "this is unclear")
2. **Ask the Superintendent** via the chat log with concrete questions
3. **Also post** the question as a GitHub Issue comment (if `url` field is present)
4. **Wait** for the Superintendent's response before beginning implementation
5. **Once clarified**, proceed with the standard implementation flow

### When to Proceed Without Clarification

If the intent is predictable or self-evident, skip the clarification step and proceed directly to:
- Writing spec documentation (Step 3)
- Writing test code (Step 4)
- Writing implementation code (Step 5)

Engineers should use judgment and err on the side of asking when genuinely unsure, but should not ask unnecessary questions for obviously clear requirements.

## Behavior Table

| Issue clarity | Action |
|--------------|--------|
| Fully specified (includes design specs) | Proceed to spec doc → test → implementation |
| Mostly clear, minor ambiguity | Make reasonable assumption, document in spec doc |
| Genuinely ambiguous (multiple interpretations) | Ask Superintendent for clarification |
| Completely vague (no actionable detail) | Ask Superintendent for clarification |

## Edge Cases

- If the Superintendent's clarification is itself ambiguous, ask a follow-up question
- If the issue has a `body` with a detailed design section (e.g., an architect's spec), treat it as fully specified
- Do not ask for clarification about technology choices or implementation details — those are the engineer's autonomous design decisions

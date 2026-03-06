# MADFLOW (Multi-Agent Development Flow) Specification

## 1. Concept

This framework is designed as a **"Dynamic Special-Team"** development flow that maximizes the parallel processing capabilities of AI agents while preventing context overflow and information confusion.

## 2. Agent Roles and Responsibilities

| Tier | Role | Role Details | Interface & Authority |
| --- | --- | --- | --- |
| **Oversight** | **Superintendent** | Full authority over the project. Combines the roles of PM, architect, reviewer, and release manager. Assigns newly opened issues to engineers. Makes design decisions, conducts code reviews, and determines merges and releases. Analyzes chat logs to file issues for resolving recurring bottlenecks. | **Human-facing (Issue comments)**, Engineer-facing |
| **Execution (N)** | **Engineer** | Code implementation based on the Superintendent's instructions. Questions and reports regarding implementation. | Superintendent-facing |

## 3. Communication Protocol

### 3.1. Shared Chat Log with Mentions

* All communication between agents takes place on a local shared text file (chat log).
* **Format**: `[@recipient] [sender]: [body]`
* All agents monitor this log at all times and only respond/act on mentions addressed to them.

### 3.2. Simple Communication Lines

To prevent information confusion, questions and reports are limited to the following lines:

* **Human ↔ Superintendent**: Communication through Issue comments.
* **Superintendent ↔ Engineer**: Issue assignment, design instructions, implementation questions/reports, review and merge instructions.

Overall, the flow is as simple as follows:

Human <-> Superintendent <-> Engineer

One engineer constitutes one team.

## 4. Execution Cycle and Branch Strategy

### 4.1. Dynamic Team Lifecycle

1. **Formation**: The Superintendent assigns one engineer to a specific issue and forms team (N).
2. **Design & Implementation**: The engineer creates a `feature` branch and carries out design, implementation, and testing. Unclear points are directed to the Superintendent.
3. **Review & Merge**: After the engineer creates a PR, the Superintendent reviews it and makes a merge decision.
4. **Disbandment**: After the Superintendent completes the merge to the `develop` branch, team (N) is dissolved.

### 4.2. Environment Management

* **main**: Production environment. Operated by the Superintendent.
* **develop**: Development integration environment. Target for human verification.
* **feature/issue-ID**: Each team's working environment.

## 5. Context Maintenance Protocol (8-Minute Reset)

To prevent AI performance degradation, the following steps are enforced:

1. **Timer monitoring**: All agents measure their own operation time and pause work after **8 minutes**.
2. **Work memo distillation**: Output the following 4 items as a "work memo" to the log:
   * Current state, decisions made, unresolved issues, next steps.

3. **Refresh**: Completely discard the context (session) once.
4. **Reload**: Only load the "original request" and "most recent work memo" and resume work.

## 6. Human's Role

* **Filing Issues**: File new requirements or bugs based on verification of the `develop` branch.
* **Final Decision-Making**: Responding to judgments (Yes/No, values, etc.) requested by the Superintendent via Issue comments.
* **Artifact Verification**: Final QA of features integrated into the `develop` branch.

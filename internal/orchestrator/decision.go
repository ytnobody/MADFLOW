package orchestrator

import (
	"fmt"

	"github.com/ytnobody/madflow/internal/issue"
)

// TeamAssignDecisionType is a sum type representing the possible outcomes
// of a team assignment decision.
type TeamAssignDecisionType int

const (
	// AssignDecisionReject means the issue is ineligible for team assignment.
	AssignDecisionReject TeamAssignDecisionType = iota
	// AssignDecisionReuseIdle means an existing idle standby team should be reused.
	AssignDecisionReuseIdle
	// AssignDecisionCreate means a new team should be created for the issue.
	AssignDecisionCreate
	// AssignDecisionDefer means team creation is deferred because max capacity is reached.
	AssignDecisionDefer
)

// String returns a human-readable label for the decision type.
func (d TeamAssignDecisionType) String() string {
	switch d {
	case AssignDecisionReject:
		return "Reject"
	case AssignDecisionReuseIdle:
		return "ReuseIdle"
	case AssignDecisionCreate:
		return "Create"
	case AssignDecisionDefer:
		return "Defer"
	default:
		return fmt.Sprintf("TeamAssignDecisionType(%d)", int(d))
	}
}

// TeamAssignResult holds the outcome of a team assignment decision.
type TeamAssignResult struct {
	// Decision is the action to take.
	Decision TeamAssignDecisionType
	// Reason is a human-readable explanation, suitable for chatlog or log output.
	Reason string
}

// DecideTeamAssignment is a pure function that determines the correct team
// assignment action for an issue given the current state of the team manager.
//
// Parameters:
//   - iss:           the issue requesting a team
//   - hasActiveTeam: true if an active or pending team already exists for the issue
//   - hasIdleTeam:   true if an idle standby team is available for reuse
//   - atCapacity:    true if the team manager is at max_teams capacity
//
// Decision priority (highest to lowest):
//  1. Terminal status or already-assigned → Reject
//  2. in_progress status → Reject
//  3. Active/pending team exists → Reject
//  4. Idle team available → ReuseIdle
//  5. At capacity → Defer
//  6. Otherwise → Create
func DecideTeamAssignment(iss issue.Issue, hasActiveTeam bool, hasIdleTeam bool, atCapacity bool) TeamAssignResult {
	// Reject terminal issues.
	if iss.Status.IsTerminal() {
		return TeamAssignResult{
			Decision: AssignDecisionReject,
			Reason:   fmt.Sprintf("issue status is %s", iss.Status),
		}
	}

	// Reject in_progress issues to prevent duplicate team creation.
	// in_progress means someone is already working on it. To re-assign,
	// the status must first be reset to "open".
	if iss.Status == issue.StatusInProgress {
		return TeamAssignResult{
			Decision: AssignDecisionReject,
			Reason:   "イシューのステータスが in_progress です。再アサインする場合はステータスを open に変更してください",
		}
	}

	// Reject issues already assigned to a team.
	if iss.AssignedTeam > 0 {
		return TeamAssignResult{
			Decision: AssignDecisionReject,
			Reason:   fmt.Sprintf("アサイン済みです (チーム %d)", iss.AssignedTeam),
		}
	}

	// Reject if an active or pending team already exists (duplicate prevention).
	if hasActiveTeam {
		return TeamAssignResult{
			Decision: AssignDecisionReject,
			Reason:   "アクティブなチームが既に存在します",
		}
	}

	// Prefer reusing an idle standby team over creating a new one.
	if hasIdleTeam {
		return TeamAssignResult{
			Decision: AssignDecisionReuseIdle,
			Reason:   "idle standby team available for reuse",
		}
	}

	// If at capacity, defer until a slot becomes available.
	if atCapacity {
		return TeamAssignResult{
			Decision: AssignDecisionDefer,
			Reason:   "team manager is at max_teams capacity",
		}
	}

	// All checks passed: create a new team.
	return TeamAssignResult{
		Decision: AssignDecisionCreate,
		Reason:   "new team will be created for the issue",
	}
}

package orchestrator

import (
	"testing"

	"github.com/ytnobody/madflow/internal/issue"
)

func TestDecideTeamAssignment(t *testing.T) {
	openIssue := issue.Issue{
		ID:           "gh-100",
		Title:        "test issue",
		Status:       issue.StatusOpen,
		AssignedTeam: 0,
	}
	inProgressIssue := issue.Issue{
		ID:           "gh-101",
		Title:        "test issue",
		Status:       issue.StatusInProgress,
		AssignedTeam: 0,
	}
	assignedIssue := issue.Issue{
		ID:           "gh-102",
		Title:        "test issue",
		Status:       issue.StatusInProgress,
		AssignedTeam: 3,
	}
	resolvedIssue := issue.Issue{
		ID:     "gh-103",
		Title:  "test issue",
		Status: issue.StatusResolved,
	}
	closedIssue := issue.Issue{
		ID:     "gh-104",
		Title:  "test issue",
		Status: issue.StatusClosed,
	}

	tests := []struct {
		name          string
		iss           issue.Issue
		hasActiveTeam bool
		hasIdleTeam   bool
		atCapacity    bool
		wantDecision  TeamAssignDecisionType
	}{
		{
			name:         "resolved issue is rejected",
			iss:          resolvedIssue,
			wantDecision: AssignDecisionReject,
		},
		{
			name:         "closed issue is rejected",
			iss:          closedIssue,
			wantDecision: AssignDecisionReject,
		},
		{
			name:         "already assigned issue is rejected",
			iss:          assignedIssue,
			wantDecision: AssignDecisionReject,
		},
		{
			name:          "issue with active team is rejected",
			iss:           openIssue,
			hasActiveTeam: true,
			wantDecision:  AssignDecisionReject,
		},
		{
			name:         "open issue with idle team reuses idle",
			iss:          openIssue,
			hasIdleTeam:  true,
			wantDecision: AssignDecisionReuseIdle,
		},
		{
			name:         "in_progress issue with idle team reuses idle",
			iss:          inProgressIssue,
			hasIdleTeam:  true,
			wantDecision: AssignDecisionReuseIdle,
		},
		{
			name:         "open issue at capacity is deferred",
			iss:          openIssue,
			atCapacity:   true,
			wantDecision: AssignDecisionDefer,
		},
		{
			name:         "open issue with no idle and no capacity creates new team",
			iss:          openIssue,
			wantDecision: AssignDecisionCreate,
		},
		{
			name:         "in_progress issue with no assignment creates new team",
			iss:          inProgressIssue,
			wantDecision: AssignDecisionCreate,
		},
		{
			name:          "active team check takes priority over idle team",
			iss:           openIssue,
			hasActiveTeam: true,
			hasIdleTeam:   true,
			wantDecision:  AssignDecisionReject,
		},
		{
			name:         "idle team takes priority over capacity",
			iss:          openIssue,
			hasIdleTeam:  true,
			atCapacity:   true,
			wantDecision: AssignDecisionReuseIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DecideTeamAssignment(tt.iss, tt.hasActiveTeam, tt.hasIdleTeam, tt.atCapacity)
			if result.Decision != tt.wantDecision {
				t.Errorf("DecideTeamAssignment() Decision = %v, want %v (reason: %s)",
					result.Decision, tt.wantDecision, result.Reason)
			}
			if result.Reason == "" {
				t.Errorf("DecideTeamAssignment() returned empty Reason")
			}
		})
	}
}

func TestTeamAssignDecisionTypeString(t *testing.T) {
	tests := []struct {
		d    TeamAssignDecisionType
		want string
	}{
		{AssignDecisionReject, "Reject"},
		{AssignDecisionReuseIdle, "ReuseIdle"},
		{AssignDecisionCreate, "Create"},
		{AssignDecisionDefer, "Defer"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.d.String()
			if got != tt.want {
				t.Errorf("TeamAssignDecisionType(%d).String() = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

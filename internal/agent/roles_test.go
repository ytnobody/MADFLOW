package agent

import "testing"

func TestAgentIDString(t *testing.T) {
	tests := []struct {
		id   AgentID
		want string
	}{
		{AgentID{Role: RoleSuperintendent, TeamNum: 0}, "superintendent"},
		{AgentID{Role: RolePM, TeamNum: 0}, "pm"},
		{AgentID{Role: RoleArchitect, TeamNum: 1}, "architect-1"},
		{AgentID{Role: RoleEngineer, TeamNum: 3}, "engineer-3"},
	}
	for _, tt := range tests {
		if got := tt.id.String(); got != tt.want {
			t.Errorf("AgentID.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestCanSendTo(t *testing.T) {
	tests := []struct {
		from Role
		to   Role
		want bool
	}{
		{RoleSuperintendent, RolePM, true},
		{RoleSuperintendent, RoleEngineer, false},
		{RolePM, RoleSuperintendent, true},
		{RolePM, RoleArchitect, true},
		{RolePM, RoleOrchestrator, true},
		{RolePM, RoleEngineer, false},
		{RoleArchitect, RoleEngineer, true},
		{RoleArchitect, RoleReviewer, false},
		{RoleEngineer, RoleArchitect, true},
		{RoleEngineer, RoleReviewer, true},
		{RoleEngineer, RolePM, false},
		{RoleReviewer, RoleEngineer, true},
		{RoleReviewer, RoleReleaseManager, true},
		{RoleReviewer, RolePM, false},
		{RoleReleaseManager, RoleReviewer, true},
		{RoleReleaseManager, RolePM, false},
	}
	for _, tt := range tests {
		got := CanSendTo(tt.from, tt.to)
		if got != tt.want {
			t.Errorf("CanSendTo(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

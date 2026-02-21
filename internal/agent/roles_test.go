package agent

import "testing"

func TestAgentIDString(t *testing.T) {
	tests := []struct {
		id   AgentID
		want string
	}{
		{AgentID{Role: RoleSuperintendent, TeamNum: 0}, "superintendent"},
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
		{RoleSuperintendent, RoleArchitect, true},
		{RoleSuperintendent, RoleOrchestrator, true},
		{RoleSuperintendent, RoleEngineer, false},
		{RoleArchitect, RoleSuperintendent, true},
		{RoleArchitect, RoleEngineer, true},
		{RoleEngineer, RoleArchitect, true},
		{RoleEngineer, RoleSuperintendent, false},
	}
	for _, tt := range tests {
		got := CanSendTo(tt.from, tt.to)
		if got != tt.want {
			t.Errorf("CanSendTo(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

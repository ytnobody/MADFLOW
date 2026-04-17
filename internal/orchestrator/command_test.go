package orchestrator

import (
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType CommandType
		wantArgs []string
	}{
		{
			name:     "TEAM_CREATE with issue ID",
			input:    "TEAM_CREATE gh-123",
			wantType: CommandTeamCreate,
			wantArgs: []string{"gh-123"},
		},
		{
			name:     "TEAM_CREATE with extra text",
			input:    "TEAM_CREATE gh-123（2回目）",
			wantType: CommandTeamCreate,
			wantArgs: []string{"gh-123（2回目）"},
		},
		{
			name:     "TEAM_CREATE no args",
			input:    "TEAM_CREATE",
			wantType: CommandTeamCreate,
			wantArgs: []string{},
		},
		{
			name:     "TEAM_DISBAND with team number",
			input:    "TEAM_DISBAND 2",
			wantType: CommandTeamDisband,
			wantArgs: []string{"2"},
		},
		{
			name:     "RELEASE command",
			input:    "RELEASE v1.2.3",
			wantType: CommandRelease,
			wantArgs: []string{"v1.2.3"},
		},
		{
			name:     "WAKE_GITHUB no args",
			input:    "WAKE_GITHUB",
			wantType: CommandWakeGitHub,
			wantArgs: []string{},
		},
		{
			name:     "PATROL_COMPLETE no args",
			input:    "PATROL_COMPLETE",
			wantType: CommandPatrolComplete,
			wantArgs: []string{},
		},
		{
			name:     "unknown command",
			input:    "UNKNOWN_CMD foo",
			wantType: CommandUnknown,
			wantArgs: nil,
		},
		{
			name:     "empty body",
			input:    "",
			wantType: CommandUnknown,
			wantArgs: nil,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			wantType: CommandUnknown,
			wantArgs: nil,
		},
		{
			name:     "leading whitespace trimmed",
			input:    "  TEAM_CREATE gh-42",
			wantType: CommandTeamCreate,
			wantArgs: []string{"gh-42"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCommand(tt.input)
			if got.Type != tt.wantType {
				t.Errorf("ParseCommand(%q).Type = %v, want %v", tt.input, got.Type, tt.wantType)
			}
			if tt.wantArgs == nil {
				if len(got.Args) != 0 {
					t.Errorf("ParseCommand(%q).Args = %v, want nil/empty", tt.input, got.Args)
				}
			} else {
				if len(got.Args) != len(tt.wantArgs) {
					t.Errorf("ParseCommand(%q).Args = %v, want %v", tt.input, got.Args, tt.wantArgs)
				} else {
					for i, arg := range got.Args {
						if arg != tt.wantArgs[i] {
							t.Errorf("ParseCommand(%q).Args[%d] = %q, want %q", tt.input, i, arg, tt.wantArgs[i])
						}
					}
				}
			}
		})
	}
}

func TestCommandTypeString(t *testing.T) {
	tests := []struct {
		ct   CommandType
		want string
	}{
		{CommandTeamCreate, "TEAM_CREATE"},
		{CommandTeamDisband, "TEAM_DISBAND"},
		{CommandRelease, "RELEASE"},
		{CommandWakeGitHub, "WAKE_GITHUB"},
		{CommandPatrolComplete, "PATROL_COMPLETE"},
		{CommandUnknown, "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.ct.String()
			if got != tt.want {
				t.Errorf("CommandType(%d).String() = %q, want %q", tt.ct, got, tt.want)
			}
		})
	}
}

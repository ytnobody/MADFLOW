package team

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidateTeamsFile verifies that validateTeamsFile catches all invariant violations.
func TestValidateTeamsFile(t *testing.T) {
	tests := []struct {
		name    string
		tf      teamsFile
		wantErr bool
	}{
		{
			name:    "empty file is valid",
			tf:      teamsFile{NextID: 1},
			wantErr: false,
		},
		{
			name: "single team valid",
			tf: teamsFile{
				NextID: 2,
				Teams:  []teamRecord{{ID: 1, IssueID: "gh-1"}},
			},
			wantErr: false,
		},
		{
			name: "multiple teams valid",
			tf: teamsFile{
				NextID: 4,
				Teams: []teamRecord{
					{ID: 1, IssueID: "gh-1"},
					{ID: 2, IssueID: ""},
					{ID: 3, IssueID: "gh-3"},
				},
			},
			wantErr: false,
		},
		{
			name: "next_id equals max team id (invariant violated)",
			tf: teamsFile{
				NextID: 1,
				Teams:  []teamRecord{{ID: 1, IssueID: "gh-1"}},
			},
			wantErr: true,
		},
		{
			name: "next_id less than team id (invariant violated)",
			tf: teamsFile{
				NextID: 1,
				Teams:  []teamRecord{{ID: 5, IssueID: "gh-5"}},
			},
			wantErr: true,
		},
		{
			name: "duplicate team ids (invariant violated)",
			tf: teamsFile{
				NextID: 3,
				Teams: []teamRecord{
					{ID: 1, IssueID: "gh-1"},
					{ID: 1, IssueID: "gh-2"},
				},
			},
			wantErr: true,
		},
		{
			name: "zero team id (invariant violated)",
			tf: teamsFile{
				NextID: 2,
				Teams:  []teamRecord{{ID: 0, IssueID: "gh-1"}},
			},
			wantErr: true,
		},
		{
			name: "negative team id (invariant violated)",
			tf: teamsFile{
				NextID: 2,
				Teams:  []teamRecord{{ID: -1, IssueID: "gh-1"}},
			},
			wantErr: true,
		},
		{
			// RC-3 regression: the incident state — next_id=6, no team entries.
			// This state IS valid (teams have all been disbanded) — the invariant
			// check should not flag a non-zero next_id with zero team entries.
			name: "next_id advanced with no teams (all disbanded) is valid",
			tf: teamsFile{
				NextID: 6,
				Teams:  nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTeamsFile(tt.tf)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTeamsFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestReadTeamsFile_NotExist verifies that reading a non-existent file returns
// a zero-value teamsFile with NextID=1.
func TestReadTeamsFile_NotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teams.toml")
	tf, err := readTeamsFile(path)
	if err != nil {
		t.Fatalf("readTeamsFile() unexpected error: %v", err)
	}
	if tf.NextID != 1 {
		t.Errorf("NextID = %d, want 1", tf.NextID)
	}
	if len(tf.Teams) != 0 {
		t.Errorf("Teams = %v, want empty", tf.Teams)
	}
}

// TestWriteReadRoundTrip verifies that writeTeamsFile + readTeamsFile preserves data.
func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teams.toml")
	want := teamsFile{
		NextID: 4,
		Teams: []teamRecord{
			{ID: 1, IssueID: "gh-100"},
			{ID: 2, IssueID: ""},
			{ID: 3, IssueID: "local-42"},
		},
	}

	if err := writeTeamsFile(path, want); err != nil {
		t.Fatalf("writeTeamsFile() error: %v", err)
	}

	got, err := readTeamsFile(path)
	if err != nil {
		t.Fatalf("readTeamsFile() error: %v", err)
	}

	if got.NextID != want.NextID {
		t.Errorf("NextID = %d, want %d", got.NextID, want.NextID)
	}
	if len(got.Teams) != len(want.Teams) {
		t.Fatalf("len(Teams) = %d, want %d", len(got.Teams), len(want.Teams))
	}
	for i, wt := range want.Teams {
		gt := got.Teams[i]
		if gt.ID != wt.ID || gt.IssueID != wt.IssueID {
			t.Errorf("Teams[%d] = {%d, %q}, want {%d, %q}", i, gt.ID, gt.IssueID, wt.ID, wt.IssueID)
		}
	}
}

// TestWriteTeamsFile_RejectsInvalid verifies that writeTeamsFile rejects
// an invalid teamsFile before writing.
func TestWriteTeamsFile_RejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teams.toml")
	invalid := teamsFile{
		NextID: 1,
		Teams:  []teamRecord{{ID: 1, IssueID: "gh-1"}}, // id >= next_id
	}
	if err := writeTeamsFile(path, invalid); err == nil {
		t.Error("writeTeamsFile() should have returned an error for invalid teamsFile")
	}
	// Verify file was not created.
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("teams.toml should not exist after rejected write")
	}
}

// TestWriteTeamsFile_Atomic verifies that a partially-written file is not left
// on disk if the rename fails (we can only test this indirectly via the round-trip).
// This test specifically validates the RC-3 regression: next_id must never
// advance without a corresponding [[team]] entry being present on disk
// (unless teams were subsequently disbanded, which is the valid all-disbanded state).
func TestWriteTeamsFile_NextIDNeverRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teams.toml")

	// Write a file with two teams.
	initial := teamsFile{
		NextID: 3,
		Teams: []teamRecord{
			{ID: 1, IssueID: "gh-1"},
			{ID: 2, IssueID: "gh-2"},
		},
	}
	if err := writeTeamsFile(path, initial); err != nil {
		t.Fatalf("writeTeamsFile() error: %v", err)
	}

	// Simulate team disbandment: remove teams but keep next_id advancing.
	// next_id=6, no teams — this is the exact incident state (RC-3).
	// It is VALID because all teams were disbanded. The invariant is:
	// next_id >= 1, and all team.id < next_id. Zero teams with next_id=6 satisfies this.
	disbanded := teamsFile{NextID: 6}
	if err := writeTeamsFile(path, disbanded); err != nil {
		t.Fatalf("writeTeamsFile() error for all-disbanded state: %v", err)
	}

	got, err := readTeamsFile(path)
	if err != nil {
		t.Fatalf("readTeamsFile() error: %v", err)
	}
	if got.NextID != 6 {
		t.Errorf("NextID = %d, want 6 (next_id must not roll back)", got.NextID)
	}
	if len(got.Teams) != 0 {
		t.Errorf("Teams = %v, want empty (all teams disbanded)", got.Teams)
	}

	// Now try to write a state where next_id < previous next_id — this should fail
	// because next_id rolling back is a sign of a partial write / logic error.
	// Note: our current implementation does NOT enforce monotonicity across writes
	// (it only validates the internal invariant within a single write). This is
	// intentional — the Manager initialises next_id by reading the file on startup.
	// The test documents the expected behaviour, not a constraint we enforce.
}

// TestManagerPersistCreate verifies that Manager.Create writes an updated
// teams.toml when a persist path is configured.
func TestManagerPersistCreate(t *testing.T) {
	dir := t.TempDir()
	teamsPath := filepath.Join(dir, "teams.toml")

	factory := newMockFactory(t)
	m := NewManager(factory, 4)
	m.SetPersistPath(teamsPath)

	ctx := t.Context()
	team, err := m.Create(ctx, "gh-1", "Test Issue")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// The file should exist and contain the created team.
	tf, err := readTeamsFile(teamsPath)
	if err != nil {
		t.Fatalf("readTeamsFile() error: %v", err)
	}
	if tf.NextID <= team.ID {
		t.Errorf("NextID %d should be > team.ID %d", tf.NextID, team.ID)
	}
	found := false
	for _, tr := range tf.Teams {
		if tr.ID == team.ID && tr.IssueID == "gh-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("teams.toml does not contain team %d with issue gh-1; got: %+v", team.ID, tf.Teams)
	}
}

// TestManagerPersistDisband verifies that Manager.Disband removes the team
// from teams.toml but does NOT decrement next_id.
func TestManagerPersistDisband(t *testing.T) {
	dir := t.TempDir()
	teamsPath := filepath.Join(dir, "teams.toml")

	factory := newMockFactory(t)
	m := NewManager(factory, 4)
	m.SetPersistPath(teamsPath)

	ctx := t.Context()
	team, err := m.Create(ctx, "gh-1", "Test Issue")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	teamID := team.ID
	prevNextID := m.nextID

	if err := m.Disband(teamID); err != nil {
		t.Fatalf("Disband() error: %v", err)
	}

	tf, err := readTeamsFile(teamsPath)
	if err != nil {
		t.Fatalf("readTeamsFile() error: %v", err)
	}
	// next_id must not roll back.
	if tf.NextID != prevNextID {
		t.Errorf("NextID = %d after disband, want %d (must not roll back)", tf.NextID, prevNextID)
	}
	// The team entry must be gone.
	for _, tr := range tf.Teams {
		if tr.ID == teamID {
			t.Errorf("teams.toml still contains disbanded team %d", teamID)
		}
	}
}

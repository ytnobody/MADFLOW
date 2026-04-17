package team

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// teamsFile is the on-disk representation of teams.toml.
// It is read and written atomically to prevent partial-write inconsistencies.
//
// Invariants maintained by this package:
//   - NextID is always > max(team.ID) across all entries (monotonically increasing,
//     never rolled back even when teams are disbanded).
//   - No two entries share the same ID.
//   - Every entry's ID is strictly less than NextID.
type teamsFile struct {
	NextID int          `toml:"next_id"`
	Teams  []teamRecord `toml:"team"`
}

// teamRecord is one [[team]] TOML array-of-tables entry.
type teamRecord struct {
	ID      int    `toml:"id"`
	IssueID string `toml:"issue_id"`
}

// validateTeamsFile checks the teamsFile invariants.
// Returns an error describing the first violation found.
func validateTeamsFile(tf teamsFile) error {
	seen := make(map[int]bool, len(tf.Teams))
	for _, t := range tf.Teams {
		if t.ID <= 0 {
			return fmt.Errorf("teams.toml invariant violated: team id must be > 0, got %d", t.ID)
		}
		if t.ID >= tf.NextID {
			return fmt.Errorf("teams.toml invariant violated: team id %d >= next_id %d", t.ID, tf.NextID)
		}
		if seen[t.ID] {
			return fmt.Errorf("teams.toml invariant violated: duplicate team id %d", t.ID)
		}
		seen[t.ID] = true
	}
	return nil
}

// readTeamsFile reads teams.toml from path.
// Returns an empty teamsFile (next_id=1, no teams) if the file does not exist.
func readTeamsFile(path string) (teamsFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return teamsFile{NextID: 1}, nil
	}
	if err != nil {
		return teamsFile{}, fmt.Errorf("read teams file: %w", err)
	}

	var tf teamsFile
	if _, err := toml.Decode(string(data), &tf); err != nil {
		return teamsFile{}, fmt.Errorf("parse teams file: %w", err)
	}
	return tf, nil
}

// writeTeamsFile validates tf and writes it to path atomically.
// The write is atomic: a temp file is written next to path, then renamed over it.
func writeTeamsFile(path string, tf teamsFile) error {
	if err := validateTeamsFile(tf); err != nil {
		return err
	}

	// Build TOML manually to produce a human-readable file with
	// comments and the teams as [[team]] array-of-tables entries.
	// Using the encoder directly avoids import of a TOML writer that
	// might not be available; BurntSushi/toml supports encoding.
	var buf []byte
	buf = append(buf, fmt.Sprintf("next_id = %d\n", tf.NextID)...)
	for _, t := range tf.Teams {
		buf = append(buf, "\n[[team]]\n"...)
		buf = append(buf, fmt.Sprintf("id = %d\n", t.ID)...)
		buf = append(buf, fmt.Sprintf("issue_id = %q\n", t.IssueID)...)
	}

	// Write to a temp file then rename for atomicity.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, buf, 0600); err != nil {
		return fmt.Errorf("write teams tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("rename teams tmp file: %w", err)
	}
	return nil
}

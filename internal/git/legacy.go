package git

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// legacyWorktreePattern matches directory names of the old-format worktree:
// "issue-{number}" (e.g. "issue-42").
var legacyWorktreePattern = regexp.MustCompile(`^issue-\d+$`)

// legacyBranchPattern matches branch names of the old format:
// "feature/issue-{number}" (e.g. "feature/issue-42").
var legacyBranchPattern = regexp.MustCompile(`^feature/issue-\d+$`)

// DetectLegacyWorktrees scans <madflowDir>/worktrees/ for directories whose
// names match the old-format "issue-{number}" pattern.
// It returns a slice of relative display paths in the form
// ".madflow/worktrees/issue-{number}/" for each detected legacy directory.
// Returns an empty (nil) slice when none are found or the directory does not exist.
func (r *Repo) DetectLegacyWorktrees(madflowDir string) []string {
	worktreesDir := filepath.Join(madflowDir, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		// Directory absent or unreadable — nothing to report.
		return nil
	}

	var found []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if legacyWorktreePattern.MatchString(entry.Name()) {
			// Build a human-readable relative path for the warning message.
			rel, err := filepath.Rel(r.path, filepath.Join(worktreesDir, entry.Name()))
			if err != nil {
				rel = filepath.Join(".madflow", "worktrees", entry.Name())
			}
			found = append(found, rel+string(filepath.Separator))
		}
	}
	return found
}

// DetectLegacyBranches lists local branches whose names match the old-format
// "feature/issue-{number}" pattern.
// Returns a slice of branch names, or an empty (nil) slice when none are found.
func (r *Repo) DetectLegacyBranches() []string {
	out, err := r.run("branch", "--format=%(refname:short)")
	if err != nil {
		return nil
	}

	var found []string
	for line := range strings.SplitSeq(out, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" {
			continue
		}
		if legacyBranchPattern.MatchString(branch) {
			found = append(found, branch)
		}
	}
	return found
}

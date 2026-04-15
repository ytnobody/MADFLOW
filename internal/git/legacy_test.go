package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDetectLegacyWorktrees_Found verifies that legacy worktree directories
// (issue-{number} pattern) under .madflow/worktrees/ are detected.
func TestDetectLegacyWorktrees_Found(t *testing.T) {
	repo := initTestRepo(t)

	madflowDir := filepath.Join(repo.Path(), ".madflow")
	worktreesDir := filepath.Join(madflowDir, "worktrees")

	// Create legacy worktree directories.
	legacyPaths := []string{
		filepath.Join(worktreesDir, "issue-1"),
		filepath.Join(worktreesDir, "issue-42"),
		filepath.Join(worktreesDir, "issue-999"),
	}
	for _, p := range legacyPaths {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("create legacy worktree dir %s: %v", p, err)
		}
	}

	detected := repo.DetectLegacyWorktrees(madflowDir)
	if len(detected) != 3 {
		t.Errorf("expected 3 legacy worktrees, got %d: %v", len(detected), detected)
	}

	// Each entry should follow the .madflow/worktrees/issue-{number}/ form.
	for _, d := range detected {
		// Strip trailing separator to get the directory name, then check
		// that its parent component is "worktrees".
		clean := strings.TrimSuffix(d, string(filepath.Separator))
		if filepath.Base(filepath.Dir(clean)) != "worktrees" {
			t.Errorf("unexpected path format: %s", d)
		}
	}
}

// TestDetectLegacyWorktrees_NewFormatNotDetected verifies that new-format
// worktrees ({gh_login}/issue-{number}) are NOT detected as legacy.
func TestDetectLegacyWorktrees_NewFormatNotDetected(t *testing.T) {
	repo := initTestRepo(t)

	madflowDir := filepath.Join(repo.Path(), ".madflow")
	worktreesDir := filepath.Join(madflowDir, "worktrees")

	// New format: sub-directory structure.
	newFormatPath := filepath.Join(worktreesDir, "ytnobody", "issue-1")
	if err := os.MkdirAll(newFormatPath, 0755); err != nil {
		t.Fatalf("create new-format worktree dir: %v", err)
	}

	detected := repo.DetectLegacyWorktrees(madflowDir)
	if len(detected) != 0 {
		t.Errorf("expected 0 legacy worktrees, got %d: %v", len(detected), detected)
	}
}

// TestDetectLegacyWorktrees_NonNumericNotDetected verifies that directories
// with names that don't match issue-{number} are not reported as legacy.
func TestDetectLegacyWorktrees_NonNumericNotDetected(t *testing.T) {
	repo := initTestRepo(t)

	madflowDir := filepath.Join(repo.Path(), ".madflow")
	worktreesDir := filepath.Join(madflowDir, "worktrees")

	// These should NOT be detected as legacy.
	dirs := []string{
		filepath.Join(worktreesDir, "team-1"),
		filepath.Join(worktreesDir, "custom"),
		filepath.Join(worktreesDir, "issue-abc"),
	}
	for _, p := range dirs {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("create dir %s: %v", p, err)
		}
	}

	detected := repo.DetectLegacyWorktrees(madflowDir)
	if len(detected) != 0 {
		t.Errorf("expected 0 legacy worktrees, got %d: %v", len(detected), detected)
	}
}

// TestDetectLegacyWorktrees_NoWorktreesDir verifies that missing .madflow/worktrees/
// directory returns an empty slice without error.
func TestDetectLegacyWorktrees_NoWorktreesDir(t *testing.T) {
	repo := initTestRepo(t)
	madflowDir := filepath.Join(repo.Path(), ".madflow")

	// Do not create the worktrees directory.
	detected := repo.DetectLegacyWorktrees(madflowDir)
	if len(detected) != 0 {
		t.Errorf("expected 0 legacy worktrees when dir absent, got %d: %v", len(detected), detected)
	}
}

// TestDetectLegacyBranches_Found verifies that local branches matching
// feature/issue-{number} are detected as legacy.
func TestDetectLegacyBranches_Found(t *testing.T) {
	repo := initTestRepo(t)

	baseBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}

	// Create legacy branches.
	legacyBranches := []string{
		"feature/issue-1",
		"feature/issue-42",
		"feature/issue-999",
	}
	for _, b := range legacyBranches {
		run(t, repo.Path(), "git", "branch", b, baseBranch)
	}

	detected := repo.DetectLegacyBranches()
	if len(detected) != 3 {
		t.Errorf("expected 3 legacy branches, got %d: %v", len(detected), detected)
	}
}

// TestDetectLegacyBranches_NewFormatNotDetected verifies that new-format branches
// (madflow/{gh_login}/issue-{number}) are NOT detected as legacy.
func TestDetectLegacyBranches_NewFormatNotDetected(t *testing.T) {
	repo := initTestRepo(t)

	baseBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}

	// New-format branches should NOT be detected.
	run(t, repo.Path(), "git", "branch", "madflow/ytnobody/issue-1", baseBranch)
	run(t, repo.Path(), "git", "branch", "madflow/alice/issue-42", baseBranch)

	detected := repo.DetectLegacyBranches()
	if len(detected) != 0 {
		t.Errorf("expected 0 legacy branches, got %d: %v", len(detected), detected)
	}
}

// TestDetectLegacyBranches_OtherBranchesNotDetected verifies that unrelated
// branches are not reported as legacy.
func TestDetectLegacyBranches_OtherBranchesNotDetected(t *testing.T) {
	repo := initTestRepo(t)

	baseBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}

	// Branches that should not be reported as legacy.
	run(t, repo.Path(), "git", "branch", "develop", baseBranch)
	run(t, repo.Path(), "git", "branch", "feature/issue-abc", baseBranch) // non-numeric
	run(t, repo.Path(), "git", "branch", "hotfix/issue-1", baseBranch)    // wrong prefix

	detected := repo.DetectLegacyBranches()
	if len(detected) != 0 {
		t.Errorf("expected 0 legacy branches, got %d: %v", len(detected), detected)
	}
}

// TestDetectLegacyBranches_NoBranches verifies that an empty repo returns
// an empty slice.
func TestDetectLegacyBranches_NoBranches(t *testing.T) {
	repo := initTestRepo(t)

	detected := repo.DetectLegacyBranches()
	if len(detected) != 0 {
		t.Errorf("expected 0 legacy branches in fresh repo, got %d: %v", len(detected), detected)
	}
}

// TestDetectLegacyBranches_Mixed verifies that only legacy branches are returned
// when both legacy and non-legacy branches coexist.
func TestDetectLegacyBranches_Mixed(t *testing.T) {
	repo := initTestRepo(t)

	baseBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}

	run(t, repo.Path(), "git", "branch", "feature/issue-10", baseBranch)      // legacy
	run(t, repo.Path(), "git", "branch", "madflow/user/issue-10", baseBranch) // new format
	run(t, repo.Path(), "git", "branch", "develop", baseBranch)               // unrelated

	detected := repo.DetectLegacyBranches()
	if len(detected) != 1 {
		t.Errorf("expected 1 legacy branch, got %d: %v", len(detected), detected)
	}
	if len(detected) > 0 && detected[0] != "feature/issue-10" {
		t.Errorf("expected 'feature/issue-10', got %q", detected[0])
	}
}

// TestDeleteLegacyBranches_DeletesAll verifies that all detected legacy branches
// are force-deleted and their names are returned.
func TestDeleteLegacyBranches_DeletesAll(t *testing.T) {
	repo := initTestRepo(t)

	baseBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}

	// Create legacy branches.
	legacyBranches := []string{
		"feature/issue-1",
		"feature/issue-42",
		"feature/issue-999",
	}
	for _, b := range legacyBranches {
		run(t, repo.Path(), "git", "branch", b, baseBranch)
	}

	deleted, err := repo.DeleteLegacyBranches()
	if err != nil {
		t.Fatalf("DeleteLegacyBranches returned error: %v", err)
	}
	if len(deleted) != 3 {
		t.Errorf("expected 3 deleted branches, got %d: %v", len(deleted), deleted)
	}

	// All legacy branches must no longer exist.
	for _, b := range legacyBranches {
		if repo.BranchExists(b) {
			t.Errorf("expected branch %q to be deleted, but it still exists", b)
		}
	}
}

// TestDeleteLegacyBranches_NonLegacyUntouched verifies that non-legacy branches
// are not deleted.
func TestDeleteLegacyBranches_NonLegacyUntouched(t *testing.T) {
	repo := initTestRepo(t)

	baseBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}

	run(t, repo.Path(), "git", "branch", "madflow/user/issue-1", baseBranch) // new format
	run(t, repo.Path(), "git", "branch", "develop", baseBranch)              // unrelated

	deleted, err := repo.DeleteLegacyBranches()
	if err != nil {
		t.Fatalf("DeleteLegacyBranches returned error: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted branches, got %d: %v", len(deleted), deleted)
	}

	// Non-legacy branches must still exist.
	for _, b := range []string{"madflow/user/issue-1", "develop"} {
		if !repo.BranchExists(b) {
			t.Errorf("expected branch %q to still exist", b)
		}
	}
}

// TestDeleteLegacyBranches_NoLegacy verifies that calling DeleteLegacyBranches
// when no legacy branches exist returns an empty slice without error.
func TestDeleteLegacyBranches_NoLegacy(t *testing.T) {
	repo := initTestRepo(t)

	deleted, err := repo.DeleteLegacyBranches()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted branches in fresh repo, got %d: %v", len(deleted), deleted)
	}
}

// TestDeleteLegacyBranches_Mixed verifies that only legacy branches are deleted
// when both legacy and non-legacy branches coexist.
func TestDeleteLegacyBranches_Mixed(t *testing.T) {
	repo := initTestRepo(t)

	baseBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}

	run(t, repo.Path(), "git", "branch", "feature/issue-10", baseBranch)      // legacy
	run(t, repo.Path(), "git", "branch", "madflow/user/issue-10", baseBranch) // new format
	run(t, repo.Path(), "git", "branch", "develop", baseBranch)               // unrelated

	deleted, err := repo.DeleteLegacyBranches()
	if err != nil {
		t.Fatalf("DeleteLegacyBranches returned error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "feature/issue-10" {
		t.Errorf("expected [feature/issue-10] deleted, got %v", deleted)
	}

	// Non-legacy branches must still exist.
	if !repo.BranchExists("madflow/user/issue-10") {
		t.Error("expected madflow/user/issue-10 to still exist")
	}
	if !repo.BranchExists("develop") {
		t.Error("expected develop to still exist")
	}
}

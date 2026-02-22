package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupCleanupRepos creates a bare "remote" repo and a working clone.
// Returns the working Repo and the default branch name (main or master).
func setupCleanupRepos(t *testing.T) (*Repo, string) {
	t.Helper()

	// Create a bare repo to serve as the remote.
	bareDir := t.TempDir()
	run(t, bareDir, "git", "init", "--bare")

	// Clone the bare repo into a working directory.
	workDir := t.TempDir()
	run(t, workDir, "git", "clone", bareDir, ".")
	run(t, workDir, "git", "config", "user.email", "test@test.com")
	run(t, workDir, "git", "config", "user.name", "Test User")

	// Create an initial commit on main and push it.
	readmePath := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "initial commit")

	repo := NewRepo(workDir)

	mainBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatalf("get default branch: %v", err)
	}
	mainBranch = strings.TrimSpace(mainBranch)
	run(t, workDir, "git", "push", "-u", "origin", mainBranch)

	return repo, mainBranch
}

func TestNewBranchCleaner(t *testing.T) {
	repo := NewRepo("/tmp/test")
	protected := []string{"main", "develop"}
	cleaner := NewBranchCleaner(repo, protected, "feature/issue-")

	if cleaner.repo != repo {
		t.Error("repo not set correctly")
	}
	if len(cleaner.protectedBranches) != 2 {
		t.Errorf("expected 2 protected branches, got %d", len(cleaner.protectedBranches))
	}
	if cleaner.featurePrefix != "feature/issue-" {
		t.Errorf("expected featurePrefix 'feature/issue-', got %q", cleaner.featurePrefix)
	}
}

func TestIsProtected(t *testing.T) {
	cleaner := NewBranchCleaner(
		NewRepo("/tmp"),
		[]string{"main", "develop"},
		"feature/",
	)

	cases := []struct {
		branch    string
		protected bool
	}{
		{"main", true},
		{"develop", true},
		{"feature/issue-001", false},
		{"release/v1.0", false},
	}

	for _, tc := range cases {
		got := cleaner.isProtected(tc.branch)
		if got != tc.protected {
			t.Errorf("isProtected(%q) = %v, want %v", tc.branch, got, tc.protected)
		}
	}
}

func TestListMergedRemoteBranches_ParsesOutput(t *testing.T) {
	// Test that feature/issue-001 is identified as a merged branch.
	repo, mainBranch := setupCleanupRepos(t)

	// Create a feature branch, commit, push, merge, and push main.
	run(t, repo.path, "git", "checkout", "-b", "feature/issue-001")
	featFile := filepath.Join(repo.path, "feat.txt")
	if err := os.WriteFile(featFile, []byte("feature\n"), 0644); err != nil {
		t.Fatalf("write feat file: %v", err)
	}
	run(t, repo.path, "git", "add", ".")
	run(t, repo.path, "git", "commit", "-m", "feat: feature")
	run(t, repo.path, "git", "push", "-u", "origin", "feature/issue-001")

	run(t, repo.path, "git", "checkout", mainBranch)
	run(t, repo.path, "git", "merge", "--no-ff", "feature/issue-001", "-m", "Merge feature/issue-001")
	run(t, repo.path, "git", "push", "origin", mainBranch)

	// Fetch to update remote-tracking refs.
	run(t, repo.path, "git", "fetch", "--prune", "origin")

	cleaner := NewBranchCleaner(repo, []string{mainBranch, "develop"}, "feature/issue-")
	branches, err := cleaner.listMergedRemoteBranches(mainBranch)
	if err != nil {
		t.Fatalf("listMergedRemoteBranches: %v", err)
	}

	found := false
	for _, b := range branches {
		if b == "feature/issue-001" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'feature/issue-001' in merged branches, got %v", branches)
	}
}

func TestCleanMergedBranches(t *testing.T) {
	repo, mainBranch := setupCleanupRepos(t)

	// Create a feature branch, add a commit, and push it.
	run(t, repo.path, "git", "checkout", "-b", "feature/issue-001")
	featureFile := filepath.Join(repo.path, "feature.txt")
	if err := os.WriteFile(featureFile, []byte("feature work\n"), 0644); err != nil {
		t.Fatalf("write feature file: %v", err)
	}
	run(t, repo.path, "git", "add", ".")
	run(t, repo.path, "git", "commit", "-m", "feat: add feature")
	run(t, repo.path, "git", "push", "-u", "origin", "feature/issue-001")

	// Merge the feature branch into main (simulate a merged PR).
	run(t, repo.path, "git", "checkout", mainBranch)
	run(t, repo.path, "git", "merge", "--no-ff", "feature/issue-001", "-m", "Merge feature/issue-001")
	run(t, repo.path, "git", "push", "origin", mainBranch)

	// Now the feature branch is merged. Run the cleaner.
	cleaner := NewBranchCleaner(repo, []string{mainBranch, "develop"}, "feature/issue-")
	deleted, err := cleaner.CleanMergedBranches(mainBranch)
	if err != nil {
		t.Fatalf("CleanMergedBranches failed: %v", err)
	}

	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted branch, got %d: %v", len(deleted), deleted)
	}
	if deleted[0] != "feature/issue-001" {
		t.Errorf("expected 'feature/issue-001' deleted, got %q", deleted[0])
	}
}

func TestCleanMergedBranches_SkipsProtected(t *testing.T) {
	repo, mainBranch := setupCleanupRepos(t)

	// Create a develop branch and push it (it's at same commit as main, so merged).
	run(t, repo.path, "git", "checkout", "-b", "develop")
	run(t, repo.path, "git", "push", "-u", "origin", "develop")
	run(t, repo.path, "git", "checkout", mainBranch)

	// Run the cleaner with develop in the protected list.
	cleaner := NewBranchCleaner(repo, []string{mainBranch, "develop"}, "feature/issue-")
	deleted, err := cleaner.CleanMergedBranches(mainBranch)
	if err != nil {
		t.Fatalf("CleanMergedBranches failed: %v", err)
	}

	for _, b := range deleted {
		if b == "develop" {
			t.Errorf("protected branch 'develop' was deleted")
		}
	}
}

func TestCleanMergedBranches_SkipsNonFeature(t *testing.T) {
	repo, mainBranch := setupCleanupRepos(t)

	// Create a hotfix branch (not a feature branch), push and merge it.
	run(t, repo.path, "git", "checkout", "-b", "hotfix/bug-123")
	hotfixFile := filepath.Join(repo.path, "hotfix.txt")
	if err := os.WriteFile(hotfixFile, []byte("hotfix\n"), 0644); err != nil {
		t.Fatalf("write hotfix file: %v", err)
	}
	run(t, repo.path, "git", "add", ".")
	run(t, repo.path, "git", "commit", "-m", "fix: hotfix")
	run(t, repo.path, "git", "push", "-u", "origin", "hotfix/bug-123")

	// Merge into main.
	run(t, repo.path, "git", "checkout", mainBranch)
	run(t, repo.path, "git", "merge", "--no-ff", "hotfix/bug-123", "-m", "Merge hotfix/bug-123")
	run(t, repo.path, "git", "push", "origin", mainBranch)

	// Run cleaner with feature prefix filter – hotfix should not be deleted.
	cleaner := NewBranchCleaner(repo, []string{mainBranch, "develop"}, "feature/issue-")
	deleted, err := cleaner.CleanMergedBranches(mainBranch)
	if err != nil {
		t.Fatalf("CleanMergedBranches failed: %v", err)
	}

	for _, b := range deleted {
		if b == "hotfix/bug-123" {
			t.Errorf("non-feature branch 'hotfix/bug-123' was unexpectedly deleted")
		}
	}
}

func TestCleanMergedBranches_NoPrefix(t *testing.T) {
	repo, mainBranch := setupCleanupRepos(t)

	// When featurePrefix is empty, all non-protected merged branches are deleted.
	run(t, repo.path, "git", "checkout", "-b", "hotfix/bug-456")
	hotfixFile := filepath.Join(repo.path, "hotfix2.txt")
	if err := os.WriteFile(hotfixFile, []byte("hotfix2\n"), 0644); err != nil {
		t.Fatalf("write hotfix file: %v", err)
	}
	run(t, repo.path, "git", "add", ".")
	run(t, repo.path, "git", "commit", "-m", "fix: hotfix2")
	run(t, repo.path, "git", "push", "-u", "origin", "hotfix/bug-456")

	run(t, repo.path, "git", "checkout", mainBranch)
	run(t, repo.path, "git", "merge", "--no-ff", "hotfix/bug-456", "-m", "Merge hotfix/bug-456")
	run(t, repo.path, "git", "push", "origin", mainBranch)

	// No feature prefix: all non-protected merged branches should be deleted.
	cleaner := NewBranchCleaner(repo, []string{mainBranch, "develop"}, "")
	deleted, err := cleaner.CleanMergedBranches(mainBranch)
	if err != nil {
		t.Fatalf("CleanMergedBranches failed: %v", err)
	}

	found := false
	for _, b := range deleted {
		if b == "hotfix/bug-456" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'hotfix/bug-456' to be deleted when no prefix filter is set; got %v", deleted)
	}
}

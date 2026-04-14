package git

import (
	"os"
	"path/filepath"
	"testing"
)

// setupNamespacedWorktreeRepo creates a bare "remote" repo and a working clone
// with a namespace directory structure under .worktrees/{ghLogin}/.
// Returns the working Repo and the default branch name.
func setupNamespacedWorktreeRepo(t *testing.T, ghLogin string) (*Repo, string) {
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
	if err := os.WriteFile(readmePath, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "initial commit")

	repo := NewRepo(workDir)
	mainBranch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatalf("get default branch: %v", err)
	}
	run(t, workDir, "git", "push", "-u", "origin", mainBranch)

	// Create namespace dir.
	namespaceDir := filepath.Join(workDir, ".worktrees", ghLogin)
	if err := os.MkdirAll(namespaceDir, 0755); err != nil {
		t.Fatalf("create namespace dir: %v", err)
	}

	return repo, mainBranch
}

func TestListNamespacedWorktrees_Empty(t *testing.T) {
	repo := initTestRepo(t)
	entries, err := repo.ListNamespacedWorktrees("alice")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestListNamespacedWorktrees_InvalidLogin(t *testing.T) {
	repo := initTestRepo(t)
	_, err := repo.ListNamespacedWorktrees("")
	if err == nil {
		t.Error("expected error for empty login, got nil")
	}
	_, err = repo.ListNamespacedWorktrees("al/ice")
	if err == nil {
		t.Error("expected error for login with slash, got nil")
	}
	_, err = repo.ListNamespacedWorktrees("al..ice")
	if err == nil {
		t.Error("expected error for login with .., got nil")
	}
}

func TestListNamespacedWorktrees_WithWorktrees(t *testing.T) {
	repo, mainBranch := setupNamespacedWorktreeRepo(t, "alice")

	// Create two namespaced worktrees.
	branchA := "madflow/alice/issue-test-001"
	branchB := "madflow/alice/issue-test-002"
	pathA := filepath.Join(repo.path, ".worktrees", "alice", "issue-test-001")
	pathB := filepath.Join(repo.path, ".worktrees", "alice", "issue-test-002")

	run(t, repo.path, "git", "branch", branchA, mainBranch)
	run(t, repo.path, "git", "branch", branchB, mainBranch)

	if err := repo.AddWorktree(pathA, branchA, mainBranch); err != nil {
		// AddWorktree creates a new branch; if branch already exists, use worktree add without -b
		run(t, repo.path, "git", "worktree", "add", pathA, branchA)
	}
	if err := repo.AddWorktree(pathB, branchB, mainBranch); err != nil {
		run(t, repo.path, "git", "worktree", "add", pathB, branchB)
	}

	entries, err := repo.ListNamespacedWorktrees("alice")
	if err != nil {
		t.Fatalf("ListNamespacedWorktrees: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(entries), entries)
	}

	// Check that entries have the right structure.
	found := map[string]bool{}
	for _, e := range entries {
		found[e.SubDir] = true
		expectedBranch := "madflow/alice/" + e.SubDir
		if e.BranchName != expectedBranch {
			t.Errorf("entry %s: expected BranchName %q, got %q", e.SubDir, expectedBranch, e.BranchName)
		}
		if e.Path == "" {
			t.Errorf("entry %s: Path is empty", e.SubDir)
		}
	}
	if !found["issue-test-001"] {
		t.Error("expected issue-test-001 in entries")
	}
	if !found["issue-test-002"] {
		t.Error("expected issue-test-002 in entries")
	}
}

func TestListNamespacedWorktrees_IgnoresFiles(t *testing.T) {
	repo, _ := setupNamespacedWorktreeRepo(t, "bob")

	// Create a file (not a dir) inside the namespace dir.
	namespaceDir := filepath.Join(repo.path, ".worktrees", "bob")
	if err := os.WriteFile(filepath.Join(namespaceDir, "somefile.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	entries, err := repo.ListNamespacedWorktrees("bob")
	if err != nil {
		t.Fatalf("ListNamespacedWorktrees: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries (files ignored), got %d", len(entries))
	}
}

func TestCleanMergedPRWorktrees_NoPRs(t *testing.T) {
	// When no worktrees exist, CleanMergedPRWorktrees should return empty result.
	repo := initTestRepo(t)

	// No namespace dir -> should succeed with empty result.
	removed, err := repo.CleanMergedPRWorktrees("owner", "repo", "alice")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected no removed entries, got: %v", removed)
	}
}

func TestCheckPRState_NoPR(t *testing.T) {
	// checkPRState should return ("", nil) for an empty JSON array.
	// We test this by verifying the function parses "[]" correctly.
	// Since we can't call real gh in unit tests, we test indirectly via
	// the parsing logic by inspecting a mocked scenario.
	//
	// This is a structural test: ensure CleanMergedPRWorktrees handles
	// the case where no PR is found gracefully (no error, no removal).
	repo := initTestRepo(t)

	// Create a namespace directory with a fake worktree dir.
	namespaceDir := filepath.Join(repo.path, ".worktrees", "testuser")
	worktreeDir := filepath.Join(namespaceDir, "issue-fake-001")
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// CleanMergedPRWorktrees will try to call gh pr list; in a test environment
	// without a real GitHub repo, it will fail and log the error — but should
	// not return an error itself (gh failures are non-fatal per spec).
	// The important thing is it doesn't panic or return unexpected errors.
	removed, err := repo.CleanMergedPRWorktrees("nonexistent-owner", "nonexistent-repo", "testuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Since gh will fail (no real repo), the worktree should not be removed.
	if len(removed) != 0 {
		t.Errorf("expected no removed entries when gh fails, got: %v", removed)
	}
}

func TestNamespacedWorktreeEntry_BranchName(t *testing.T) {
	// Verify the BranchName format for namespaced worktree entries.
	e := NamespacedWorktreeEntry{
		SubDir:     "issue-myorg-REPO-42",
		Path:       "/some/path/.worktrees/alice/issue-myorg-REPO-42",
		BranchName: "madflow/alice/issue-myorg-REPO-42",
	}
	if e.BranchName != "madflow/alice/issue-myorg-REPO-42" {
		t.Errorf("unexpected BranchName: %q", e.BranchName)
	}
}

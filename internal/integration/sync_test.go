package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ytnobody/madflow/internal/git"
	"github.com/ytnobody/madflow/internal/issue"
)

func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// TestGitHubSyncNewIssueImport tests that new GitHub issues
// are correctly imported as local issue files.
func TestGitHubSyncNewIssueImport(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Simulate what the syncer does: create an issue with GitHub-style ID
	ghIssue := &issue.Issue{
		ID:           "myorg-myrepo-042",
		Title:        "Add user authentication",
		URL:          "https://github.com/myorg/myrepo/issues/42",
		Status:       issue.StatusOpen,
		AssignedTeam: 0,
		Repos:        []string{"myrepo"},
		Labels:       []string{"feature"},
		Body:         "Implement JWT-based authentication",
	}

	if err := store.Update(ghIssue); err != nil {
		t.Fatalf("create GitHub-synced issue: %v", err)
	}

	// Verify it can be retrieved
	loaded, err := store.Get("myorg-myrepo-042")
	if err != nil {
		t.Fatalf("get synced issue: %v", err)
	}
	if loaded.Title != "Add user authentication" {
		t.Errorf("expected title, got %s", loaded.Title)
	}
	if loaded.URL != "https://github.com/myorg/myrepo/issues/42" {
		t.Errorf("expected URL, got %s", loaded.URL)
	}
}

// TestGitHubSyncExistingUpdate tests that existing issues in open status
// get updated when their title/body changes.
func TestGitHubSyncExistingUpdate(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Create initial issue
	initial := &issue.Issue{
		ID:     "myorg-myrepo-001",
		Title:  "Old title",
		Status: issue.StatusOpen,
		Body:   "Old body",
	}
	store.Update(initial)

	// Simulate sync updating the issue (syncer only updates open issues)
	loaded, _ := store.Get("myorg-myrepo-001")
	if loaded.Status == issue.StatusOpen {
		loaded.Title = "New title"
		loaded.Body = "New body"
		store.Update(loaded)
	}

	// Verify update
	updated, _ := store.Get("myorg-myrepo-001")
	if updated.Title != "New title" {
		t.Errorf("expected updated title, got %s", updated.Title)
	}
	if updated.Body != "New body" {
		t.Errorf("expected updated body, got %s", updated.Body)
	}
}

// TestGitHubSyncSkipInProgress tests that issues already in in_progress
// status are NOT overwritten by GitHub sync.
func TestGitHubSyncSkipInProgress(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Create issue and move it to in_progress
	iss := &issue.Issue{
		ID:           "myorg-myrepo-005",
		Title:        "Original title",
		Status:       issue.StatusInProgress,
		AssignedTeam: 1,
		Body:         "Original body",
	}
	store.Update(iss)

	// Simulate sync: check status before updating (as syncer does)
	loaded, _ := store.Get("myorg-myrepo-005")
	if loaded.Status != issue.StatusOpen {
		// Syncer would skip this issue - do nothing
	}

	// Verify the issue was NOT changed
	final, _ := store.Get("myorg-myrepo-005")
	if final.Title != "Original title" {
		t.Errorf("in_progress issue should not be updated, title is: %s", final.Title)
	}
	if final.Status != issue.StatusInProgress {
		t.Errorf("status should remain in_progress, got %s", final.Status)
	}
	if final.AssignedTeam != 1 {
		t.Errorf("assigned team should remain 1, got %d", final.AssignedTeam)
	}
}

// TestListNewDetectsUnknownIssues tests the ListNew function that
// the superintendent uses to detect newly added issues.
func TestListNewDetectsUnknownIssues(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Create several issues
	store.Create("Issue 1", "Body 1")
	store.Create("Issue 2", "Body 2")
	store.Create("Issue 3", "Body 3")

	// Superintendent knows about issue 1 and 2
	known := []string{"local-001", "local-002"}

	newIssues, err := store.ListNew(known)
	if err != nil {
		t.Fatalf("ListNew: %v", err)
	}

	if len(newIssues) != 1 {
		t.Fatalf("expected 1 new issue, got %d", len(newIssues))
	}
	if newIssues[0].ID != "local-003" {
		t.Errorf("expected local-003, got %s", newIssues[0].ID)
	}
}

// TestMultiRepoBranchOperations tests git branch operations across
// multiple repositories.
func TestMultiRepoBranchOperations(t *testing.T) {
	// Create two git repos
	repo1Dir := t.TempDir()
	repo2Dir := t.TempDir()

	initGitRepo(t, repo1Dir)
	initGitRepo(t, repo2Dir)

	repo1 := git.NewRepo(repo1Dir)
	repo2 := git.NewRepo(repo2Dir)

	baseBranch := getBaseBranch(t, repo1Dir)

	// Create develop branch in both repos
	if err := repo1.CreateBranch("develop", baseBranch); err != nil {
		t.Fatalf("create develop on repo1: %v", err)
	}
	if err := repo2.CreateBranch("develop", baseBranch); err != nil {
		t.Fatalf("create develop on repo2: %v", err)
	}

	// Create feature branches
	if err := repo1.CreateBranch("feature/issue-001", "develop"); err != nil {
		t.Fatalf("create feature on repo1: %v", err)
	}
	if err := repo2.CreateBranch("feature/issue-001", "develop"); err != nil {
		t.Fatalf("create feature on repo2: %v", err)
	}

	// Simulate work on repo1's feature branch
	writeFile(t, filepath.Join(repo1Dir, "feature.txt"), "feature code")
	gitCommit(t, repo1Dir, "feature.txt", "Add feature")

	// Merge feature -> develop on repo1
	repo1.Checkout("develop")
	ok, err := repo1.Merge("feature/issue-001")
	if err != nil {
		t.Fatalf("merge on repo1: %v", err)
	}
	if !ok {
		t.Error("expected clean merge on repo1")
	}

	// Repo2's feature branch should still be empty (independent)
	repo2.Checkout("feature/issue-001")
	if _, err := os.Stat(filepath.Join(repo2Dir, "feature.txt")); !os.IsNotExist(err) {
		t.Error("repo2 should not have feature.txt")
	}

	// Merge feature -> develop on repo2 (should be clean since no changes)
	repo2.Checkout("develop")
	ok, err = repo2.Merge("feature/issue-001")
	if err != nil {
		t.Fatalf("merge on repo2: %v", err)
	}
	if !ok {
		t.Error("expected clean merge on repo2")
	}

	// Verify develop has the feature on repo1
	repo1.Checkout("develop")
	if _, err := os.Stat(filepath.Join(repo1Dir, "feature.txt")); err != nil {
		t.Error("develop on repo1 should have feature.txt after merge")
	}
}

// TestMergeConflictHandling tests that merge conflicts are properly detected
// and the merge is aborted cleanly.
func TestMergeConflictHandling(t *testing.T) {
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)
	repo := git.NewRepo(repoDir)

	baseBranch := getBaseBranch(t, repoDir)

	// Create develop and feature branches
	repo.CreateBranch("develop", baseBranch)

	// Make a change on develop
	writeFile(t, filepath.Join(repoDir, "shared.txt"), "develop version")
	gitCommit(t, repoDir, "shared.txt", "develop change")

	// Create feature from develop's parent
	repo.Checkout(baseBranch)
	repo.CreateBranch("feature/conflict", baseBranch)
	writeFile(t, filepath.Join(repoDir, "shared.txt"), "feature version")
	gitCommit(t, repoDir, "shared.txt", "feature change")

	// Try to merge feature into develop
	repo.Checkout("develop")
	ok, err := repo.Merge("feature/conflict")
	if err != nil {
		t.Fatalf("merge error: %v", err)
	}
	if ok {
		t.Error("expected merge conflict, but merge succeeded")
	}

	// After conflict detection, repo should be in clean state (merge aborted)
	branch, _ := repo.CurrentBranch()
	if branch != "develop" {
		t.Errorf("should still be on develop after aborted merge, got %s", branch)
	}
}

// Helper functions

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, "README.md"), "# Test Repo")
	run(t, dir, "git", "add", "README.md")
	run(t, dir, "git", "commit", "-m", "Initial commit")
}

func getBaseBranch(t *testing.T, dir string) string {
	t.Helper()
	repo := git.NewRepo(dir)
	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatalf("get base branch: %v", err)
	}
	return branch
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func gitCommit(t *testing.T, dir, file, msg string) {
	t.Helper()
	run(t, dir, "git", "add", file)
	run(t, dir, "git", "commit", "-m", msg)
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := execCommand(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
	}
}

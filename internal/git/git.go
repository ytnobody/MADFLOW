package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo represents a git repository for command execution.
type Repo struct {
	path string
}

func NewRepo(path string) *Repo {
	return &Repo{path: path}
}

func (r *Repo) Path() string {
	return r.path
}

// CreateBranch creates a new branch from the given base branch.
func (r *Repo) CreateBranch(name, base string) error {
	if _, err := r.run("checkout", base); err != nil {
		return fmt.Errorf("checkout %s: %w", base, err)
	}
	if _, err := r.run("pull"); err != nil {
		// pull may fail if no remote; continue
	}
	if _, err := r.run("checkout", "-b", name); err != nil {
		return fmt.Errorf("create branch %s: %w", name, err)
	}
	return nil
}

// Merge merges the given branch into the current branch with --no-ff.
// Returns true if successful, false if there was a conflict.
func (r *Repo) Merge(branch string) (bool, error) {
	_, err := r.run("merge", "--no-ff", branch)
	if err != nil {
		// Check if it's a merge conflict
		if isConflict(err) {
			// Abort the merge
			r.run("merge", "--abort")
			return false, nil
		}
		return false, fmt.Errorf("merge %s: %w", branch, err)
	}
	return true, nil
}

// Checkout switches to the given branch.
func (r *Repo) Checkout(branch string) error {
	_, err := r.run("checkout", branch)
	if err != nil {
		return fmt.Errorf("checkout %s: %w", branch, err)
	}
	return nil
}

// DeleteBranch deletes a local branch.
func (r *Repo) DeleteBranch(name string) error {
	_, err := r.run("branch", "-d", name)
	if err != nil {
		return fmt.Errorf("delete branch %s: %w", name, err)
	}
	return nil
}

// DeleteRemoteBranch deletes a remote branch on origin.
// It also removes the local tracking branch if it exists.
func (r *Repo) DeleteRemoteBranch(name string) error {
	if _, err := r.run("push", "origin", "--delete", name); err != nil {
		return fmt.Errorf("delete remote branch %s: %w", name, err)
	}
	// Remove the local branch if it exists (ignore errors).
	if r.BranchExists(name) {
		r.run("branch", "-d", name) //nolint:errcheck
	}
	return nil
}

// CurrentBranch returns the name of the current branch.
func (r *Repo) CurrentBranch() (string, error) {
	out, err := r.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// BranchExists checks if a branch exists.
func (r *Repo) BranchExists(name string) bool {
	_, err := r.run("rev-parse", "--verify", name)
	return err == nil
}

// Pull pulls the latest changes from remote.
func (r *Repo) Pull() error {
	_, err := r.run("pull")
	return err
}

// EnsureBranch ensures the given branch exists.
// If it doesn't exist, it creates it from baseBranch.
func (r *Repo) EnsureBranch(name, base string) error {
	if r.BranchExists(name) {
		return nil
	}
	if _, err := r.run("branch", name, base); err != nil {
		return fmt.Errorf("create branch %s from %s: %w", name, base, err)
	}
	return nil
}

// AddWorktree creates a new git worktree at the given path with a new branch
// based on the specified base branch.
func (r *Repo) AddWorktree(path, newBranch, baseBranch string) error {
	if _, err := r.run("worktree", "add", "-b", newBranch, path, baseBranch); err != nil {
		return fmt.Errorf("add worktree at %s: %w", path, err)
	}
	return nil
}

// RemoveWorktree removes a git worktree.
func (r *Repo) RemoveWorktree(path string) error {
	if _, err := r.run("worktree", "remove", path, "--force"); err != nil {
		return fmt.Errorf("remove worktree %s: %w", path, err)
	}
	return nil
}

// CleanWorktrees removes all worktrees under the .worktrees/ directory
// that match the given prefix (e.g. "team-"). This is used at startup to
// clean up stale worktrees from previous runs.
func (r *Repo) CleanWorktrees(prefix string) (removed []string) {
	worktreeDir := filepath.Join(r.path, ".worktrees")
	entries, err := os.ReadDir(worktreeDir)
	if err != nil {
		return nil // directory doesn't exist; nothing to clean
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if prefix != "" && !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		wtPath := filepath.Join(worktreeDir, entry.Name())
		if err := r.RemoveWorktree(wtPath); err != nil {
			// If git worktree remove fails, try to prune and remove manually.
			r.run("worktree", "prune")
			os.RemoveAll(wtPath)
		}
		removed = append(removed, entry.Name())
	}
	return removed
}

// CleanOrphanedWorktrees removes worktree directories under .worktrees/ that
// start with "team-" but are NOT in the activeTeamDirs set. This catches
// orphaned worktrees from teams that crashed or were not properly cleaned up.
// It also runs "git worktree prune" to clean stale internal references.
func (r *Repo) CleanOrphanedWorktrees(activeTeamDirs map[string]bool) (removed []string) {
	worktreeDir := filepath.Join(r.path, ".worktrees")
	entries, err := os.ReadDir(worktreeDir)
	if err != nil {
		return nil // directory doesn't exist; nothing to clean
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "team-") {
			continue
		}
		if activeTeamDirs[name] {
			continue
		}
		wtPath := filepath.Join(worktreeDir, name)
		if err := r.RemoveWorktree(wtPath); err != nil {
			// If git worktree remove fails, try to prune and remove manually.
			r.run("worktree", "prune")
			os.RemoveAll(wtPath)
		}
		removed = append(removed, name)
	}
	// Always prune stale worktree references at the end.
	r.run("worktree", "prune")
	return removed
}

// ValidateSafeName validates that name is safe to use as a branch name component
// or issue ID in file path operations. It rejects empty strings, strings
// containing ".." (path traversal), path separators ("/" or "\"), and null bytes.
func ValidateSafeName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name %q contains prohibited sequence \"..\"", name)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("name %q contains prohibited path separator", name)
	}
	if strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("name %q contains null byte", name)
	}
	return nil
}

// PrepareWorktree ensures the develop branch exists (creating from main if needed)
// and creates a worktree with a new feature branch based on develop.
// It validates featureBranch to prevent path traversal attacks.
func (r *Repo) PrepareWorktree(path, featureBranch, developBranch, mainBranch string) error {
	if err := ValidateSafeName(featureBranch); err != nil {
		return fmt.Errorf("invalid feature branch name: %w", err)
	}
	if err := r.EnsureBranch(developBranch, mainBranch); err != nil {
		return fmt.Errorf("ensure develop branch: %w", err)
	}
	return r.AddWorktree(path, featureBranch, developBranch)
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.path

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

func isConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "CONFLICT") || strings.Contains(msg, "Automatic merge failed")
}

package git

import (
	"bytes"
	"fmt"
	"os/exec"
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

// AddWorktree creates a worktree at wtPath with a new branch from base.
// If the branch already exists, attaches it instead.
func (r *Repo) AddWorktree(wtPath, branch, base string) error {
	_, err := r.run("worktree", "add", wtPath, "-b", branch, base)
	if err != nil {
		// Branch may already exist; try without -b
		_, err2 := r.run("worktree", "add", wtPath, branch)
		if err2 != nil {
			return fmt.Errorf("add worktree %s: %w", wtPath, err2)
		}
	}
	return nil
}

// RemoveWorktree removes the worktree at wtPath (--force).
func (r *Repo) RemoveWorktree(wtPath string) error {
	_, err := r.run("worktree", "remove", "--force", wtPath)
	if err != nil {
		return fmt.Errorf("remove worktree %s: %w", wtPath, err)
	}
	return nil
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

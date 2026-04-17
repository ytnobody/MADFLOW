package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// NamespacedWorktreeEntry represents a worktree under .worktrees/{ghLogin}/{subDir}.
type NamespacedWorktreeEntry struct {
	// SubDir is the sub-directory name, e.g. "issue-myorg-REPO-42".
	SubDir string
	// Path is the full filesystem path to the worktree directory.
	Path string
	// BranchName is the inferred git branch, e.g. "madflow/{ghLogin}/{subDir}".
	BranchName string
}

// ListNamespacedWorktrees lists all worktrees under .worktrees/{ghLogin}/.
// Returns an empty slice (no error) if the namespace directory does not exist.
// Returns an error if ghLogin fails validation.
func (r *Repo) ListNamespacedWorktrees(ghLogin string) ([]NamespacedWorktreeEntry, error) {
	if err := ValidateSafeName(ghLogin); err != nil {
		return nil, fmt.Errorf("invalid ghLogin: %w", err)
	}
	namespaceDir := filepath.Join(r.path, ".worktrees", ghLogin)
	entries, err := os.ReadDir(namespaceDir)
	if os.IsNotExist(err) {
		return nil, nil // no namespace directory yet; not an error
	}
	if err != nil {
		return nil, fmt.Errorf("read namespace dir %s: %w", namespaceDir, err)
	}

	var result []NamespacedWorktreeEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subDir := entry.Name()
		result = append(result, NamespacedWorktreeEntry{
			SubDir:     subDir,
			Path:       filepath.Join(namespaceDir, subDir),
			BranchName: "madflow/" + ghLogin + "/" + subDir,
		})
	}
	return result, nil
}

// ghPRStateItem represents one element from `gh pr list --json state` output.
type ghPRStateItem struct {
	State string `json:"state"`
}

// CheckPRState checks the state of the most recent PR for the given branch head.
// It calls `gh pr list --head {branchName} --state all --json state --repo {owner}/{repo}`.
// Returns the state in lower-case ("merged", "closed", "open"), or "" if no PR exists.
// Returns an error if the gh CLI call fails or output cannot be parsed.
func CheckPRState(owner, repo, branchName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--head", branchName,
		"--state", "all",
		"--json", "state",
		"--repo", owner+"/"+repo,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh pr list --head %s: %w (stderr: %s)", branchName, err, stderr.String())
	}
	var items []ghPRStateItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		return "", fmt.Errorf("parse gh pr list output for %s: %w", branchName, err)
	}
	if len(items) == 0 {
		return "", nil // no PR found
	}
	return strings.ToLower(items[0].State), nil
}

// CleanMergedPRWorktrees scans .worktrees/{ghLogin}/ for worktrees whose associated
// GitHub PRs have been merged or closed, and removes them.
//
// For each matching worktree the following steps are performed:
//  1. Remove the git worktree with --force (handles uncommitted changes)
//  2. Delete the local branch (git branch -D)
//  3. Delete the remote branch (best-effort; failures are logged, not returned)
//
// Manually-deleted worktrees are recovered by running `git worktree prune` first.
// Returns the list of SubDir values that were successfully removed.
func (r *Repo) CleanMergedPRWorktrees(owner, repo, ghLogin string) ([]string, error) {
	// Prune stale worktree references before scanning, so manually-deleted
	// worktrees don't appear as phantom entries.
	r.run("worktree", "prune") //nolint:errcheck // best-effort

	entries, err := r.ListNamespacedWorktrees(ghLogin)
	if err != nil {
		return nil, fmt.Errorf("list namespaced worktrees: %w", err)
	}

	var removed []string
	for _, entry := range entries {
		state, err := CheckPRState(owner, repo, entry.BranchName)
		if err != nil {
			log.Printf("[worktree-cleanup] skipping %s: failed to check PR state: %v", entry.BranchName, err)
			continue
		}
		if state == "" {
			// No PR exists yet — engineer is still working or hasn't pushed.
			continue
		}
		if state != "merged" && state != "closed" {
			// PR is still open.
			continue
		}

		log.Printf("[worktree-cleanup] removing worktree for branch %s (PR state: %s)", entry.BranchName, state)

		// a. Remove the worktree (--force to handle uncommitted changes).
		if err := r.RemoveWorktree(entry.Path); err != nil {
			// Worktree may have been manually deleted; prune and continue.
			r.run("worktree", "prune") //nolint:errcheck
			log.Printf("[worktree-cleanup] worktree remove failed for %s (may be already gone): %v", entry.Path, err)
		}

		// b. Delete the local branch if it still exists.
		if r.BranchExists(entry.BranchName) {
			if _, err := r.run("branch", "-D", entry.BranchName); err != nil {
				log.Printf("[worktree-cleanup] failed to delete local branch %s: %v", entry.BranchName, err)
			}
		}

		// c. Delete the remote branch — best-effort, don't abort on failure.
		if _, err := r.run("push", "origin", "--delete", entry.BranchName); err != nil {
			log.Printf("[worktree-cleanup] failed to delete remote branch %s (will retry next poll): %v", entry.BranchName, err)
		}

		removed = append(removed, entry.SubDir)
	}

	return removed, nil
}

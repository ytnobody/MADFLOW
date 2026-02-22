package git

import (
	"fmt"
	"log"
	"strings"
)

// BranchCleaner deletes merged feature branches from a repository (local and remote).
type BranchCleaner struct {
	repo              *Repo
	protectedBranches []string
	featurePrefix     string
}

// NewBranchCleaner creates a new BranchCleaner.
// protectedBranches are branches that will never be deleted (e.g. "main", "develop").
// featurePrefix, if non-empty, restricts deletion to branches with that prefix.
func NewBranchCleaner(repo *Repo, protectedBranches []string, featurePrefix string) *BranchCleaner {
	return &BranchCleaner{
		repo:              repo,
		protectedBranches: protectedBranches,
		featurePrefix:     featurePrefix,
	}
}

// CleanMergedBranches fetches the latest remote state, identifies remote branches
// that have been merged into baseBranch, and deletes them from the remote and locally.
// Returns the list of deleted branch names.
func (c *BranchCleaner) CleanMergedBranches(baseBranch string) ([]string, error) {
	// Fetch and prune stale remote-tracking references.
	if _, err := c.repo.run("fetch", "--prune", "origin"); err != nil {
		return nil, fmt.Errorf("fetch --prune: %w", err)
	}

	merged, err := c.listMergedRemoteBranches(baseBranch)
	if err != nil {
		return nil, err
	}

	var deleted []string
	for _, branch := range merged {
		if c.isProtected(branch) {
			continue
		}
		if c.featurePrefix != "" && !strings.HasPrefix(branch, c.featurePrefix) {
			continue
		}

		// Delete the remote branch.
		if _, err := c.repo.run("push", "origin", "--delete", branch); err != nil {
			log.Printf("[branch-cleanup] failed to delete remote branch %s: %v", branch, err)
			continue
		}

		// Delete the local branch if it exists (ignore errors – may not be checked out locally).
		if c.repo.BranchExists(branch) {
			if _, err := c.repo.run("branch", "-d", branch); err != nil {
				log.Printf("[branch-cleanup] failed to delete local branch %s: %v", branch, err)
			}
		}

		log.Printf("[branch-cleanup] deleted merged branch: %s", branch)
		deleted = append(deleted, branch)
	}

	return deleted, nil
}

// listMergedRemoteBranches returns the names of remote branches (without the
// "origin/" prefix) that are already merged into origin/<baseBranch>.
func (c *BranchCleaner) listMergedRemoteBranches(baseBranch string) ([]string, error) {
	out, err := c.repo.run("branch", "-r", "--merged", "origin/"+baseBranch)
	if err != nil {
		return nil, fmt.Errorf("list merged remote branches: %w", err)
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(line)
		// Remote-tracking refs look like "origin/foo" or "  origin/HEAD -> origin/main".
		if strings.Contains(branch, "HEAD") {
			continue
		}
		branch = strings.TrimPrefix(branch, "origin/")
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		branches = append(branches, branch)
	}
	return branches, nil
}

// isProtected reports whether branch is in the protected list.
func (c *BranchCleaner) isProtected(branch string) bool {
	for _, p := range c.protectedBranches {
		if branch == p {
			return true
		}
	}
	return false
}

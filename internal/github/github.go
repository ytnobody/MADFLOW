package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/ytnobody/madflow/internal/issue"
)

// ghIssue represents a GitHub issue from `gh issue list --json`.
type ghIssue struct {
	Number int       `json:"number"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`
	Body   string    `json:"body"`
	Labels []ghLabel `json:"labels"`
}

type ghLabel struct {
	Name string `json:"name"`
}

// Syncer synchronizes GitHub Issues to local issue files.
type Syncer struct {
	store    *issue.Store
	owner    string
	repos    []string
	interval time.Duration
}

// NewSyncer creates a new GitHub issue syncer.
func NewSyncer(store *issue.Store, owner string, repos []string, interval time.Duration) *Syncer {
	return &Syncer{
		store:    store,
		owner:    owner,
		repos:    repos,
		interval: interval,
	}
}

// Run starts the periodic sync loop. Blocks until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context) error {
	log.Printf("[github-sync] started (interval: %v, repos: %v)", s.interval, s.repos)

	// Initial sync
	if err := s.SyncOnce(); err != nil {
		log.Printf("[github-sync] initial sync failed: %v", err)
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[github-sync] stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := s.SyncOnce(); err != nil {
				log.Printf("[github-sync] sync failed: %v", err)
			}
		}
	}
}

// SyncOnce performs a single sync cycle across all configured repos.
func (s *Syncer) SyncOnce() error {
	for _, repo := range s.repos {
		if err := s.syncRepo(repo); err != nil {
			log.Printf("[github-sync] repo %s/%s failed: %v", s.owner, repo, err)
			// Continue with other repos
		}
	}
	return nil
}

func (s *Syncer) syncRepo(repo string) error {
	issues, err := s.fetchIssues(repo)
	if err != nil {
		return err
	}

	for _, gh := range issues {
		localID := formatID(s.owner, repo, gh.Number)

		existing, err := s.store.Get(localID)
		if err != nil {
			// New issue - create it
			newIssue := &issue.Issue{
				ID:           localID,
				Title:        gh.Title,
				URL:          gh.URL,
				Status:       issue.StatusOpen,
				AssignedTeam: 0,
				Repos:        []string{repo},
				Labels:       extractLabels(gh.Labels),
				Body:         gh.Body,
			}
			if err := s.store.Update(newIssue); err != nil {
				log.Printf("[github-sync] create %s failed: %v", localID, err)
			} else {
				log.Printf("[github-sync] imported %s: %s", localID, gh.Title)
			}
			continue
		}

		// Existing issue - only update if still in "open" status
		if existing.Status != issue.StatusOpen {
			continue
		}

		updated := false
		if existing.Title != gh.Title {
			existing.Title = gh.Title
			updated = true
		}
		if existing.Body != gh.Body {
			existing.Body = gh.Body
			updated = true
		}

		if updated {
			if err := s.store.Update(existing); err != nil {
				log.Printf("[github-sync] update %s failed: %v", localID, err)
			} else {
				log.Printf("[github-sync] updated %s", localID)
			}
		}
	}

	return nil
}

func (s *Syncer) fetchIssues(repo string) ([]ghIssue, error) {
	fullRepo := fmt.Sprintf("%s/%s", s.owner, repo)
	cmd := exec.Command("gh", "issue", "list",
		"-R", fullRepo,
		"--state", "open",
		"--json", "number,title,url,body,labels",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue list for %s: %w", fullRepo, err)
	}

	var issues []ghIssue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	return issues, nil
}

func formatID(owner, repo string, number int) string {
	return fmt.Sprintf("%s-%s-%03d", owner, repo, number)
}

func extractLabels(labels []ghLabel) []string {
	result := make([]string, len(labels))
	for i, l := range labels {
		result[i] = l.Name
	}
	return result
}

// FormatID is exported for use by other packages.
func FormatID(owner, repo string, number int) string {
	return formatID(owner, repo, number)
}

// ParseID extracts owner, repo, and number from a GitHub-synced issue ID.
func ParseID(id string) (owner, repo string, number int, err error) {
	// Format: owner-repo-number
	// Find the last dash before the number
	lastDash := strings.LastIndex(id, "-")
	if lastDash < 0 {
		return "", "", 0, fmt.Errorf("invalid github issue id: %s", id)
	}

	numStr := id[lastDash+1:]
	prefix := id[:lastDash]

	_, err = fmt.Sscanf(numStr, "%d", &number)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid number in issue id %s: %w", id, err)
	}

	// Find the first dash to split owner and repo
	firstDash := strings.Index(prefix, "-")
	if firstDash < 0 {
		return "", "", 0, fmt.Errorf("invalid github issue id: %s", id)
	}

	owner = prefix[:firstDash]
	repo = prefix[firstDash+1:]
	return owner, repo, number, nil
}

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

// ghComment represents a comment from `gh api` for issue comments.
type ghComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

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
	store        *issue.Store
	owner        string
	repos        []string
	interval     time.Duration
	idleDetector *IdleDetector // nil = no adaptive behavior
	idleInterval time.Duration // effective only when idleDetector is set
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

// WithIdleDetector attaches an IdleDetector to the Syncer, enabling adaptive polling.
// When the detector reports no active issues, the Syncer uses idleInterval instead
// of the normal interval. Returns the Syncer for method chaining.
func (s *Syncer) WithIdleDetector(d *IdleDetector, idleInterval time.Duration) *Syncer {
	s.idleDetector = d
	s.idleInterval = idleInterval
	return s
}

// currentInterval returns the interval to use for the next sleep, taking idle state into account.
func (s *Syncer) currentInterval() time.Duration {
	if s.idleDetector != nil {
		return s.idleDetector.AdaptInterval(s.interval, s.idleInterval)
	}
	return s.interval
}

// updateIdleState checks the issue store for active issues and updates the IdleDetector.
// An issue is considered active if its status is open or in_progress.
func (s *Syncer) updateIdleState() {
	if s.idleDetector == nil {
		return
	}

	all, err := s.store.List(issue.StatusFilter{})
	if err != nil {
		// On error, assume there might be issues to avoid suppressing polling
		s.idleDetector.SetHasIssues(true)
		return
	}

	for _, iss := range all {
		if iss.Status == issue.StatusOpen || iss.Status == issue.StatusInProgress {
			s.idleDetector.SetHasIssues(true)
			return
		}
	}
	s.idleDetector.SetHasIssues(false)
}

// dormancyCheckInterval is how often the Syncer re-checks dormancy state
// while dormant (no GitHub API calls are made during this wait).
const dormancyCheckInterval = 30 * time.Second

// Run starts the periodic sync loop. Blocks until ctx is cancelled.
// If an IdleDetector is attached (via WithIdleDetector), the sync interval
// automatically increases to idleInterval when no active issues are present,
// and reverts to the normal interval when issues appear.
// When the detector reports dormancy (via IsDormant), all GitHub API calls are
// suspended until Wake() is called on the detector or issues reappear.
func (s *Syncer) Run(ctx context.Context) error {
	log.Printf("[github-sync] started (interval: %v, repos: %v)", s.interval, s.repos)

	// Initial sync
	if err := s.SyncOnce(); err != nil {
		log.Printf("[github-sync] initial sync failed: %v", err)
	}
	s.updateIdleState()

	for {
		// Dormancy check: if dormant, suspend GitHub API calls entirely.
		if s.idleDetector != nil && s.idleDetector.IsDormant() {
			select {
			case <-ctx.Done():
				log.Println("[github-sync] stopped")
				return ctx.Err()
			case <-time.After(dormancyCheckInterval):
				// Re-evaluate dormancy without making any GitHub API calls.
				continue
			}
		}

		interval := s.currentInterval()
		select {
		case <-ctx.Done():
			log.Println("[github-sync] stopped")
			return ctx.Err()
		case <-time.After(interval):
			if err := s.SyncOnce(); err != nil {
				log.Printf("[github-sync] sync failed: %v", err)
			}
			s.updateIdleState()
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
			// Sync comments for new issue
			s.syncComments(repo, gh.Number, localID)
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

		// Sync comments for existing issue
		s.syncComments(repo, gh.Number, localID)
	}

	return nil
}

// syncComments fetches and persists comments for a single issue.
func (s *Syncer) syncComments(repo string, issueNumber int, localID string) {
	comments, err := s.fetchComments(repo, issueNumber)
	if err != nil {
		log.Printf("[github-sync] fetch comments for %s failed: %v", localID, err)
		return
	}
	if len(comments) == 0 {
		return
	}

	iss, err := s.store.Get(localID)
	if err != nil {
		return
	}

	added := 0
	for _, c := range comments {
		createdAt, _ := time.Parse(time.RFC3339, c.CreatedAt)
		updatedAt, _ := time.Parse(time.RFC3339, c.UpdatedAt)
		comment := issue.Comment{
			ID:        c.ID,
			Author:    c.User.Login,
			Body:      c.Body,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}
		if iss.AddComment(comment) {
			added++
		}
	}

	if added > 0 {
		if err := s.store.Update(iss); err != nil {
			log.Printf("[github-sync] save comments for %s failed: %v", localID, err)
		} else {
			log.Printf("[github-sync] added %d comments to %s", added, localID)
		}
	}
}

// fetchComments fetches all comments for a GitHub issue via gh CLI.
func (s *Syncer) fetchComments(repo string, issueNumber int) ([]ghComment, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/issues/%d/comments", s.owner, repo, issueNumber)
	cmd := exec.Command("gh", "api", endpoint, "--paginate")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api comments for %s/%s#%d: %w", s.owner, repo, issueNumber, err)
	}

	if len(out) == 0 {
		return nil, nil
	}

	var comments []ghComment
	if err := json.Unmarshal(out, &comments); err != nil {
		return nil, fmt.Errorf("parse comments: %w", err)
	}
	return comments, nil
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

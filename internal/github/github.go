package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
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
		// Type is the GitHub account type: "User", "Bot", or "Organization".
		Type string `json:"type"`
	} `json:"user"`
}

// isBotUser reports whether a GitHub commenter is a bot.
// A commenter is considered a bot if the GitHub API reports their type as "Bot",
// or if their login name ends with the "[bot]" suffix used by GitHub Apps
// (e.g. "github-actions[bot]", "dependabot[bot]").
func isBotUser(login, userType string) bool {
	return userType == "Bot" || issue.IsBotLogin(login)
}

// CompileBotPatterns compiles a slice of regular-expression strings into
// []*regexp.Regexp.  The first invalid pattern causes an error; all compiled
// patterns up to that point are also returned so callers can decide how to
// handle a partial result.
func CompileBotPatterns(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return compiled, fmt.Errorf("invalid bot_comment_pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// matchesBotPattern reports whether the given comment body matches any of the
// compiled bot-comment patterns.
func matchesBotPattern(body string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(body) {
			return true
		}
	}
	return false
}

// isBot combines account-level and body-pattern-level bot detection.
// It returns true if the commenter is a known bot account (GitHub Apps /
// accounts whose login ends with "[bot]") OR if the comment body matches any
// of the supplied compiled patterns.
func isBot(login, userType, body string, patterns []*regexp.Regexp) bool {
	return isBotUser(login, userType) || matchesBotPattern(body, patterns)
}

// ghIssue represents a GitHub issue from `gh issue list --json` or Events API.
type ghIssue struct {
	Number int       `json:"number"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`
	Body   string    `json:"body"`
	Labels []ghLabel `json:"labels"`
	// User is populated from the GitHub Events API (issue.user.login).
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	// Author is populated from `gh issue list --json author` (author.login).
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

// authorLogin returns the GitHub login of the issue author.
// It checks User.Login first (Events API) and falls back to Author.Login (gh CLI).
func (g *ghIssue) authorLogin() string {
	if g.User.Login != "" {
		return g.User.Login
	}
	return g.Author.Login
}

type ghLabel struct {
	Name string `json:"name"`
}

// Syncer synchronizes GitHub Issues to local issue files.
type Syncer struct {
	store           *issue.Store
	owner           string
	repos           []string
	interval        time.Duration
	idleDetector    *IdleDetector    // nil = no adaptive behavior
	idleInterval    time.Duration    // effective only when idleDetector is set
	authorizedUsers []string         // empty = all users trusted
	botPatterns     []*regexp.Regexp // compiled bot comment patterns; nil = no pattern check
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

// WithAuthorizedUsers restricts the Syncer to only process issues created by
// the specified GitHub users. An empty slice (the default) allows all users.
func (s *Syncer) WithAuthorizedUsers(users []string) *Syncer {
	s.authorizedUsers = users
	return s
}

// WithBotCommentPatterns attaches pre-compiled bot comment patterns to the
// Syncer.  When a fetched comment body matches any of these patterns, the
// comment is stored with IsBot=true, allowing consumers (e.g. the orchestrator)
// to distinguish bot-generated status updates from human discussion comments.
// Use CompileBotPatterns to compile the raw string patterns from the config.
func (s *Syncer) WithBotCommentPatterns(patterns []*regexp.Regexp) *Syncer {
	s.botPatterns = patterns
	return s
}

// isAuthorized returns true if the given GitHub login is authorized.
// When authorizedUsers is empty, all users are authorized.
func isAuthorized(login string, authorizedUsers []string) bool {
	if len(authorizedUsers) == 0 {
		return true
	}
	for _, u := range authorizedUsers {
		if u == login {
			return true
		}
	}
	return false
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

	// Build a set of open issue IDs from GitHub for stale-close detection.
	openIDs := make(map[string]struct{}, len(issues))

	for _, gh := range issues {
		localID := formatID(s.owner, repo, gh.Number)
		openIDs[localID] = struct{}{}

		// Determine authorization - unauthorized issues are stored with PendingApproval=true
		// instead of being skipped, so that an authorized user can later approve them.
		login := gh.authorLogin()
		pendingApproval := !isAuthorized(login, s.authorizedUsers)
		if pendingApproval {
			log.Printf("[github-sync] issue %s by unauthorized user %q - stored as pending approval", localID, login)
		}

		existing, err := s.store.Get(localID)
		if err != nil {
			// New issue - create it
			newIssue := &issue.Issue{
				ID:              localID,
				Title:           gh.Title,
				URL:             gh.URL,
				Status:          issue.StatusOpen,
				AssignedTeam:    0,
				PendingApproval: pendingApproval,
				Repos:           []string{repo},
				Labels:          extractLabels(gh.Labels),
				Body:            gh.Body,
			}
			if err := s.store.Update(newIssue); err != nil {
				log.Printf("[github-sync] create %s failed: %v", localID, err)
			} else {
				log.Printf("[github-sync] imported %s: %s", localID, gh.Title)
			}
			// Sync comments for new issue (may contain /approve)
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

	// Close local issues that are no longer open on GitHub.
	s.closeStaleIssues(repo, openIDs)

	return nil
}

// closeStaleIssues marks local open/in_progress issues as closed if they no
// longer appear in the GitHub open-issue list for the given repo.
func (s *Syncer) closeStaleIssues(repo string, openIDs map[string]struct{}) {
	allLocal, err := s.store.List(issue.StatusFilter{})
	if err != nil {
		log.Printf("[github-sync] closeStaleIssues: list: %v", err)
		return
	}
	for _, iss := range allLocal {
		// Skip local-only issues (not from GitHub).
		if iss.URL == "" {
			continue
		}
		// Skip issues not belonging to this repo.
		if !belongsToRepo(iss, repo) {
			continue
		}
		// Skip already closed/resolved issues.
		if iss.Status == issue.StatusClosed || iss.Status == issue.StatusResolved {
			continue
		}
		// Skip issues that are still open on GitHub.
		if _, ok := openIDs[iss.ID]; ok {
			continue
		}
		// Issue was closed on GitHub — update local status.
		iss.Status = issue.StatusClosed
		if err := s.store.Update(iss); err != nil {
			log.Printf("[github-sync] closeStaleIssues: update %s failed: %v", iss.ID, err)
		} else {
			log.Printf("[github-sync] closed %s (no longer open on GitHub)", iss.ID)
		}
	}
}

// belongsToRepo reports whether the issue belongs to the given repo.
// It checks the Repos field first, then falls back to checking the URL.
func belongsToRepo(iss *issue.Issue, repo string) bool {
	for _, r := range iss.Repos {
		if r == repo {
			return true
		}
	}
	// Fallback: check if the URL contains the repo name.
	if iss.URL != "" && strings.Contains(iss.URL, "/"+repo+"/") {
		return true
	}
	return false
}

// syncComments fetches and persists comments for a single issue.
// If the issue has PendingApproval=true and an authorized user's comment contains
// "/approve", the PendingApproval flag is cleared.
func (s *Syncer) syncComments(repo string, issueNumber int, localID string) {
	comments, err := s.fetchComments(repo, issueNumber)
	if err != nil {
		log.Printf("[github-sync] fetch comments for %s failed: %v", localID, err)
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
			IsBot:     isBot(c.User.Login, c.User.Type, c.Body, s.botPatterns),
		}
		if iss.AddComment(comment) {
			added++
		}
	}

	// Check for /approve from an authorized user to clear PendingApproval.
	approvalChanged := false
	if iss.PendingApproval {
		for _, c := range iss.Comments {
			if isAuthorized(c.Author, s.authorizedUsers) && strings.Contains(strings.ToLower(c.Body), "/approve") {
				iss.PendingApproval = false
				approvalChanged = true
				log.Printf("[github-sync] issue %s approved by %s", localID, c.Author)
				break
			}
		}
	}

	if added > 0 || approvalChanged {
		if err := s.store.Update(iss); err != nil {
			log.Printf("[github-sync] save comments for %s failed: %v", localID, err)
		} else if added > 0 {
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
		"--json", "number,title,url,body,labels,author",
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

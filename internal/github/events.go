package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ytnobody/madflow/internal/issue"
)

const maxSeenEvents = 1000
const maxRetries = 2
const retryDelay = 5 * time.Second

// EventType represents the type of a GitHub event.
type EventType string

const (
	EventTypeIssues       EventType = "IssuesEvent"
	EventTypeIssueComment EventType = "IssueCommentEvent"
)

// EventCallback is invoked when an event is processed.
// eventType is the GitHub event type, issueID is the local issue ID,
// and comment is non-nil only for IssueCommentEvent.
type EventCallback func(eventType EventType, issueID string, comment *issue.Comment)

// ghEvent represents a single event from the GitHub Events API.
type ghEvent struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ghEventPayloadIssue is the payload for IssuesEvent.
type ghEventPayloadIssue struct {
	Action string  `json:"action"`
	Issue  ghIssue `json:"issue"`
}

// ghEventPayloadComment is the payload for IssueCommentEvent.
type ghEventPayloadComment struct {
	Action  string         `json:"action"`
	Issue   ghIssue        `json:"issue"`
	Comment ghEventComment `json:"comment"`
}

// ghEventComment represents a comment within the Events API payload.
type ghEventComment struct {
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

// EventWatcher polls the GitHub Events API using ETag-based conditional requests.
type EventWatcher struct {
	store    *issue.Store
	owner    string
	repos    []string
	interval time.Duration
	callback EventCallback

	idleDetector    *IdleDetector // nil = no adaptive behavior
	idleInterval    time.Duration // effective only when idleDetector is set
	authorizedUsers []string      // empty = all users trusted

	mu         sync.Mutex
	seenEvents map[string]struct{}
}

// NewEventWatcher creates a new EventWatcher.
func NewEventWatcher(store *issue.Store, owner string, repos []string, interval time.Duration, cb EventCallback) *EventWatcher {
	return &EventWatcher{
		store:      store,
		owner:      owner,
		repos:      repos,
		interval:   interval,
		callback:   cb,
		seenEvents: make(map[string]struct{}),
	}
}

// WithAuthorizedUsers restricts the EventWatcher to only process events from
// the specified GitHub users. An empty slice (the default) allows all users.
func (w *EventWatcher) WithAuthorizedUsers(users []string) *EventWatcher {
	w.authorizedUsers = users
	return w
}

// WithIdleDetector attaches an IdleDetector to the EventWatcher, enabling adaptive polling.
// When the detector reports no active issues, the watcher uses idleInterval instead
// of the normal interval. Returns the EventWatcher for method chaining.
func (w *EventWatcher) WithIdleDetector(d *IdleDetector, idleInterval time.Duration) *EventWatcher {
	w.idleDetector = d
	w.idleInterval = idleInterval
	return w
}

// currentInterval returns the interval to use for the next sleep, taking idle state into account.
func (w *EventWatcher) currentInterval() time.Duration {
	if w.idleDetector != nil {
		return w.idleDetector.AdaptInterval(w.interval, w.idleInterval)
	}
	return w.interval
}

// Run starts the event polling loop. Blocks until ctx is cancelled.
// If an IdleDetector is attached (via WithIdleDetector), the poll interval
// automatically increases to idleInterval when no active issues are present,
// and reverts to the normal interval when an issue event is detected.
// When the detector reports dormancy (via IsDormant), all GitHub API calls are
// suspended until Wake() is called on the detector or issues reappear.
func (w *EventWatcher) Run(ctx context.Context) error {
	log.Printf("[event-watcher] started (interval: %v, repos: %v)", w.interval, w.repos)

	// etags tracks per-repo ETag for conditional requests
	etags := make(map[string]string)

	// Initial poll
	for _, repo := range w.repos {
		etags[repo] = w.pollRepo(repo, etags[repo])
	}

	for {
		// Dormancy check: if dormant, suspend GitHub API calls entirely.
		if w.idleDetector != nil && w.idleDetector.IsDormant() {
			select {
			case <-ctx.Done():
				log.Println("[event-watcher] stopped")
				return ctx.Err()
			case <-time.After(dormancyCheckInterval):
				// Re-evaluate dormancy without making any GitHub API calls.
				continue
			}
		}

		interval := w.currentInterval()
		select {
		case <-ctx.Done():
			log.Println("[event-watcher] stopped")
			return ctx.Err()
		case <-time.After(interval):
			for _, repo := range w.repos {
				etags[repo] = w.pollRepo(repo, etags[repo])
			}
		}
	}
}

// pollRepo fetches events for a single repo with retry, returns updated ETag.
func (w *EventWatcher) pollRepo(repo, etag string) string {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[event-watcher] retry %d/%d for %s/%s", attempt, maxRetries, w.owner, repo)
			time.Sleep(retryDelay * time.Duration(attempt))
		}

		events, newETag, err := w.fetchEvents(repo, etag)
		if err != nil {
			lastErr = err
			continue
		}

		// Success
		for i := len(events) - 1; i >= 0; i-- {
			w.processEvent(repo, events[i])
		}
		return newETag
	}

	log.Printf("[event-watcher] fetch %s/%s events failed after %d attempts: %v", w.owner, repo, maxRetries+1, lastErr)
	return etag
}

// fetchEvents calls the GitHub Events API via gh CLI.
// Returns events, new ETag, and error.
func (w *EventWatcher) fetchEvents(repo, etag string) ([]ghEvent, string, error) {
	fullRepo := fmt.Sprintf("repos/%s/%s/events", w.owner, repo)

	args := []string{"api", fullRepo, "--include"}
	if etag != "" {
		args = append(args, "-H", fmt.Sprintf("If-None-Match: %s", etag))
	}

	cmd := exec.Command("gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()

	if err != nil {
		// Check if it's a 304 Not Modified
		// gh api exits with code 0 for 304 when using --include,
		// but some versions may exit with non-zero.
		// Better heuristic: check stderr for clues
		stderrStr := strings.TrimSpace(stderr.String())

		if len(out) == 0 && stderrStr == "" {
			// Likely 304 Not Modified (no output at all)
			return nil, etag, nil
		}

		// If there IS output despite the error, try to parse status code
		if len(out) > 0 {
			statusCode, newETag, _ := ParseGHResponseWithStatus(string(out))
			if statusCode == 304 {
				if newETag != "" {
					return nil, newETag, nil
				}
				return nil, etag, nil
			}
		}

		// Real error
		errMsg := fmt.Sprintf("gh api events for %s/%s: %v", w.owner, repo, err)
		if stderrStr != "" {
			errMsg += " (stderr: " + stderrStr + ")"
		}
		return nil, etag, fmt.Errorf("%s", errMsg)
	}

	// Command succeeded (exit 0)
	statusCode, newETag, body := ParseGHResponseWithStatus(string(out))
	if newETag == "" {
		newETag = etag
	}

	// Handle non-200 status codes
	if statusCode == 304 {
		return nil, newETag, nil
	}
	if statusCode >= 400 {
		log.Printf("[event-watcher] HTTP %d for %s/%s, body: %.200s", statusCode, w.owner, repo, body)
		return nil, newETag, fmt.Errorf("HTTP %d from GitHub API for %s/%s", statusCode, w.owner, repo)
	}

	if body == "" {
		return nil, newETag, nil
	}

	// Validate body looks like JSON before parsing
	if !strings.HasPrefix(body, "[") && !strings.HasPrefix(body, "{") {
		return nil, newETag, fmt.Errorf("unexpected response body (not JSON): %.100s", body)
	}

	var events []ghEvent
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		return nil, newETag, fmt.Errorf("parse events: %w (body prefix: %.100s)", err, body)
	}

	return events, newETag, nil
}

// ParseGHResponseWithStatus splits the --include output into status code, ETag header, and JSON body.
// Returns HTTP status code (e.g. 200, 304, 403), ETag, and body.
func ParseGHResponseWithStatus(raw string) (statusCode int, etag, body string) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.SplitN(normalized, "\n\n", 2)

	if len(parts) == 2 {
		headerSection := parts[0]
		body = strings.TrimSpace(parts[1])

		// Parse status code from first line: "HTTP/2.0 200 OK"
		lines := strings.Split(headerSection, "\n")
		if len(lines) > 0 {
			statusCode = parseHTTPStatusCode(lines[0])
		}

		// Extract ETag
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(trimmed), "etag:") {
				etag = strings.TrimSpace(trimmed[5:])
				break
			}
		}
	} else {
		// No header-body separator found
		// Check if the output starts with "HTTP/" (headers only, no body)
		trimmed := strings.TrimSpace(normalized)
		if strings.HasPrefix(trimmed, "HTTP/") {
			lines := strings.Split(trimmed, "\n")
			if len(lines) > 0 {
				statusCode = parseHTTPStatusCode(lines[0])
			}
			// No body
			body = ""
		} else {
			// Raw JSON (no headers)
			body = trimmed
		}
	}
	return statusCode, etag, body
}

// parseHTTPStatusCode extracts the HTTP status code from a status line.
// e.g. "HTTP/2.0 200 OK" or "HTTP/1.1 403 Forbidden"
func parseHTTPStatusCode(statusLine string) int {
	parts := strings.Fields(statusLine)
	if len(parts) >= 2 {
		code, err := strconv.Atoi(parts[1])
		if err == nil {
			return code
		}
	}
	return 0
}

// ParseGHResponse splits the --include output into ETag header and JSON body.
// Deprecated: Use ParseGHResponseWithStatus for full status code support.
func ParseGHResponse(raw string) (etag, body string) {
	_, etag, body = ParseGHResponseWithStatus(raw)
	return etag, body
}

// processEvent handles a single GitHub event.
func (w *EventWatcher) processEvent(repo string, ev ghEvent) {
	if w.markSeen(ev.ID) {
		return // already processed
	}

	switch EventType(ev.Type) {
	case EventTypeIssues:
		w.handleIssuesEvent(repo, ev)
	case EventTypeIssueComment:
		w.handleIssueCommentEvent(repo, ev)
	}
}

// handleIssuesEvent processes an IssuesEvent (opened, edited, etc.).
func (w *EventWatcher) handleIssuesEvent(repo string, ev ghEvent) {
	var payload ghEventPayloadIssue
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		log.Printf("[event-watcher] parse IssuesEvent payload: %v", err)
		return
	}

	if payload.Action != "opened" && payload.Action != "edited" {
		return
	}

	// Determine authorization - unauthorized issues are stored with PendingApproval=true
	// instead of being skipped, so that an authorized user can later approve them.
	login := payload.Issue.authorLogin()
	pendingApproval := !isAuthorized(login, w.authorizedUsers)
	if pendingApproval {
		log.Printf("[event-watcher] issue #%d by unauthorized user %q - stored as pending approval", payload.Issue.Number, login)
	}

	localID := FormatID(w.owner, repo, payload.Issue.Number)
	existing, err := w.store.Get(localID)
	if err != nil {
		// New issue
		newIssue := &issue.Issue{
			ID:              localID,
			Title:           payload.Issue.Title,
			URL:             payload.Issue.URL,
			Status:          issue.StatusOpen,
			AssignedTeam:    0,
			PendingApproval: pendingApproval,
			Repos:           []string{repo},
			Labels:          extractLabels(payload.Issue.Labels),
			Body:            payload.Issue.Body,
		}
		if err := w.store.Update(newIssue); err != nil {
			log.Printf("[event-watcher] create %s failed: %v", localID, err)
			return
		}
		log.Printf("[event-watcher] imported %s: %s", localID, payload.Issue.Title)
	} else {
		// Update existing issue if still open
		if existing.Status != issue.StatusOpen {
			return
		}
		updated := false
		if existing.Title != payload.Issue.Title {
			existing.Title = payload.Issue.Title
			updated = true
		}
		if existing.Body != payload.Issue.Body {
			existing.Body = payload.Issue.Body
			updated = true
		}
		if updated {
			if err := w.store.Update(existing); err != nil {
				log.Printf("[event-watcher] update %s failed: %v", localID, err)
				return
			}
			log.Printf("[event-watcher] updated %s", localID)
		}
	}

	// Notify idle detector that there is an active issue
	if w.idleDetector != nil {
		w.idleDetector.SetHasIssues(true)
	}

	if w.callback != nil {
		w.callback(EventTypeIssues, localID, nil)
	}
}

// handleIssueCommentEvent processes an IssueCommentEvent.
func (w *EventWatcher) handleIssueCommentEvent(repo string, ev ghEvent) {
	var payload ghEventPayloadComment
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		log.Printf("[event-watcher] parse IssueCommentEvent payload: %v", err)
		return
	}

	if payload.Action != "created" {
		return
	}

	commentLogin := payload.Comment.User.Login

	// Only process comments from authorized users.
	if !isAuthorized(commentLogin, w.authorizedUsers) {
		log.Printf("[event-watcher] skipping IssueCommentEvent comment #%d by unauthorized user %q", payload.Comment.ID, commentLogin)
		return
	}

	localID := FormatID(w.owner, repo, payload.Issue.Number)
	existing, err := w.store.Get(localID)
	if err != nil {
		// Issue not yet synced; skip comment
		log.Printf("[event-watcher] comment for unknown issue %s, skipping", localID)
		return
	}

	createdAt, _ := time.Parse(time.RFC3339, payload.Comment.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, payload.Comment.UpdatedAt)

	comment := issue.Comment{
		ID:        payload.Comment.ID,
		Author:    payload.Comment.User.Login,
		Body:      payload.Comment.Body,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		IsBot:     isBotUser(payload.Comment.User.Login, payload.Comment.User.Type),
	}

	changed := existing.AddComment(comment)

	// If an authorized user posts /approve on a pending-approval issue, clear the flag.
	if existing.PendingApproval && strings.Contains(strings.ToLower(payload.Comment.Body), "/approve") {
		existing.PendingApproval = false
		changed = true
		log.Printf("[event-watcher] issue %s approved by %s via comment #%d", localID, commentLogin, payload.Comment.ID)
	}

	if changed {
		if err := w.store.Update(existing); err != nil {
			log.Printf("[event-watcher] save comment on %s failed: %v", localID, err)
			return
		}
		log.Printf("[event-watcher] comment #%d processed for %s", comment.ID, localID)

		if w.callback != nil {
			w.callback(EventTypeIssueComment, localID, &comment)
		}
	}
}

// markSeen returns true if the event was already seen, false if newly added.
func (w *EventWatcher) markSeen(eventID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, ok := w.seenEvents[eventID]; ok {
		return true
	}

	// Auto-clear when limit is exceeded
	if len(w.seenEvents) >= maxSeenEvents {
		w.seenEvents = make(map[string]struct{})
	}

	w.seenEvents[eventID] = struct{}{}
	return false
}

// SeenCount returns the number of tracked event IDs (for testing).
func (w *EventWatcher) SeenCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.seenEvents)
}

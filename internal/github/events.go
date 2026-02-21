package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ytnobody/madflow/internal/issue"
)

const maxSeenEvents = 1000

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
	Action  string          `json:"action"`
	Issue   ghIssue         `json:"issue"`
	Comment ghEventComment  `json:"comment"`
}

// ghEventComment represents a comment within the Events API payload.
type ghEventComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// EventWatcher polls the GitHub Events API using ETag-based conditional requests.
type EventWatcher struct {
	store    *issue.Store
	owner    string
	repos    []string
	interval time.Duration
	callback EventCallback

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

// Run starts the event polling loop. Blocks until ctx is cancelled.
func (w *EventWatcher) Run(ctx context.Context) error {
	log.Printf("[event-watcher] started (interval: %v, repos: %v)", w.interval, w.repos)

	// etags tracks per-repo ETag for conditional requests
	etags := make(map[string]string)

	// Initial poll
	for _, repo := range w.repos {
		etags[repo] = w.pollRepo(repo, etags[repo])
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[event-watcher] stopped")
			return ctx.Err()
		case <-ticker.C:
			for _, repo := range w.repos {
				etags[repo] = w.pollRepo(repo, etags[repo])
			}
		}
	}
}

// pollRepo fetches events for a single repo, returns updated ETag.
func (w *EventWatcher) pollRepo(repo, etag string) string {
	events, newETag, err := w.fetchEvents(repo, etag)
	if err != nil {
		log.Printf("[event-watcher] fetch %s/%s events failed: %v", w.owner, repo, err)
		return etag
	}

	// Process events in reverse order (oldest first)
	for i := len(events) - 1; i >= 0; i-- {
		w.processEvent(repo, events[i])
	}

	return newETag
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
	out, err := cmd.Output()
	if err != nil {
		// 304 Not Modified comes back as an exit error with empty output
		if len(out) == 0 {
			return nil, etag, nil
		}
		return nil, etag, fmt.Errorf("gh api events for %s/%s: %w", w.owner, repo, err)
	}

	newETag, body := ParseGHResponse(string(out))
	if newETag == "" {
		newETag = etag
	}

	if body == "" {
		return nil, newETag, nil
	}

	var events []ghEvent
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		return nil, newETag, fmt.Errorf("parse events: %w", err)
	}

	return events, newETag, nil
}

// ParseGHResponse splits the --include output into ETag header and JSON body.
func ParseGHResponse(raw string) (etag, body string) {
	// Normalize \r\n to \n for consistent parsing
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")

	// gh --include prepends HTTP headers separated by a blank line from the body
	parts := strings.SplitN(normalized, "\n\n", 2)
	if len(parts) == 2 {
		body = strings.TrimSpace(parts[1])
		// Extract ETag from headers
		for _, line := range strings.Split(parts[0], "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(trimmed), "etag:") {
				etag = strings.TrimSpace(trimmed[5:])
				break
			}
		}
	} else {
		// No headers, entire output is body
		body = strings.TrimSpace(normalized)
	}
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

	localID := FormatID(w.owner, repo, payload.Issue.Number)
	existing, err := w.store.Get(localID)
	if err != nil {
		// New issue
		newIssue := &issue.Issue{
			ID:           localID,
			Title:        payload.Issue.Title,
			URL:          payload.Issue.URL,
			Status:       issue.StatusOpen,
			AssignedTeam: 0,
			Repos:        []string{repo},
			Labels:       extractLabels(payload.Issue.Labels),
			Body:         payload.Issue.Body,
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
	}

	if existing.AddComment(comment) {
		if err := w.store.Update(existing); err != nil {
			log.Printf("[event-watcher] save comment on %s failed: %v", localID, err)
			return
		}
		log.Printf("[event-watcher] comment #%d added to %s", comment.ID, localID)
	}

	if w.callback != nil {
		w.callback(EventTypeIssueComment, localID, &comment)
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

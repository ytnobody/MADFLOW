package github

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ytnobody/madflow/internal/issue"
)

func TestParseGHResponse(t *testing.T) {
	raw := "HTTP/2.0 200 OK\nETag: \"abc123\"\nContent-Type: application/json\n\n[{\"id\":\"1\",\"type\":\"IssuesEvent\"}]"
	etag, body := ParseGHResponse(raw)

	if etag != "\"abc123\"" {
		t.Errorf("expected etag '\"abc123\"', got %q", etag)
	}
	if body != "[{\"id\":\"1\",\"type\":\"IssuesEvent\"}]" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestParseGHResponseNoHeaders(t *testing.T) {
	raw := `[{"id":"1","type":"IssuesEvent"}]`
	etag, body := ParseGHResponse(raw)
	if etag != "" {
		t.Errorf("expected empty etag, got %q", etag)
	}
	if body != raw {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestParseGHResponseETagCaseInsensitive(t *testing.T) {
	raw := "HTTP/2.0 200 OK\netag: W/\"xyz\"\n\n[]"
	etag, body := ParseGHResponse(raw)
	if etag != "W/\"xyz\"" {
		t.Errorf("expected etag W/\"xyz\", got %q", etag)
	}
	if body != "[]" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestParseGHResponseCRLF(t *testing.T) {
	// HTTP headers with \r\n line endings (standard HTTP format)
	raw := "HTTP/2.0 200 OK\r\nEtag: \"crlf-test\"\r\nContent-Type: application/json\r\n\r\n[{\"id\":\"1\"}]"
	etag, body := ParseGHResponse(raw)
	if etag != "\"crlf-test\"" {
		t.Errorf("expected etag '\"crlf-test\"', got %q", etag)
	}
	if body != "[{\"id\":\"1\"}]" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestProcessIssuesEvent(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	var gotEventType EventType
	var gotIssueID string

	cb := func(et EventType, id string, c *issue.Comment) {
		gotEventType = et
		gotIssueID = id
	}

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, cb)

	payload := ghEventPayloadIssue{
		Action: "opened",
		Issue: ghIssue{
			Number: 1,
			Title:  "Test Issue",
			URL:    "https://github.com/owner/repo/issues/1",
			Body:   "Issue body",
			Labels: []ghLabel{{Name: "bug"}},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{
		ID:      "evt-1",
		Type:    "IssuesEvent",
		Payload: payloadBytes,
	}

	w.processEvent("repo", ev)

	if gotEventType != EventTypeIssues {
		t.Errorf("expected IssuesEvent callback, got %s", gotEventType)
	}
	if gotIssueID != "owner-repo-001" {
		t.Errorf("expected issue ID owner-repo-001, got %s", gotIssueID)
	}

	// Verify issue was created in store
	iss, err := store.Get("owner-repo-001")
	if err != nil {
		t.Fatalf("expected issue in store: %v", err)
	}
	if iss.Title != "Test Issue" {
		t.Errorf("expected title 'Test Issue', got %s", iss.Title)
	}
}

func TestProcessIssueCommentEvent(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Pre-create the issue
	iss := &issue.Issue{
		ID:     "owner-repo-001",
		Title:  "Test",
		Status: issue.StatusOpen,
	}
	store.Update(iss)

	var gotComment *issue.Comment
	cb := func(et EventType, id string, c *issue.Comment) {
		gotComment = c
	}

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, cb)

	payload := ghEventPayloadComment{
		Action: "created",
		Issue:  ghIssue{Number: 1},
		Comment: ghEventComment{
			ID:        42,
			Body:      "Nice work!",
			CreatedAt: "2026-02-21T10:00:00Z",
			UpdatedAt: "2026-02-21T10:00:00Z",
		},
	}
	payload.Comment.User.Login = "alice"
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{
		ID:      "evt-2",
		Type:    "IssueCommentEvent",
		Payload: payloadBytes,
	}

	w.processEvent("repo", ev)

	if gotComment == nil {
		t.Fatal("expected comment callback")
	}
	if gotComment.Author != "alice" {
		t.Errorf("expected author alice, got %s", gotComment.Author)
	}
	if gotComment.Body != "Nice work!" {
		t.Errorf("expected body 'Nice work!', got %s", gotComment.Body)
	}

	// Verify comment was persisted
	updated, _ := store.Get("owner-repo-001")
	if len(updated.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(updated.Comments))
	}
	if updated.Comments[0].ID != 42 {
		t.Errorf("expected comment ID 42, got %d", updated.Comments[0].ID)
	}
}

func TestEventDedup(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	callCount := 0
	cb := func(et EventType, id string, c *issue.Comment) {
		callCount++
	}

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, cb)

	payload := ghEventPayloadIssue{
		Action: "opened",
		Issue: ghIssue{
			Number: 1,
			Title:  "Test",
			Body:   "Body",
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{
		ID:      "evt-dup",
		Type:    "IssuesEvent",
		Payload: payloadBytes,
	}

	// Process same event twice
	w.processEvent("repo", ev)
	w.processEvent("repo", ev)

	if callCount != 1 {
		t.Errorf("expected callback called once (dedup), got %d", callCount)
	}
}

func TestSeenEventsAutoClean(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil)

	// Fill up seen events to the limit
	for i := 0; i < maxSeenEvents; i++ {
		w.markSeen(FormatID("x", "y", i))
	}

	if w.SeenCount() != maxSeenEvents {
		t.Fatalf("expected %d seen events, got %d", maxSeenEvents, w.SeenCount())
	}

	// Adding one more should trigger a clear
	w.markSeen("trigger-clean")
	if w.SeenCount() != 1 {
		t.Errorf("expected 1 after auto-clean, got %d", w.SeenCount())
	}
}

func TestIgnoredIssueActions(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	callCount := 0
	cb := func(et EventType, id string, c *issue.Comment) {
		callCount++
	}

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, cb)

	// "closed" action should be ignored
	payload := ghEventPayloadIssue{
		Action: "closed",
		Issue:  ghIssue{Number: 1, Title: "Test", Body: "Body"},
	}
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{ID: "evt-closed", Type: "IssuesEvent", Payload: payloadBytes}
	w.processEvent("repo", ev)

	if callCount != 0 {
		t.Errorf("expected 0 callbacks for ignored action, got %d", callCount)
	}
}

func TestIgnoredCommentActions(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Pre-create issue
	store.Update(&issue.Issue{ID: "owner-repo-001", Title: "Test", Status: issue.StatusOpen})

	callCount := 0
	cb := func(et EventType, id string, c *issue.Comment) {
		callCount++
	}

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, cb)

	// "deleted" action should be ignored
	payload := ghEventPayloadComment{
		Action:  "deleted",
		Issue:   ghIssue{Number: 1},
		Comment: ghEventComment{ID: 1, Body: "Deleted"},
	}
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{ID: "evt-del", Type: "IssueCommentEvent", Payload: payloadBytes}
	w.processEvent("repo", ev)

	if callCount != 0 {
		t.Errorf("expected 0 callbacks for deleted comment, got %d", callCount)
	}
}

func TestNilCallback(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// nil callback should not panic
	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil)

	payload := ghEventPayloadIssue{
		Action: "opened",
		Issue:  ghIssue{Number: 1, Title: "Test", Body: "Body"},
	}
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{ID: "evt-nil", Type: "IssuesEvent", Payload: payloadBytes}
	w.processEvent("repo", ev) // should not panic
}

// --- ParseGHResponseWithStatus tests ---

func TestParseGHResponseWithStatus_Normal(t *testing.T) {
	raw := "HTTP/2.0 200 OK\nETag: \"abc123\"\nContent-Type: application/json\n\n[{\"id\":\"1\",\"type\":\"IssuesEvent\"}]"
	statusCode, etag, body := ParseGHResponseWithStatus(raw)

	if statusCode != 200 {
		t.Errorf("expected status code 200, got %d", statusCode)
	}
	if etag != "\"abc123\"" {
		t.Errorf("expected etag '\"abc123\"', got %q", etag)
	}
	if body != "[{\"id\":\"1\",\"type\":\"IssuesEvent\"}]" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestParseGHResponseWithStatus_304(t *testing.T) {
	raw := "HTTP/2.0 304 Not Modified\nETag: \"abc123\"\n\n"
	statusCode, etag, body := ParseGHResponseWithStatus(raw)

	if statusCode != 304 {
		t.Errorf("expected status code 304, got %d", statusCode)
	}
	if etag != "\"abc123\"" {
		t.Errorf("expected etag '\"abc123\"', got %q", etag)
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestParseGHResponseWithStatus_403(t *testing.T) {
	raw := "HTTP/1.1 403 Forbidden\nContent-Type: application/json\n\n{\"message\":\"Forbidden\"}"
	statusCode, etag, body := ParseGHResponseWithStatus(raw)

	if statusCode != 403 {
		t.Errorf("expected status code 403, got %d", statusCode)
	}
	if etag != "" {
		t.Errorf("expected empty etag, got %q", etag)
	}
	if body != "{\"message\":\"Forbidden\"}" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestParseGHResponseWithStatus_HeadersOnly(t *testing.T) {
	// Headers only, no body (no blank line separator)
	raw := "HTTP/2.0 304 Not Modified\nETag: \"xyz\""
	statusCode, etag, body := ParseGHResponseWithStatus(raw)

	if statusCode != 304 {
		t.Errorf("expected status code 304, got %d", statusCode)
	}
	_ = etag // ETag may or may not be parsed without the separator
	if body != "" {
		t.Errorf("expected empty body for headers-only response, got %q", body)
	}
}

func TestParseGHResponseWithStatus_HTMLBody(t *testing.T) {
	// CDN or proxy returning HTML error page
	raw := "HTTP/1.1 503 Service Unavailable\nContent-Type: text/html\n\n<html><body>Service Unavailable</body></html>"
	statusCode, etag, body := ParseGHResponseWithStatus(raw)

	if statusCode != 503 {
		t.Errorf("expected status code 503, got %d", statusCode)
	}
	if etag != "" {
		t.Errorf("expected empty etag, got %q", etag)
	}
	if !strings.HasPrefix(body, "<html>") {
		t.Errorf("expected HTML body, got %q", body)
	}
}

func TestParseGHResponseWithStatus_NoHeaders(t *testing.T) {
	// Raw JSON with no headers
	raw := `[{"id":"1","type":"IssuesEvent"}]`
	statusCode, etag, body := ParseGHResponseWithStatus(raw)

	if statusCode != 0 {
		t.Errorf("expected status code 0 for headerless response, got %d", statusCode)
	}
	if etag != "" {
		t.Errorf("expected empty etag, got %q", etag)
	}
	if body != raw {
		t.Errorf("expected body to be the raw JSON, got %q", body)
	}
}

func TestParseGHResponseWithStatus_BackwardCompat(t *testing.T) {
	// ParseGHResponse should still work as before (calls ParseGHResponseWithStatus internally)
	raw := "HTTP/2.0 200 OK\nETag: \"test-etag\"\nContent-Type: application/json\n\n[{\"id\":\"2\"}]"
	etag, body := ParseGHResponse(raw)

	if etag != "\"test-etag\"" {
		t.Errorf("expected etag '\"test-etag\"', got %q", etag)
	}
	if body != "[{\"id\":\"2\"}]" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestParseGHResponseWithStatus_InvalidHTTPStartsWithH(t *testing.T) {
	// This was the root cause of MADFLOW-002:
	// When gh api returns a non-JSON response starting with "HTTP/",
	// and there's no \n\n separator, the whole thing was treated as body.
	// Now it should be recognized as headers-only and return empty body.
	raw := "HTTP/2.0 403 Forbidden"
	statusCode, _, body := ParseGHResponseWithStatus(raw)

	if statusCode != 403 {
		t.Errorf("expected status code 403, got %d", statusCode)
	}
	if body != "" {
		t.Errorf("expected empty body (headers-only response), got %q", body)
	}
}

// --- Bot detection via IssueCommentEvent ---

func TestProcessIssueCommentEvent_BotComment(t *testing.T) {
	// A comment from a GitHub App (type="Bot") should have IsBot=true.
	dir := t.TempDir()
	store := issue.NewStore(dir)

	store.Update(&issue.Issue{ID: "owner-repo-001", Title: "Test", Status: issue.StatusOpen})

	var gotComment *issue.Comment
	cb := func(et EventType, id string, c *issue.Comment) {
		gotComment = c
	}

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, cb)

	payload := ghEventPayloadComment{
		Action: "created",
		Issue:  ghIssue{Number: 1},
		Comment: ghEventComment{
			ID:        100,
			Body:      "Automated status update",
			CreatedAt: "2026-02-24T10:00:00Z",
			UpdatedAt: "2026-02-24T10:00:00Z",
		},
	}
	payload.Comment.User.Login = "my-app[bot]"
	payload.Comment.User.Type = "Bot"
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{ID: "evt-bot-1", Type: "IssueCommentEvent", Payload: payloadBytes}
	w.processEvent("repo", ev)

	if gotComment == nil {
		t.Fatal("expected comment callback")
	}
	if !gotComment.IsBot {
		t.Errorf("expected IsBot=true for bot comment (type=Bot), got false")
	}
}

func TestProcessIssueCommentEvent_BotLoginSuffix(t *testing.T) {
	// A comment from a login ending with "[bot]" should have IsBot=true
	// even when user.type is not "Bot" (e.g. old API response).
	dir := t.TempDir()
	store := issue.NewStore(dir)

	store.Update(&issue.Issue{ID: "owner-repo-002", Title: "Test", Status: issue.StatusOpen})

	var gotComment *issue.Comment
	cb := func(et EventType, id string, c *issue.Comment) {
		gotComment = c
	}

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, cb)

	payload := ghEventPayloadComment{
		Action: "created",
		Issue:  ghIssue{Number: 2},
		Comment: ghEventComment{
			ID:        101,
			Body:      "Bot comment via login suffix",
			CreatedAt: "2026-02-24T10:00:00Z",
			UpdatedAt: "2026-02-24T10:00:00Z",
		},
	}
	payload.Comment.User.Login = "github-actions[bot]"
	payload.Comment.User.Type = "User" // type not set to Bot intentionally
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{ID: "evt-bot-2", Type: "IssueCommentEvent", Payload: payloadBytes}
	w.processEvent("repo", ev)

	if gotComment == nil {
		t.Fatal("expected comment callback")
	}
	if !gotComment.IsBot {
		t.Errorf("expected IsBot=true for comment from login ending with [bot], got false")
	}
}

func TestProcessIssueCommentEvent_HumanComment(t *testing.T) {
	// A comment from a regular user account should have IsBot=false.
	dir := t.TempDir()
	store := issue.NewStore(dir)

	store.Update(&issue.Issue{ID: "owner-repo-003", Title: "Test", Status: issue.StatusOpen})

	var gotComment *issue.Comment
	cb := func(et EventType, id string, c *issue.Comment) {
		gotComment = c
	}

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, cb)

	payload := ghEventPayloadComment{
		Action: "created",
		Issue:  ghIssue{Number: 3},
		Comment: ghEventComment{
			ID:        102,
			Body:      "Human discussion comment",
			CreatedAt: "2026-02-24T10:00:00Z",
			UpdatedAt: "2026-02-24T10:00:00Z",
		},
	}
	payload.Comment.User.Login = "alice"
	payload.Comment.User.Type = "User"
	payloadBytes, _ := json.Marshal(payload)

	ev := ghEvent{ID: "evt-human-1", Type: "IssueCommentEvent", Payload: payloadBytes}
	w.processEvent("repo", ev)

	if gotComment == nil {
		t.Fatal("expected comment callback")
	}
	if gotComment.IsBot {
		t.Errorf("expected IsBot=false for human comment, got true")
	}
}

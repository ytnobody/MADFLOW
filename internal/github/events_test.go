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

// --- Idle mode tests ---

// TestEventWatcherHasOpenIssues verifies that hasOpenIssues correctly queries the store.
func TestEventWatcherHasOpenIssues(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)
	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil)

	// No issues → false
	if w.hasOpenIssues() {
		t.Error("expected no open issues in empty store")
	}

	// Add an open issue → true
	openIss := &issue.Issue{ID: "test-001", Status: issue.StatusOpen}
	store.Update(openIss)
	if !w.hasOpenIssues() {
		t.Error("expected open issues after adding open issue")
	}

	// Add an in-progress issue alongside → still true
	inProgressIss := &issue.Issue{ID: "test-002", Status: issue.StatusInProgress}
	store.Update(inProgressIss)
	if !w.hasOpenIssues() {
		t.Error("expected open issues with in-progress issue present")
	}

	// Close the open issue → still true because in-progress remains
	openIss.Status = issue.StatusClosed
	store.Update(openIss)
	if !w.hasOpenIssues() {
		t.Error("expected open issues because in-progress issue remains")
	}

	// Resolve the in-progress issue → false
	inProgressIss.Status = issue.StatusResolved
	store.Update(inProgressIss)
	if w.hasOpenIssues() {
		t.Error("expected no open issues after all issues resolved/closed")
	}
}

// TestEventWatcherWithIdleThreshold verifies that WithIdleThreshold sets the field.
func TestEventWatcherWithIdleThreshold(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)
	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil)

	w.WithIdleThreshold(5 * time.Minute)
	if w.idleThreshold != 5*time.Minute {
		t.Errorf("expected idleThreshold 5m, got %v", w.idleThreshold)
	}
}

// TestEventWatcherSignalActive verifies that signalActive sends to activateCh
// and that duplicate signals do not block.
func TestEventWatcherSignalActive(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)
	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil)

	// Initially the channel is empty
	select {
	case <-w.activateCh:
		t.Fatal("expected activateCh to be empty initially")
	default:
	}

	// First signal should succeed
	w.signalActive()
	select {
	case <-w.activateCh:
		// OK
	default:
		t.Fatal("expected activateCh to have a value after signalActive()")
	}

	// Multiple signals should not block (buffered channel capacity = 1)
	w.signalActive()
	w.signalActive()
	// Channel should have at most one value
	count := 0
	for {
		select {
		case <-w.activateCh:
			count++
		default:
			goto done
		}
	}
done:
	if count != 1 {
		t.Errorf("expected 1 buffered signal, got %d", count)
	}
}

// TestEventWatcherSignalActiveOnIssueEvent verifies that processing an IssuesEvent
// sends a signal to activateCh.
func TestEventWatcherSignalActiveOnIssueEvent(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)
	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil)

	payload := ghEventPayloadIssue{
		Action: "opened",
		Issue:  ghIssue{Number: 1, Title: "Test", Body: "Body"},
	}
	payloadBytes, _ := json.Marshal(payload)
	ev := ghEvent{ID: "evt-active-1", Type: "IssuesEvent", Payload: payloadBytes}

	w.processEvent("repo", ev)

	select {
	case <-w.activateCh:
		// OK - signal was sent
	default:
		t.Error("expected activateCh to receive signal after IssuesEvent")
	}
}

// TestEventWatcherSignalActiveOnCommentEvent verifies that processing an IssueCommentEvent
// sends a signal to activateCh.
func TestEventWatcherSignalActiveOnCommentEvent(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Pre-create the issue so the comment event can be processed
	iss := &issue.Issue{ID: "owner-repo-001", Title: "Test", Status: issue.StatusOpen}
	store.Update(iss)

	w := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil)

	payload := ghEventPayloadComment{
		Action:  "created",
		Issue:   ghIssue{Number: 1},
		Comment: ghEventComment{ID: 99, Body: "Hello", CreatedAt: "2026-02-22T00:00:00Z", UpdatedAt: "2026-02-22T00:00:00Z"},
	}
	payload.Comment.User.Login = "bot"
	payloadBytes, _ := json.Marshal(payload)
	ev := ghEvent{ID: "evt-active-2", Type: "IssueCommentEvent", Payload: payloadBytes}

	w.processEvent("repo", ev)

	select {
	case <-w.activateCh:
		// OK - signal was sent
	default:
		t.Error("expected activateCh to receive signal after IssueCommentEvent")
	}
}

// TestEventWatcherIdleModeEnabled verifies the idle mode flag calculation.
func TestEventWatcherIdleModeEnabled(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// idle mode disabled: idleInterval <= interval
	w1 := NewEventWatcher(store, "owner", []string{"repo"}, 5*time.Minute, nil).
		WithIdleDetector(NewIdleDetector(), 1*time.Minute). // idleInterval < interval
		WithIdleThreshold(1 * time.Minute)
	enabled1 := w1.idleInterval > w1.interval && w1.idleThreshold > 0
	if enabled1 {
		t.Error("idle mode should be disabled when idleInterval <= interval")
	}

	// idle mode disabled: threshold == 0
	w2 := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil).
		WithIdleDetector(NewIdleDetector(), 15*time.Minute)
	// idleThreshold not set → 0
	enabled2 := w2.idleInterval > w2.interval && w2.idleThreshold > 0
	if enabled2 {
		t.Error("idle mode should be disabled when idleThreshold == 0")
	}

	// idle mode enabled: idleInterval > interval and threshold > 0
	w3 := NewEventWatcher(store, "owner", []string{"repo"}, time.Minute, nil).
		WithIdleDetector(NewIdleDetector(), 15*time.Minute).
		WithIdleThreshold(5 * time.Minute)
	enabled3 := w3.idleInterval > w3.interval && w3.idleThreshold > 0
	if !enabled3 {
		t.Error("idle mode should be enabled when idleInterval > interval and threshold > 0")
	}
}

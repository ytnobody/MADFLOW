package issue

import (
	"testing"
	"time"
)

func TestCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	issue, err := store.Create("テストイシュー", "これはテストです")
	if err != nil {
		t.Fatal(err)
	}
	if issue.ID != "local-001" {
		t.Errorf("expected ID local-001, got %s", issue.ID)
	}
	if issue.Status != StatusOpen {
		t.Errorf("expected status open, got %s", issue.Status)
	}

	got, err := store.Get("local-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "テストイシュー" {
		t.Errorf("expected title テストイシュー, got %s", got.Title)
	}
	if got.Body != "これはテストです" {
		t.Errorf("expected body これはテストです, got %s", got.Body)
	}
}

func TestCreateAutoIncrement(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	i1, _ := store.Create("Issue 1", "body1")
	i2, _ := store.Create("Issue 2", "body2")
	i3, _ := store.Create("Issue 3", "body3")

	if i1.ID != "local-001" {
		t.Errorf("expected local-001, got %s", i1.ID)
	}
	if i2.ID != "local-002" {
		t.Errorf("expected local-002, got %s", i2.ID)
	}
	if i3.ID != "local-003" {
		t.Errorf("expected local-003, got %s", i3.ID)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Create("Issue 1", "body1")
	store.Create("Issue 2", "body2")

	issues, err := store.List(StatusFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestListWithFilter(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	i1, _ := store.Create("Issue 1", "body1")
	store.Create("Issue 2", "body2")

	i1.Status = StatusInProgress
	store.Update(i1)

	open := StatusOpen
	issues, err := store.List(StatusFilter{Status: &open})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 open issue, got %d", len(issues))
	}
	if issues[0].ID != "local-002" {
		t.Errorf("expected local-002, got %s", issues[0].ID)
	}
}

func TestListNew(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Create("Issue 1", "body1")
	store.Create("Issue 2", "body2")
	store.Create("Issue 3", "body3")

	known := []string{"local-001", "local-002"}
	newIssues, err := store.ListNew(known)
	if err != nil {
		t.Fatal(err)
	}
	if len(newIssues) != 1 {
		t.Fatalf("expected 1 new issue, got %d", len(newIssues))
	}
	if newIssues[0].ID != "local-003" {
		t.Errorf("expected local-003, got %s", newIssues[0].ID)
	}
}

func TestUpdate(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	issue, _ := store.Create("テスト", "本文")
	issue.Status = StatusInProgress
	issue.AssignedTeam = 1
	issue.Labels = []string{"feature"}
	issue.Acceptance = "テストが通ること"

	if err := store.Update(issue); err != nil {
		t.Fatal(err)
	}

	got, _ := store.Get(issue.ID)
	if got.Status != StatusInProgress {
		t.Errorf("expected in_progress, got %s", got.Status)
	}
	if got.AssignedTeam != 1 {
		t.Errorf("expected team 1, got %d", got.AssignedTeam)
	}
	if got.Acceptance != "テストが通ること" {
		t.Errorf("unexpected acceptance: %s", got.Acceptance)
	}
}

func TestListEmptyDir(t *testing.T) {
	store := NewStore("/tmp/nonexistent-issue-dir")
	issues, err := store.List(StatusFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d", len(issues))
	}
}

func TestCommentTOMLPersistence(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	iss, err := store.Create("Issue with comments", "body")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	c1 := Comment{ID: 100, Author: "alice", Body: "first comment", CreatedAt: now, UpdatedAt: now}
	c2 := Comment{ID: 200, Author: "bob", Body: "second comment", CreatedAt: now, UpdatedAt: now}

	iss.AddComment(c1)
	iss.AddComment(c2)

	if err := store.Update(iss); err != nil {
		t.Fatal(err)
	}

	// Re-read from disk
	got, err := store.Get(iss.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(got.Comments))
	}
	if got.Comments[0].Author != "alice" {
		t.Errorf("expected author alice, got %s", got.Comments[0].Author)
	}
	if got.Comments[1].ID != 200 {
		t.Errorf("expected comment ID 200, got %d", got.Comments[1].ID)
	}
}

func TestCommentDedup(t *testing.T) {
	iss := &Issue{ID: "test-001"}
	c := Comment{ID: 42, Author: "user", Body: "hello"}

	if !iss.AddComment(c) {
		t.Error("first AddComment should return true")
	}
	if iss.AddComment(c) {
		t.Error("duplicate AddComment should return false")
	}
	if len(iss.Comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(iss.Comments))
	}
}

func TestHasComment(t *testing.T) {
	iss := &Issue{ID: "test-001"}
	if iss.HasComment(1) {
		t.Error("should not have comment 1")
	}
	iss.Comments = []Comment{{ID: 1, Author: "a", Body: "b"}}
	if !iss.HasComment(1) {
		t.Error("should have comment 1")
	}
}

func TestBackwardCompatNoComments(t *testing.T) {
	// An issue TOML file without comments should load fine
	dir := t.TempDir()
	store := NewStore(dir)

	iss, err := store.Create("No comments", "body")
	if err != nil {
		t.Fatal(err)
	}

	// Re-read — Comments should be nil/empty
	got, err := store.Get(iss.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(got.Comments))
	}
}

func TestPendingApprovalDefault(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	iss, err := store.Create("Test", "body")
	if err != nil {
		t.Fatal(err)
	}

	// PendingApproval should default to false.
	if iss.PendingApproval {
		t.Error("expected PendingApproval=false by default")
	}
}

func TestPendingApprovalPersistence(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	iss, err := store.Create("Pending Issue", "body")
	if err != nil {
		t.Fatal(err)
	}

	// Set PendingApproval and persist.
	iss.PendingApproval = true
	if err := store.Update(iss); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(iss.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.PendingApproval {
		t.Error("expected PendingApproval=true after Update/Get")
	}

	// Clear PendingApproval and persist.
	got.PendingApproval = false
	if err := store.Update(got); err != nil {
		t.Fatal(err)
	}

	cleared, err := store.Get(iss.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.PendingApproval {
		t.Error("expected PendingApproval=false after clearing and Update/Get")
	}
}

func TestPendingApprovalNotAssignable(t *testing.T) {
	// Verify that listing can be used to filter pending-approval issues.
	dir := t.TempDir()
	store := NewStore(dir)

	regular, _ := store.Create("Regular Issue", "body")
	pending, _ := store.Create("Pending Issue", "body")

	pending.PendingApproval = true
	store.Update(pending)

	// Simulate what the orchestrator does: filter out PendingApproval issues.
	all, err := store.List(StatusFilter{})
	if err != nil {
		t.Fatal(err)
	}

	var assignable []*Issue
	for _, iss := range all {
		if iss.Status == StatusOpen && !iss.PendingApproval {
			assignable = append(assignable, iss)
		}
	}

	if len(assignable) != 1 {
		t.Fatalf("expected 1 assignable issue, got %d", len(assignable))
	}
	if assignable[0].ID != regular.ID {
		t.Errorf("expected regular issue %s, got %s", regular.ID, assignable[0].ID)
	}
}

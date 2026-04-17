package issue

import (
	"testing"
	"time"
)

// --- Status sum type tests ---

func TestStatus_String(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOpen, "open"},
		{StatusInProgress, "in_progress"},
		{StatusResolved, "resolved"},
		{StatusClosed, "closed"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Status.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusOpen, false},
		{StatusInProgress, false},
		{StatusResolved, true},
		{StatusClosed, true},
	}
	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("Status.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatus_MarshalUnmarshalText(t *testing.T) {
	tests := []struct {
		status Status
		text   string
	}{
		{StatusOpen, "open"},
		{StatusInProgress, "in_progress"},
		{StatusResolved, "resolved"},
		{StatusClosed, "closed"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			b, err := tt.status.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error: %v", err)
			}
			if string(b) != tt.text {
				t.Errorf("MarshalText() = %q, want %q", string(b), tt.text)
			}

			var s Status
			if err := s.UnmarshalText([]byte(tt.text)); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", tt.text, err)
			}
			if s != tt.status {
				t.Errorf("UnmarshalText(%q) = %v, want %v", tt.text, s, tt.status)
			}
		})
	}
}

func TestStatus_UnmarshalText_Unknown(t *testing.T) {
	var s Status
	err := s.UnmarshalText([]byte("unknown_status"))
	if err == nil {
		t.Error("expected error for unknown status, got nil")
	}
}

func TestStatus_TOMLRoundtrip(t *testing.T) {
	// Verify that Status survives TOML encode/decode as a human-readable string.
	dir := t.TempDir()
	store := NewStore(dir)

	iss, err := store.Create("TOML roundtrip test", "body")
	if err != nil {
		t.Fatal(err)
	}

	iss.Status = StatusInProgress
	if err := store.Update(iss); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(iss.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Status != StatusInProgress {
		t.Errorf("expected StatusInProgress after TOML roundtrip, got %v", got.Status)
	}
}

// --- NewIssue smart constructor tests ---

func TestNewIssue(t *testing.T) {
	iss := NewIssue("test-001", "Test Title", "Test body")
	if iss.ID != "test-001" {
		t.Errorf("expected ID test-001, got %s", iss.ID)
	}
	if iss.Title != "Test Title" {
		t.Errorf("expected title Test Title, got %s", iss.Title)
	}
	if iss.Body != "Test body" {
		t.Errorf("expected body Test body, got %s", iss.Body)
	}
	if iss.Status != StatusOpen {
		t.Errorf("expected status open, got %v", iss.Status)
	}
	if iss.AssignedTeam != 0 {
		t.Errorf("expected AssignedTeam 0, got %d", iss.AssignedTeam)
	}
}

// --- State transition function tests ---

func TestTransitionToInProgress(t *testing.T) {
	iss := NewIssue("test-001", "Title", "body")

	result, err := TransitionToInProgress(*iss, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusInProgress {
		t.Errorf("expected StatusInProgress, got %v", result.Status)
	}
	if result.AssignedTeam != 42 {
		t.Errorf("expected AssignedTeam 42, got %d", result.AssignedTeam)
	}
	// Original should be unchanged (pure function).
	if iss.Status != StatusOpen {
		t.Error("original issue should be unchanged")
	}
}

func TestTransitionToInProgress_FromTerminal(t *testing.T) {
	for _, s := range []Status{StatusResolved, StatusClosed} {
		iss := Issue{ID: "test-001", Status: s}
		_, err := TransitionToInProgress(iss, 1)
		if err == nil {
			t.Errorf("expected error transitioning from terminal status %v", s)
		}
	}
}

func TestTransitionToOpen(t *testing.T) {
	iss := Issue{ID: "test-001", Status: StatusInProgress, AssignedTeam: 5}
	result, err := TransitionToOpen(iss)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusOpen {
		t.Errorf("expected StatusOpen, got %v", result.Status)
	}
	if result.AssignedTeam != 0 {
		t.Errorf("expected AssignedTeam 0, got %d", result.AssignedTeam)
	}
	// Original should be unchanged.
	if iss.Status != StatusInProgress {
		t.Error("original issue should be unchanged")
	}
}

func TestTransitionToOpen_InvalidState(t *testing.T) {
	for _, s := range []Status{StatusOpen, StatusResolved, StatusClosed} {
		iss := Issue{ID: "test-001", Status: s}
		_, err := TransitionToOpen(iss)
		if err == nil {
			t.Errorf("expected error transitioning to open from status %v", s)
		}
	}
}

func TestTransitionToResolved(t *testing.T) {
	iss := Issue{ID: "test-001", Status: StatusInProgress, AssignedTeam: 3}
	result, err := TransitionToResolved(iss)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusResolved {
		t.Errorf("expected StatusResolved, got %v", result.Status)
	}
	// Original should be unchanged.
	if iss.Status != StatusInProgress {
		t.Error("original issue should be unchanged")
	}
}

func TestTransitionToResolved_InvalidState(t *testing.T) {
	for _, s := range []Status{StatusOpen, StatusResolved, StatusClosed} {
		iss := Issue{ID: "test-001", Status: s}
		_, err := TransitionToResolved(iss)
		if err == nil {
			t.Errorf("expected error resolving from status %v", s)
		}
	}
}

func TestTransitionToClosed(t *testing.T) {
	for _, s := range []Status{StatusOpen, StatusInProgress, StatusResolved, StatusClosed} {
		iss := Issue{ID: "test-001", Status: s}
		result, err := TransitionToClosed(iss)
		if err != nil {
			t.Errorf("unexpected error closing from status %v: %v", s, err)
			continue
		}
		if result.Status != StatusClosed {
			t.Errorf("expected StatusClosed, got %v", result.Status)
		}
	}
}

// --- MergeComments pure function tests ---

func TestMergeComments_NewComment(t *testing.T) {
	existing := []Comment{{ID: 1, Author: "alice", Body: "hello"}}
	c := Comment{ID: 2, Author: "bob", Body: "world"}

	result, added := MergeComments(existing, c)
	if !added {
		t.Error("expected comment to be added")
	}
	if len(result) != 2 {
		t.Errorf("expected 2 comments, got %d", len(result))
	}
	// Original slice should be unchanged.
	if len(existing) != 1 {
		t.Error("original slice should be unchanged")
	}
}

func TestMergeComments_DuplicateComment(t *testing.T) {
	c := Comment{ID: 1, Author: "alice", Body: "hello"}
	existing := []Comment{c}

	result, added := MergeComments(existing, c)
	if added {
		t.Error("expected duplicate comment NOT to be added")
	}
	if len(result) != 1 {
		t.Errorf("expected 1 comment, got %d", len(result))
	}
}

func TestMergeComments_EmptySlice(t *testing.T) {
	c := Comment{ID: 1, Author: "alice", Body: "hello"}
	result, added := MergeComments(nil, c)
	if !added {
		t.Error("expected comment to be added to empty slice")
	}
	if len(result) != 1 {
		t.Errorf("expected 1 comment, got %d", len(result))
	}
}

func TestMergeComments_Immutability(t *testing.T) {
	// Verify that MergeComments does not modify the original slice.
	original := []Comment{{ID: 1, Author: "alice", Body: "a"}}
	originalLen := len(original)

	result, _ := MergeComments(original, Comment{ID: 2, Author: "bob", Body: "b"})
	if len(original) != originalLen {
		t.Error("MergeComments must not modify the original slice")
	}
	_ = result
}

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

func TestIsBotLogin(t *testing.T) {
	tests := []struct {
		login string
		want  bool
	}{
		{"github-actions[bot]", true},
		{"dependabot[bot]", true},
		{"renovate[bot]", true},
		{"alice", false},
		{"bob", false},
		{"[bot]", true},  // edge case: exactly "[bot]"
		{"mybot", false}, // does not end with "[bot]"
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.login, func(t *testing.T) {
			got := IsBotLogin(tt.login)
			if got != tt.want {
				t.Errorf("IsBotLogin(%q) = %v, want %v", tt.login, got, tt.want)
			}
		})
	}
}

func TestCommentIsBot_Persistence(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	iss, err := store.Create("Issue with bot comment", "body")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	human := Comment{ID: 1, Author: "alice", Body: "human comment", CreatedAt: now, UpdatedAt: now, IsBot: false}
	bot := Comment{ID: 2, Author: "github-actions[bot]", Body: "bot comment", CreatedAt: now, UpdatedAt: now, IsBot: true}

	iss.AddComment(human)
	iss.AddComment(bot)

	if err := store.Update(iss); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(iss.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(got.Comments))
	}

	// Human comment: IsBot should be false
	if got.Comments[0].IsBot {
		t.Errorf("human comment should have IsBot=false")
	}
	// Bot comment: IsBot should be true
	if !got.Comments[1].IsBot {
		t.Errorf("bot comment should have IsBot=true")
	}
}

func TestCommentIsBot_DefaultFalse(t *testing.T) {
	// Comments created without setting IsBot should default to false.
	dir := t.TempDir()
	store := NewStore(dir)

	iss, err := store.Create("Default IsBot test", "body")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	c := Comment{ID: 1, Author: "alice", Body: "hello", CreatedAt: now, UpdatedAt: now}
	iss.AddComment(c)
	if err := store.Update(iss); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(iss.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Comments[0].IsBot {
		t.Error("IsBot should default to false when not set")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	iss, err := store.Create("To be deleted", "body")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(iss.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Get should fail after deletion.
	if _, err := store.Get(iss.ID); err == nil {
		t.Error("expected Get to fail after Delete")
	}
}

func TestDeleteIdempotent(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Deleting a non-existent ID should not return an error.
	if err := store.Delete("nonexistent-999"); err != nil {
		t.Errorf("Delete of non-existent ID should be idempotent, got: %v", err)
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

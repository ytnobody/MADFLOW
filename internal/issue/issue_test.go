package issue

import (
	"testing"
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

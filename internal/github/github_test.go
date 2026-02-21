package github

import (
	"testing"
)

func TestFormatID(t *testing.T) {
	tests := []struct {
		owner  string
		repo   string
		number int
		want   string
	}{
		{"ytnobody", "madflow", 1, "ytnobody-madflow-001"},
		{"ytnobody", "madflow", 42, "ytnobody-madflow-042"},
		{"ytnobody", "madflow", 100, "ytnobody-madflow-100"},
		{"ytnobody", "madflow", 1234, "ytnobody-madflow-1234"},
		{"owner", "repo", 0, "owner-repo-000"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatID(tt.owner, tt.repo, tt.number)
			if got != tt.want {
				t.Errorf("FormatID(%q, %q, %d) = %q, want %q", tt.owner, tt.repo, tt.number, got, tt.want)
			}
		})
	}
}

func TestFormatIDInternal(t *testing.T) {
	// Test the unexported formatID function (accessible from same package)
	got := formatID("acme", "tools", 7)
	want := "acme-tools-007"
	if got != want {
		t.Errorf("formatID = %q, want %q", got, want)
	}
}

func TestParseID(t *testing.T) {
	tests := []struct {
		id         string
		wantOwner  string
		wantRepo   string
		wantNumber int
	}{
		{"ytnobody-madflow-001", "ytnobody", "madflow", 1},
		{"ytnobody-madflow-042", "ytnobody", "madflow", 42},
		{"ytnobody-madflow-100", "ytnobody", "madflow", 100},
		{"owner-repo-000", "owner", "repo", 0},
		{"org-my-repo-005", "org", "my-repo", 5},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			owner, repo, number, err := ParseID(tt.id)
			if err != nil {
				t.Fatalf("ParseID(%q) unexpected error: %v", tt.id, err)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if number != tt.wantNumber {
				t.Errorf("number = %d, want %d", number, tt.wantNumber)
			}
		})
	}
}

func TestParseIDRoundTrip(t *testing.T) {
	// FormatID -> ParseID should give back the same values
	owner, repo, number := "ytnobody", "madflow", 42
	id := FormatID(owner, repo, number)

	gotOwner, gotRepo, gotNumber, err := ParseID(id)
	if err != nil {
		t.Fatalf("ParseID round-trip error: %v", err)
	}
	if gotOwner != owner {
		t.Errorf("round-trip owner = %q, want %q", gotOwner, owner)
	}
	if gotRepo != repo {
		t.Errorf("round-trip repo = %q, want %q", gotRepo, repo)
	}
	if gotNumber != number {
		t.Errorf("round-trip number = %d, want %d", gotNumber, number)
	}
}

func TestParseIDErrors(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"no dashes", "nodashes"},
		{"empty string", ""},
		{"single dash no number", "owner-repo"},
		{"number not numeric", "owner-repo-abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := ParseID(tt.id)
			if err == nil {
				t.Errorf("ParseID(%q) expected error, got nil", tt.id)
			}
		})
	}
}

func TestExtractLabels(t *testing.T) {
	labels := []ghLabel{
		{Name: "bug"},
		{Name: "enhancement"},
		{Name: "help wanted"},
	}

	result := extractLabels(labels)
	if len(result) != 3 {
		t.Fatalf("expected 3 labels, got %d", len(result))
	}
	expected := []string{"bug", "enhancement", "help wanted"}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("label[%d] = %q, want %q", i, result[i], want)
		}
	}
}

func TestExtractLabelsEmpty(t *testing.T) {
	result := extractLabels(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 labels for nil input, got %d", len(result))
	}
}

func TestNewSyncer(t *testing.T) {
	// Verify NewSyncer constructs a Syncer with correct fields.
	// We pass nil store since we only check struct fields.
	s := NewSyncer(nil, "ytnobody", []string{"madflow", "other-repo"}, 0)
	if s == nil {
		t.Fatal("NewSyncer returned nil")
	}
	if s.owner != "ytnobody" {
		t.Errorf("owner = %q, want %q", s.owner, "ytnobody")
	}
	if len(s.repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(s.repos))
	}
	if s.repos[0] != "madflow" {
		t.Errorf("repos[0] = %q, want %q", s.repos[0], "madflow")
	}
}

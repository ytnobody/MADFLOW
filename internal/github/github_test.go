package github

import (
	"strings"
	"testing"
	"time"

	"github.com/ytnobody/madflow/internal/issue"
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

func TestSyncer_WithIdleDetector(t *testing.T) {
	d := NewIdleDetector()
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithIdleDetector(d, 15*time.Minute)

	if s.idleDetector != d {
		t.Error("expected idleDetector to be set")
	}
	if s.idleInterval != 15*time.Minute {
		t.Errorf("expected idleInterval 15m, got %v", s.idleInterval)
	}
}

func TestSyncer_CurrentInterval_NoDetector(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, 30*time.Second)
	// Without idle detector, always returns normal interval
	if got := s.currentInterval(); got != 30*time.Second {
		t.Errorf("expected 30s, got %v", got)
	}
}

func TestSyncer_CurrentInterval_WithDetector(t *testing.T) {
	d := NewIdleDetector()
	normal := 30 * time.Second
	idle := 15 * time.Minute
	s := NewSyncer(nil, "owner", []string{"repo"}, normal).
		WithIdleDetector(d, idle)

	// With issues (detector default) → normal
	if got := s.currentInterval(); got != normal {
		t.Errorf("with issues: expected %v, got %v", normal, got)
	}

	// Without issues → idle
	d.SetHasIssues(false)
	if got := s.currentInterval(); got != idle {
		t.Errorf("without issues: expected %v, got %v", idle, got)
	}

	// Issues return → normal again
	d.SetHasIssues(true)
	if got := s.currentInterval(); got != normal {
		t.Errorf("issues returned: expected %v, got %v", normal, got)
	}
}

func TestSyncer_UpdateIdleState_NoIssues(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	d := NewIdleDetector()
	// Start as active
	d.SetHasIssues(true)

	s := NewSyncer(store, "owner", []string{"repo"}, time.Minute).
		WithIdleDetector(d, 15*time.Minute)

	// No issues in store → detector should report no issues
	s.updateIdleState()
	if d.HasIssues() {
		t.Error("expected HasIssues=false when store is empty")
	}
}

func TestSyncer_UpdateIdleState_WithOpenIssue(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Add an open issue
	iss := &issue.Issue{
		ID:     "owner-repo-001",
		Title:  "Open Issue",
		Status: issue.StatusOpen,
	}
	_ = store.Update(iss)

	d := NewIdleDetector()
	d.SetHasIssues(false) // Start as idle

	s := NewSyncer(store, "owner", []string{"repo"}, time.Minute).
		WithIdleDetector(d, 15*time.Minute)

	// Open issue in store → detector should report has issues
	s.updateIdleState()
	if !d.HasIssues() {
		t.Error("expected HasIssues=true when there is an open issue")
	}
}

func TestSyncer_UpdateIdleState_WithInProgressIssue(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Add an in-progress issue
	iss := &issue.Issue{
		ID:     "local-001",
		Title:  "In Progress Issue",
		Status: issue.StatusInProgress,
	}
	_ = store.Update(iss)

	d := NewIdleDetector()
	d.SetHasIssues(false)

	s := NewSyncer(store, "owner", []string{"repo"}, time.Minute).
		WithIdleDetector(d, 15*time.Minute)

	s.updateIdleState()
	if !d.HasIssues() {
		t.Error("expected HasIssues=true when there is an in-progress issue")
	}
}

func TestSyncer_UpdateIdleState_OnlyClosedIssues(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Add only closed/resolved issues
	_ = store.Update(&issue.Issue{ID: "owner-repo-001", Title: "Closed", Status: issue.StatusClosed})
	_ = store.Update(&issue.Issue{ID: "owner-repo-002", Title: "Resolved", Status: issue.StatusResolved})

	d := NewIdleDetector()
	d.SetHasIssues(true)

	s := NewSyncer(store, "owner", []string{"repo"}, time.Minute).
		WithIdleDetector(d, 15*time.Minute)

	s.updateIdleState()
	if d.HasIssues() {
		t.Error("expected HasIssues=false when all issues are closed/resolved")
	}
}

func TestSyncer_UpdateIdleState_NilDetector(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Without idle detector, updateIdleState should be a no-op (no panic)
	s := NewSyncer(store, "owner", []string{"repo"}, time.Minute)
	s.updateIdleState() // should not panic
}

func TestIsAuthorized_EmptyList(t *testing.T) {
	// Empty authorized users means everyone is authorized.
	if !isAuthorized("alice", nil) {
		t.Error("expected alice to be authorized when list is empty")
	}
	if !isAuthorized("", nil) {
		t.Error("expected empty login to be authorized when list is empty")
	}
}

func TestIsAuthorized_WithList(t *testing.T) {
	authorized := []string{"alice", "bob"}

	if !isAuthorized("alice", authorized) {
		t.Error("expected alice to be authorized")
	}
	if !isAuthorized("bob", authorized) {
		t.Error("expected bob to be authorized")
	}
	if isAuthorized("charlie", authorized) {
		t.Error("expected charlie to be unauthorized")
	}
	if isAuthorized("", authorized) {
		t.Error("expected empty login to be unauthorized when list is non-empty")
	}
}

func TestGhIssueAuthorLogin_UserField(t *testing.T) {
	g := &ghIssue{}
	g.User.Login = "alice"
	g.Author.Login = "bob"

	// User.Login takes priority over Author.Login.
	if got := g.authorLogin(); got != "alice" {
		t.Errorf("expected alice, got %q", got)
	}
}

func TestGhIssueAuthorLogin_FallbackAuthor(t *testing.T) {
	g := &ghIssue{}
	g.Author.Login = "bob"

	// Falls back to Author.Login when User.Login is empty.
	if got := g.authorLogin(); got != "bob" {
		t.Errorf("expected bob, got %q", got)
	}
}

func TestGhIssueAuthorLogin_Empty(t *testing.T) {
	g := &ghIssue{}
	if got := g.authorLogin(); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestSyncer_WithAuthorizedUsers(t *testing.T) {
	s := NewSyncer(nil, "owner", []string{"repo"}, time.Minute).
		WithAuthorizedUsers([]string{"alice", "bob"})

	if len(s.authorizedUsers) != 2 {
		t.Fatalf("expected 2 authorized users, got %d", len(s.authorizedUsers))
	}
	if s.authorizedUsers[0] != "alice" {
		t.Errorf("expected alice, got %q", s.authorizedUsers[0])
	}
}

func TestSyncer_SyncComments_ApprovalByAuthorizedUser(t *testing.T) {
	// Test that /approve from an authorized user clears PendingApproval.
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Create a pending-approval issue.
	iss := &issue.Issue{
		ID:              "owner-repo-001",
		Title:           "Pending Issue",
		Status:          issue.StatusOpen,
		PendingApproval: true,
	}
	_ = store.Update(iss)

	// Add an /approve comment from an authorized user.
	now := time.Now()
	iss.Comments = []issue.Comment{
		{ID: 1, Author: "alice", Body: "/approve", CreatedAt: now, UpdatedAt: now},
	}
	_ = store.Update(iss)

	// Simulate the approval-check logic from syncComments.
	syncer := NewSyncer(store, "owner", []string{"repo"}, time.Minute).
		WithAuthorizedUsers([]string{"alice"})

	loaded, _ := store.Get("owner-repo-001")
	if !loaded.PendingApproval {
		t.Fatal("expected PendingApproval=true initially")
	}

	// Check for /approve among comments.
	if loaded.PendingApproval {
		for _, c := range loaded.Comments {
			if isAuthorized(c.Author, syncer.authorizedUsers) &&
				containsApprove(c.Body) {
				loaded.PendingApproval = false
				_ = store.Update(loaded)
				break
			}
		}
	}

	result, _ := store.Get("owner-repo-001")
	if result.PendingApproval {
		t.Error("expected PendingApproval=false after /approve comment from authorized user")
	}
}

func TestSyncer_SyncComments_NoApprovalFromUnauthorized(t *testing.T) {
	// Test that /approve from an unauthorized user does NOT clear PendingApproval.
	dir := t.TempDir()
	store := issue.NewStore(dir)

	iss := &issue.Issue{
		ID:              "owner-repo-002",
		Title:           "Pending Issue",
		Status:          issue.StatusOpen,
		PendingApproval: true,
		Comments: []issue.Comment{
			{ID: 2, Author: "charlie", Body: "/approve"},
		},
	}
	_ = store.Update(iss)

	syncer := NewSyncer(store, "owner", []string{"repo"}, time.Minute).
		WithAuthorizedUsers([]string{"alice", "bob"})

	// Simulate the approval-check logic - charlie is not authorized.
	loaded, _ := store.Get("owner-repo-002")
	if loaded.PendingApproval {
		for _, c := range loaded.Comments {
			if isAuthorized(c.Author, syncer.authorizedUsers) &&
				containsApprove(c.Body) {
				loaded.PendingApproval = false
				_ = store.Update(loaded)
				break
			}
		}
	}

	result, _ := store.Get("owner-repo-002")
	if !result.PendingApproval {
		t.Error("expected PendingApproval=true: /approve from unauthorized user should be ignored")
	}
}

// containsApprove is a helper mirroring the /approve detection in syncComments.
func containsApprove(body string) bool {
	return strings.Contains(strings.ToLower(body), "/approve")
}

// --- belongsToRepo tests ---

func TestBelongsToRepo_ByReposField(t *testing.T) {
	iss := &issue.Issue{
		ID:    "owner-myrepo-001",
		URL:   "https://github.com/owner/myrepo/issues/1",
		Repos: []string{"myrepo"},
	}
	if !belongsToRepo(iss, "myrepo") {
		t.Error("expected belongsToRepo=true when repo is in Repos field")
	}
	if belongsToRepo(iss, "other-repo") {
		t.Error("expected belongsToRepo=false for a different repo")
	}
}

func TestBelongsToRepo_ByURL(t *testing.T) {
	iss := &issue.Issue{
		ID:  "owner-myrepo-001",
		URL: "https://github.com/owner/myrepo/issues/1",
	}
	if !belongsToRepo(iss, "myrepo") {
		t.Error("expected belongsToRepo=true when URL contains repo name")
	}
	if belongsToRepo(iss, "other-repo") {
		t.Error("expected belongsToRepo=false for a different repo via URL")
	}
}

func TestBelongsToRepo_NoURLNoRepos(t *testing.T) {
	iss := &issue.Issue{
		ID: "local-001",
	}
	if belongsToRepo(iss, "myrepo") {
		t.Error("expected belongsToRepo=false for local issue with no URL or Repos")
	}
}

// --- closeStaleIssues tests ---

func TestCloseStaleIssues_ClosesStaleIssue(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Create an in_progress issue that belongs to "repo"
	store.Update(&issue.Issue{
		ID:     "owner-repo-001",
		Title:  "Stale Issue",
		URL:    "https://github.com/owner/repo/issues/1",
		Status: issue.StatusInProgress,
		Repos:  []string{"repo"},
	})

	s := NewSyncer(store, "owner", []string{"repo"}, 0)

	// openIDs is empty → issue should be closed
	s.closeStaleIssues("repo", map[string]struct{}{})

	iss, _ := store.Get("owner-repo-001")
	if iss.Status != issue.StatusClosed {
		t.Errorf("expected status=closed, got %s", iss.Status)
	}
}

func TestCloseStaleIssues_KeepsOpenIssue(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	store.Update(&issue.Issue{
		ID:     "owner-repo-002",
		Title:  "Open Issue",
		URL:    "https://github.com/owner/repo/issues/2",
		Status: issue.StatusOpen,
		Repos:  []string{"repo"},
	})

	s := NewSyncer(store, "owner", []string{"repo"}, 0)

	// Issue is in the open set → should NOT be closed
	openIDs := map[string]struct{}{"owner-repo-002": {}}
	s.closeStaleIssues("repo", openIDs)

	iss, _ := store.Get("owner-repo-002")
	if iss.Status != issue.StatusOpen {
		t.Errorf("expected status=open, got %s", iss.Status)
	}
}

func TestCloseStaleIssues_SkipsAlreadyClosed(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	store.Update(&issue.Issue{
		ID:     "owner-repo-003",
		Title:  "Already Closed",
		URL:    "https://github.com/owner/repo/issues/3",
		Status: issue.StatusClosed,
		Repos:  []string{"repo"},
	})

	s := NewSyncer(store, "owner", []string{"repo"}, 0)

	// Should not error or change anything
	s.closeStaleIssues("repo", map[string]struct{}{})

	iss, _ := store.Get("owner-repo-003")
	if iss.Status != issue.StatusClosed {
		t.Errorf("expected status=closed (unchanged), got %s", iss.Status)
	}
}

func TestCloseStaleIssues_SkipsLocalIssues(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Local issue (no URL) should be untouched
	store.Update(&issue.Issue{
		ID:     "local-001",
		Title:  "Local Issue",
		Status: issue.StatusOpen,
	})

	s := NewSyncer(store, "owner", []string{"repo"}, 0)
	s.closeStaleIssues("repo", map[string]struct{}{})

	iss, _ := store.Get("local-001")
	if iss.Status != issue.StatusOpen {
		t.Errorf("expected local issue status=open (unchanged), got %s", iss.Status)
	}
}

func TestCloseStaleIssues_SkipsOtherRepo(t *testing.T) {
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Issue belongs to "other-repo", not "repo"
	store.Update(&issue.Issue{
		ID:     "owner-other-repo-001",
		Title:  "Other Repo Issue",
		URL:    "https://github.com/owner/other-repo/issues/1",
		Status: issue.StatusInProgress,
		Repos:  []string{"other-repo"},
	})

	s := NewSyncer(store, "owner", []string{"repo", "other-repo"}, 0)

	// Sync "repo" with empty openIDs — should NOT close the "other-repo" issue
	s.closeStaleIssues("repo", map[string]struct{}{})

	iss, _ := store.Get("owner-other-repo-001")
	if iss.Status != issue.StatusInProgress {
		t.Errorf("expected status=in_progress (belongs to other repo), got %s", iss.Status)
	}
}

func TestIsBotUser_BotType(t *testing.T) {
	// A user with type "Bot" should be identified as a bot regardless of login.
	if !isBotUser("some-app", "Bot") {
		t.Error("expected isBotUser=true for type=Bot")
	}
}

func TestIsBotUser_BotLogin(t *testing.T) {
	// A user whose login ends with "[bot]" should be identified as a bot.
	if !isBotUser("github-actions[bot]", "User") {
		t.Error("expected isBotUser=true for login ending with [bot]")
	}
	if !isBotUser("dependabot[bot]", "") {
		t.Error("expected isBotUser=true for dependabot[bot]")
	}
}

func TestIsBotUser_HumanUser(t *testing.T) {
	// A regular user should NOT be identified as a bot.
	if isBotUser("alice", "User") {
		t.Error("expected isBotUser=false for regular user")
	}
	if isBotUser("bob", "") {
		t.Error("expected isBotUser=false for user with empty type")
	}
}

// --- CompileBotPatterns / matchesBotPattern / isBot tests ---

func TestCompileBotPatterns_Valid(t *testing.T) {
	patterns, err := CompileBotPatterns([]string{`^\*\*\[`, `\[bot\]$`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 2 {
		t.Errorf("expected 2 compiled patterns, got %d", len(patterns))
	}
}

func TestCompileBotPatterns_Empty(t *testing.T) {
	patterns, err := CompileBotPatterns(nil)
	if err != nil {
		t.Fatalf("unexpected error for nil input: %v", err)
	}
	if len(patterns) != 0 {
		t.Errorf("expected 0 compiled patterns, got %d", len(patterns))
	}
}

func TestCompileBotPatterns_Invalid(t *testing.T) {
	_, err := CompileBotPatterns([]string{`^\*\*\[`, `[invalid`})
	if err == nil {
		t.Error("expected error for invalid regex, got nil")
	}
}

func TestMatchesBotPattern_Match(t *testing.T) {
	patterns, _ := CompileBotPatterns([]string{`^\*\*\[`})

	bodies := []string{
		"**[実装開始]** by `engineer-1`\n実装を開始しました。",
		"**[実装完了]** by `engineer-1`",
		"**[質問]** by `engineer-1`",
		"**[エンジニアアサイン]** by `superintendent`",
	}
	for _, body := range bodies {
		if !matchesBotPattern(body, patterns) {
			t.Errorf("expected matchesBotPattern=true for body: %q", body)
		}
	}
}

func TestMatchesBotPattern_NoMatch(t *testing.T) {
	patterns, _ := CompileBotPatterns([]string{`^\*\*\[`})

	humanBodies := []string{
		"This looks great, please review.",
		"Can we discuss the approach?",
		"LGTM",
	}
	for _, body := range humanBodies {
		if matchesBotPattern(body, patterns) {
			t.Errorf("expected matchesBotPattern=false for human body: %q", body)
		}
	}
}

func TestMatchesBotPattern_NilPatterns(t *testing.T) {
	// With nil patterns no body should be considered a bot comment.
	if matchesBotPattern("**[実装開始]**", nil) {
		t.Error("expected matchesBotPattern=false when patterns is nil")
	}
}

func TestIsBot_PatternOnly(t *testing.T) {
	// Regular user login but bot-formatted body should be detected.
	patterns, _ := CompileBotPatterns([]string{`^\*\*\[`})

	if !isBot("ytnobody", "User", "**[実装完了]** by `engineer-1`", patterns) {
		t.Error("expected isBot=true for bot-formatted body from regular user")
	}
}

func TestIsBot_HumanComment(t *testing.T) {
	// Regular user login and human body: should NOT be detected as bot.
	patterns, _ := CompileBotPatterns([]string{`^\*\*\[`})

	if isBot("ytnobody", "User", "This is a human comment", patterns) {
		t.Error("expected isBot=false for human comment")
	}
}

func TestSyncer_CommentIsBot_Pattern(t *testing.T) {
	// End-to-end: Syncer should mark comments matching bot patterns as IsBot=true.
	dir := t.TempDir()
	store := issue.NewStore(dir)

	// Pre-create the issue
	iss := &issue.Issue{
		ID:     "owner-repo-001",
		Title:  "Test issue",
		Status: issue.StatusOpen,
	}
	if err := store.Update(iss); err != nil {
		t.Fatal(err)
	}

	patterns, err := CompileBotPatterns([]string{`^\*\*\[`})
	if err != nil {
		t.Fatal(err)
	}

	syncer := NewSyncer(store, "owner", []string{"repo"}, time.Minute).
		WithBotCommentPatterns(patterns)

	// Simulate raw comments
	humanComment := ghComment{
		ID:        1,
		Body:      "This is a human comment",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}
	humanComment.User.Login = "alice"
	humanComment.User.Type = "User"

	botComment := ghComment{
		ID:        2,
		Body:      "**[実装完了]** by `engineer-1`\n実装が完了しました。",
		CreatedAt: "2026-01-01T01:00:00Z",
		UpdatedAt: "2026-01-01T01:00:00Z",
	}
	botComment.User.Login = "ytnobody"
	botComment.User.Type = "User"

	// Inject comments directly via the internal helper (calling syncComments
	// would require a live gh CLI, so we call the store directly here).
	issLoaded, _ := store.Get("owner-repo-001")
	createdAt1, _ := time.Parse(time.RFC3339, humanComment.CreatedAt)
	updatedAt1, _ := time.Parse(time.RFC3339, humanComment.UpdatedAt)
	createdAt2, _ := time.Parse(time.RFC3339, botComment.CreatedAt)
	updatedAt2, _ := time.Parse(time.RFC3339, botComment.UpdatedAt)
	issLoaded.AddComment(issue.Comment{
		ID:        humanComment.ID,
		Author:    humanComment.User.Login,
		Body:      humanComment.Body,
		CreatedAt: createdAt1,
		UpdatedAt: updatedAt1,
		IsBot:     isBot(humanComment.User.Login, humanComment.User.Type, humanComment.Body, syncer.botPatterns),
	})
	issLoaded.AddComment(issue.Comment{
		ID:        botComment.ID,
		Author:    botComment.User.Login,
		Body:      botComment.Body,
		CreatedAt: createdAt2,
		UpdatedAt: updatedAt2,
		IsBot:     isBot(botComment.User.Login, botComment.User.Type, botComment.Body, syncer.botPatterns),
	})
	if err := store.Update(issLoaded); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Get("owner-repo-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(loaded.Comments))
	}
	if loaded.Comments[0].IsBot {
		t.Errorf("human comment should have IsBot=false")
	}
	if !loaded.Comments[1].IsBot {
		t.Errorf("bot comment (pattern match) should have IsBot=true")
	}
}

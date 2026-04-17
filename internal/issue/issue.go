package issue

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Comment represents a GitHub issue comment.
type Comment struct {
	ID        int64     `toml:"id"`
	Author    string    `toml:"author"`
	Body      string    `toml:"body"`
	CreatedAt time.Time `toml:"created_at"`
	UpdatedAt time.Time `toml:"updated_at"`
	// IsBot is true when the comment was posted by a bot account (e.g. a GitHub
	// App or an account whose login ends with "[bot]"). Human discussion comments
	// have IsBot=false, which allows the orchestrator and other consumers to
	// distinguish human-initiated discussions from automated agent status posts.
	IsBot bool `toml:"is_bot,omitempty"`
}

// IsBotLogin reports whether the given GitHub login belongs to a bot account.
// GitHub Apps and automation accounts typically end their login with "[bot]"
// (e.g. "github-actions[bot]", "dependabot[bot]").
func IsBotLogin(login string) bool {
	return strings.HasSuffix(login, "[bot]")
}

// Status represents the lifecycle state of an Issue.
// It is an iota-based sum type that prevents invalid status values from being
// constructed. TOML serialization uses the human-readable string form ("open",
// "in_progress", "resolved", "closed") via the encoding.TextMarshaler and
// encoding.TextUnmarshaler interfaces.
type Status int

const (
	StatusOpen       Status = iota // "open"
	StatusInProgress               // "in_progress"
	StatusResolved                 // "resolved"
	StatusClosed                   // "closed"
)

// statusStrings maps each Status value to its canonical string representation.
var statusStrings = [...]string{
	StatusOpen:       "open",
	StatusInProgress: "in_progress",
	StatusResolved:   "resolved",
	StatusClosed:     "closed",
}

// String returns the human-readable string representation of the Status.
// It implements the fmt.Stringer interface.
func (s Status) String() string {
	if int(s) < len(statusStrings) {
		return statusStrings[s]
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// IsTerminal reports whether s is a terminal (non-recoverable) status.
// StatusResolved and StatusClosed are terminal; StatusOpen and StatusInProgress are not.
func (s Status) IsTerminal() bool {
	return s == StatusResolved || s == StatusClosed
}

// MarshalText implements encoding.TextMarshaler.
// The TOML encoder calls this to serialize Status as a string.
func (s Status) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// The TOML decoder calls this to parse a string status value into Status.
func (s *Status) UnmarshalText(b []byte) error {
	text := string(b)
	for i, str := range statusStrings {
		if str == text {
			*s = Status(i)
			return nil
		}
	}
	return fmt.Errorf("unknown issue status: %q", text)
}

type Issue struct {
	ID           string `toml:"id"`
	Title        string `toml:"title"`
	URL          string `toml:"url,omitempty"`
	Status       Status `toml:"status"`
	AssignedTeam int    `toml:"assigned_team"`
	// PendingApproval is set to true when an issue is created by a user not in
	// authorized_users. The issue will not be assigned to a team until an
	// authorized user posts a comment containing "/approve".
	PendingApproval bool      `toml:"pending_approval,omitempty"`
	Repos           []string  `toml:"repos,omitempty"`
	Labels          []string  `toml:"labels,omitempty"`
	Body            string    `toml:"body"`
	Acceptance      string    `toml:"acceptance,omitempty"`
	Comments        []Comment `toml:"comments,omitempty"`
}

// NewIssue creates a new Issue with the given ID, title, and body.
// The returned Issue has Status=StatusOpen and AssignedTeam=0.
// Use this smart constructor to ensure a consistent initial state.
func NewIssue(id, title, body string) *Issue {
	return &Issue{
		ID:           id,
		Title:        title,
		Body:         body,
		Status:       StatusOpen,
		AssignedTeam: 0,
	}
}

// TransitionToInProgress returns a new Issue copy with Status=StatusInProgress
// and AssignedTeam set to teamID. Returns an error if iss is in a terminal state.
// This is a pure function: iss is not mutated.
func TransitionToInProgress(iss Issue, teamID int) (Issue, error) {
	if iss.Status.IsTerminal() {
		return iss, fmt.Errorf("cannot transition issue %s from terminal status %s to in_progress", iss.ID, iss.Status)
	}
	result := iss
	result.Status = StatusInProgress
	result.AssignedTeam = teamID
	return result, nil
}

// TransitionToOpen returns a new Issue copy with Status=StatusOpen and
// AssignedTeam=0. Only valid from StatusInProgress; returns an error otherwise.
// This is a pure function: iss is not mutated.
func TransitionToOpen(iss Issue) (Issue, error) {
	if iss.Status != StatusInProgress {
		return iss, fmt.Errorf("cannot transition issue %s from status %s to open (must be in_progress)", iss.ID, iss.Status)
	}
	result := iss
	result.Status = StatusOpen
	result.AssignedTeam = 0
	return result, nil
}

// TransitionToResolved returns a new Issue copy with Status=StatusResolved.
// Only valid from StatusInProgress; returns an error otherwise.
// This is a pure function: iss is not mutated.
func TransitionToResolved(iss Issue) (Issue, error) {
	if iss.Status != StatusInProgress {
		return iss, fmt.Errorf("cannot transition issue %s from status %s to resolved (must be in_progress)", iss.ID, iss.Status)
	}
	result := iss
	result.Status = StatusResolved
	return result, nil
}

// TransitionToClosed returns a new Issue copy with Status=StatusClosed.
// This transition is always valid regardless of the current status.
// This is a pure function: iss is not mutated.
func TransitionToClosed(iss Issue) (Issue, error) {
	result := iss
	result.Status = StatusClosed
	return result, nil
}

// MergeComments returns a new comment slice with c appended if c's ID is not
// already present. Returns (newSlice, true) when c was added, or (original, false)
// when c is a duplicate. The original slice is never modified (pure function).
func MergeComments(comments []Comment, c Comment) ([]Comment, bool) {
	for _, existing := range comments {
		if existing.ID == c.ID {
			return comments, false
		}
	}
	result := make([]Comment, len(comments)+1)
	copy(result, comments)
	result[len(comments)] = c
	return result, true
}

// HasComment checks whether a comment with the given ID already exists.
func (iss *Issue) HasComment(id int64) bool {
	for _, c := range iss.Comments {
		if c.ID == id {
			return true
		}
	}
	return false
}

// AddComment appends a comment if it doesn't already exist (dedup by ID).
// Returns true if the comment was added.
// Prefer the pure function MergeComments when immutability is desired.
func (iss *Issue) AddComment(c Comment) bool {
	if iss.HasComment(c.ID) {
		return false
	}
	iss.Comments = append(iss.Comments, c)
	return true
}

type StatusFilter struct {
	Status *Status // nil means all
}

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Dir() string {
	return s.dir
}

// Create creates a new local issue with auto-incremented ID.
func (s *Store) Create(title, body string) (*Issue, error) {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return nil, fmt.Errorf("create issues dir: %w", err)
	}

	nextNum, err := s.nextLocalNum()
	if err != nil {
		return nil, err
	}

	issue := &Issue{
		ID:           fmt.Sprintf("local-%03d", nextNum),
		Title:        title,
		Status:       StatusOpen,
		AssignedTeam: 0,
		Body:         body,
	}

	if err := s.write(issue); err != nil {
		return nil, err
	}
	return issue, nil
}

// Get retrieves an issue by ID.
func (s *Store) Get(id string) (*Issue, error) {
	path := s.path(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read issue %s: %w", id, err)
	}

	var issue Issue
	if err := toml.Unmarshal(data, &issue); err != nil {
		return nil, fmt.Errorf("parse issue %s: %w", id, err)
	}
	return &issue, nil
}

// List returns all issues, optionally filtered by status.
func (s *Store) List(filter StatusFilter) ([]*Issue, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read issues dir: %w", err)
	}

	var issues []*Issue
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".toml")
		issue, err := s.Get(id)
		if err != nil {
			continue
		}
		if filter.Status != nil && issue.Status != *filter.Status {
			continue
		}
		issues = append(issues, issue)
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ID < issues[j].ID
	})

	return issues, nil
}

// ListNew returns issues whose IDs are not in the known set.
func (s *Store) ListNew(known []string) ([]*Issue, error) {
	knownSet := make(map[string]struct{}, len(known))
	for _, id := range known {
		knownSet[id] = struct{}{}
	}

	all, err := s.List(StatusFilter{})
	if err != nil {
		return nil, err
	}

	var newIssues []*Issue
	for _, issue := range all {
		if _, ok := knownSet[issue.ID]; !ok {
			newIssues = append(newIssues, issue)
		}
	}
	return newIssues, nil
}

// Update writes the issue back to disk.
func (s *Store) Update(issue *Issue) error {
	return s.write(issue)
}

// Delete removes an issue file by ID. It is idempotent: deleting a
// non-existent issue returns nil.
func (s *Store) Delete(id string) error {
	err := os.Remove(s.path(id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete issue %s: %w", id, err)
	}
	return nil
}

func (s *Store) write(issue *Issue) error {
	path := s.path(issue.ID)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create issue file: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(issue); err != nil {
		return fmt.Errorf("write issue: %w", err)
	}
	return nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".toml")
}

func (s *Store) nextLocalNum() (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}

	maxNum := 0
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".toml")
		if !strings.HasPrefix(name, "local-") {
			continue
		}
		numStr := strings.TrimPrefix(name, "local-")
		num, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		if num > maxNum {
			maxNum = num
		}
	}
	return maxNum + 1, nil
}

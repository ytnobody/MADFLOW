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

type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusResolved   Status = "resolved"
	StatusClosed     Status = "closed"
)

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
	if err := os.MkdirAll(s.dir, 0755); err != nil {
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

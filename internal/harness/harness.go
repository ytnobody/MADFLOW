// Package harness implements automatic generation of harnesses (reproduction
// tests, failure patterns, prompt improvement material) from agent failure logs.
//
// While internal/lessons/ produces human-readable lessons for the Superintendent,
// the harness system produces real code assets to prevent regressions in CI.
package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ytnobody/madflow/internal/lessons"
)

// FailureCase represents a single captured failure event.
type FailureCase struct {
	ID        string            `json:"id"`
	IssueID   string            `json:"issue_id"`
	Timestamp time.Time         `json:"timestamp"`
	Score     int               `json:"score"`
	Failures  []lessons.Failure `json:"failures"`
	Pattern   string            `json:"pattern"`
	Prompt    string            `json:"prompt,omitempty"`
	Output    string            `json:"output,omitempty"`
}

// PatternStats holds aggregated statistics for a single failure pattern.
type PatternStats struct {
	Pattern  string    `json:"pattern"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

// PatternsFile is the top-level structure of patterns.json.
type PatternsFile struct {
	UpdatedAt time.Time               `json:"updated_at"`
	Patterns  map[string]PatternStats `json:"patterns"`
}

// Manager manages the harness lifecycle.
type Manager struct {
	// DataDir is the MADFLOW data directory (e.g. ~/.madflow/MADFLOW).
	DataDir string
	// RepoDir is the target repository root directory (for PR creation).
	RepoDir string
	// Owner is the GitHub owner of the target repository.
	Owner string
	// Repo is the GitHub repo name of the target repository.
	Repo string
	// AnthropicAPIKey is the API key for LLM-based test generation.
	// When empty, os.Getenv("ANTHROPIC_API_KEY") is used. When that is also
	// empty, a template-based fallback is used.
	AnthropicAPIKey string
}

// HarnessDir returns the path to the harness data directory.
func (m *Manager) HarnessDir() string {
	return filepath.Join(m.DataDir, "harness")
}

// CasesDir returns the path to the cases directory.
func (m *Manager) CasesDir() string {
	return filepath.Join(m.HarnessDir(), "cases")
}

// PatternsPath returns the path to patterns.json.
func (m *Manager) PatternsPath() string {
	return filepath.Join(m.HarnessDir(), "patterns.json")
}

// ProcessScoringResult processes a ScoringResult, persists the failure case,
// and updates the pattern statistics. It is a no-op when result is nil or has
// no failures.
func (m *Manager) ProcessScoringResult(result *lessons.ScoringResult, prompt, output string) error {
	if result == nil || len(result.Failures) == 0 {
		return nil
	}

	caseID := generateCaseID(result.IssueID)
	fc := FailureCase{
		ID:        caseID,
		IssueID:   result.IssueID,
		Timestamp: time.Now(),
		Score:     result.Score,
		Failures:  result.Failures,
		Pattern:   ClassifyPattern(result.Failures),
		Prompt:    prompt,
		Output:    output,
	}

	if err := m.saveCase(fc); err != nil {
		return fmt.Errorf("save case %s: %w", caseID, err)
	}

	if err := m.updatePatterns(fc); err != nil {
		return fmt.Errorf("update patterns for case %s: %w", caseID, err)
	}

	return nil
}

// ClassifyPattern applies rule-based classification to a set of failures and
// returns a pattern key. When multiple patterns match, the highest-risk one
// takes precedence. Returns "unknown" for unrecognized or empty failure sets.
func ClassifyPattern(failures []lessons.Failure) string {
	if len(failures) == 0 {
		return "unknown"
	}

	// Map known descriptions to pattern keys.
	type match struct {
		keyword string
		pattern string
	}
	knownPatterns := []match{
		{"派生・修正Issue", "derived_issue"},
		{"Clarification Needed", "clarification_needed"},
		{"Superintendentが直接実装", "superintendent_direct_impl"},
		{"PRが2本以上", "multiple_prs"},
	}

	var matched []string
	for _, f := range failures {
		for _, kp := range knownPatterns {
			if strings.Contains(f.Description, kp.keyword) {
				matched = append(matched, kp.pattern)
				break
			}
		}
	}

	switch len(matched) {
	case 0:
		return "unknown"
	case 1:
		return matched[0]
	default:
		return "multiple_failures"
	}
}

// generateCaseID produces a unique case ID based on the issue ID and the
// current time in nanoseconds.
func generateCaseID(issueID string) string {
	return fmt.Sprintf("%s-%d", issueID, time.Now().UnixNano())
}

// saveCase persists a FailureCase to its directory under CasesDir.
//
// Directory layout:
//
//	<CasesDir>/<case.ID>/
//	  metadata.json   — FailureCase (without Prompt/Output fields)
//	  prompt.txt      — prompt snapshot (only when non-empty)
//	  output.txt      — output snapshot (only when non-empty)
func (m *Manager) saveCase(fc FailureCase) error {
	caseDir := filepath.Join(m.CasesDir(), fc.ID)
	if err := os.MkdirAll(caseDir, 0700); err != nil {
		return fmt.Errorf("mkdir case dir %s: %w", caseDir, err)
	}

	// Save metadata.json (without Prompt/Output to avoid duplication).
	meta := fc
	meta.Prompt = ""
	meta.Output = ""
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "metadata.json"), metaData, 0600); err != nil {
		return fmt.Errorf("write metadata.json: %w", err)
	}

	// Save prompt.txt only when non-empty.
	if fc.Prompt != "" {
		if err := os.WriteFile(filepath.Join(caseDir, "prompt.txt"), []byte(fc.Prompt), 0600); err != nil {
			return fmt.Errorf("write prompt.txt: %w", err)
		}
	}

	// Save output.txt only when non-empty.
	if fc.Output != "" {
		if err := os.WriteFile(filepath.Join(caseDir, "output.txt"), []byte(fc.Output), 0600); err != nil {
			return fmt.Errorf("write output.txt: %w", err)
		}
	}

	return nil
}

// updatePatterns loads patterns.json, increments the count for the case's
// pattern, and saves it back.
func (m *Manager) updatePatterns(fc FailureCase) error {
	pf, err := LoadPatterns(m.PatternsPath())
	if err != nil {
		return fmt.Errorf("load patterns: %w", err)
	}

	stats := pf.Patterns[fc.Pattern]
	stats.Pattern = fc.Pattern
	stats.Count++
	if fc.Timestamp.After(stats.LastSeen) {
		stats.LastSeen = fc.Timestamp
	}
	pf.Patterns[fc.Pattern] = stats
	pf.UpdatedAt = time.Now()

	return savePatterns(m.PatternsPath(), pf)
}

// LoadPatterns reads patterns.json from path. Returns an empty PatternsFile
// (not an error) when the file does not exist.
func LoadPatterns(path string) (*PatternsFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &PatternsFile{Patterns: make(map[string]PatternStats)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read patterns file %s: %w", path, err)
	}

	var pf PatternsFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("unmarshal patterns file %s: %w", path, err)
	}
	if pf.Patterns == nil {
		pf.Patterns = make(map[string]PatternStats)
	}
	return &pf, nil
}

// savePatterns writes a PatternsFile to path using atomic write.
func savePatterns(path string, pf *PatternsFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir for patterns file: %w", err)
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal patterns: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write tmp patterns file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename patterns file: %w", err)
	}
	return nil
}

// GenerateTestDraft generates a Go test function draft for the given
// FailureCase. It calls the Anthropic API when an API key is available;
// otherwise it falls back to a template-based stub.
func (m *Manager) GenerateTestDraft(fc FailureCase) (string, error) {
	apiKey := m.AnthropicAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return templateTestDraft(fc), nil
	}

	prompt := buildTestDraftPrompt(fc)
	text, err := callAnthropicSimple(apiKey, prompt)
	if err != nil {
		// Fall back to template on API error.
		return templateTestDraft(fc), nil
	}
	return text, nil
}

// templateTestDraft returns a basic Go test stub without calling an LLM.
func templateTestDraft(fc FailureCase) string {
	var sb strings.Builder
	sb.WriteString("package harness_test\n\n")
	sb.WriteString("import \"testing\"\n\n")
	fmt.Fprintf(&sb, "// TestHarness_%s is an auto-generated reproduction test stub\n", fc.ID)
	fmt.Fprintf(&sb, "// for failure case %s (issue: %s, score: %d).\n", fc.ID, fc.IssueID, fc.Score)
	sb.WriteString("// TODO: fill in the actual reproduction steps based on the failure case.\n")
	fmt.Fprintf(&sb, "func TestHarness_%s(t *testing.T) {\n", fc.ID)
	sb.WriteString("\tt.Skip(\"auto-generated stub — fill in reproduction steps\")\n")
	sb.WriteString("}\n")
	return sb.String()
}

// buildTestDraftPrompt builds the prompt for LLM-based test generation.
func buildTestDraftPrompt(fc FailureCase) string {
	var failureLines strings.Builder
	for _, f := range fc.Failures {
		fmt.Fprintf(&failureLines, "- [%s] %s\n", f.Risk, f.Description)
	}

	var contextSection strings.Builder
	if fc.Prompt != "" {
		fmt.Fprintf(&contextSection, "\n## Prompt Snapshot\n\n```\n%s\n```\n", fc.Prompt)
	}
	if fc.Output != "" {
		fmt.Fprintf(&contextSection, "\n## Output Snapshot\n\n```\n%s\n```\n", fc.Output)
	}

	return fmt.Sprintf(`You are a Go test engineer. Generate a Go test function that reproduces the following agent failure case.

## Failure Case

- Case ID: %s
- Issue ID: %s
- Score: %d/100
- Pattern: %s
- Failures:
%s%s
## Requirements

- Write a single Go test function named TestHarness_%s
- Package: package harness_test
- Include appropriate imports
- Add a TODO comment explaining what needs to be filled in
- The test should document the failure scenario even if it calls t.Skip()
- Output only valid Go code, no markdown fences

`,
		fc.ID, fc.IssueID, fc.Score, fc.Pattern,
		failureLines.String(), contextSection.String(), fc.ID)
}

// ProposePR generates a Go test draft for the given FailureCase and creates a
// GitHub PR in the target repository. It requires RepoDir, Owner, and Repo to
// be set on the Manager.
//
// The generated test is written to testdata/harness/<caseID>_test.go in the
// target repository, committed on a branch named harness/<caseID>, and a PR is
// opened against the develop branch.
func (m *Manager) ProposePR(fc FailureCase) error {
	if m.RepoDir == "" {
		return fmt.Errorf("RepoDir is required for ProposePR")
	}
	if m.Owner == "" || m.Repo == "" {
		return fmt.Errorf("Owner and Repo are required for ProposePR")
	}

	draft, err := m.GenerateTestDraft(fc)
	if err != nil {
		return fmt.Errorf("generate test draft: %w", err)
	}

	// Write the test file into the target repository.
	testDir := filepath.Join(m.RepoDir, "testdata", "harness")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return fmt.Errorf("mkdir testdata/harness: %w", err)
	}
	testFile := filepath.Join(testDir, fc.ID+"_test.go")
	if err := os.WriteFile(testFile, []byte(draft), 0644); err != nil {
		return fmt.Errorf("write test file: %w", err)
	}

	branchName := "harness/" + fc.ID
	fullRepo := m.Owner + "/" + m.Repo

	// Create and push the branch.
	if err := runGit(m.RepoDir, "checkout", "-b", branchName); err != nil {
		return fmt.Errorf("git checkout -b %s: %w", branchName, err)
	}
	if err := runGit(m.RepoDir, "add", testFile); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	commitMsg := fmt.Sprintf("test: add harness stub for case %s (issue %s)", fc.ID, fc.IssueID)
	if err := runGit(m.RepoDir, "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := runGit(m.RepoDir, "push", "-u", "origin", branchName); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	// Open PR via gh CLI.
	prTitle := fmt.Sprintf("harness: add reproduction test stub for case %s", fc.ID)
	prBody := fmt.Sprintf("Auto-generated harness test stub for failure case `%s` (issue: `%s`, score: %d).\n\n"+
		"Pattern: `%s`\n\nTODO: fill in the actual reproduction steps.",
		fc.ID, fc.IssueID, fc.Score, fc.Pattern)
	if err := runGh("pr", "create",
		"-R", fullRepo,
		"--base", "develop",
		"--head", branchName,
		"--title", prTitle,
		"--body", prBody,
	); err != nil {
		return fmt.Errorf("gh pr create: %w", err)
	}

	return nil
}

// --- Anthropic API helpers (duplicated from lessons to avoid coupling) ---

const (
	harnessAnthropicEndpoint   = "https://api.anthropic.com/v1/messages"
	harnessAnthropicAPIVersion = "2023-06-01"
	harnessModel               = "claude-haiku-4-5"
)

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// callAnthropicSimple makes a simple (non-agentic) Anthropic API call.
func callAnthropicSimple(apiKey, prompt string) (string, error) {
	reqBody := anthropicRequest{
		Model:     harnessModel,
		MaxTokens: 2048,
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal anthropic request: %w", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodPost, harnessAnthropicEndpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create anthropic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", harnessAnthropicAPIVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic API call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read anthropic response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("unmarshal anthropic response: %w", err)
	}
	if apiResp.Error != nil {
		return "", fmt.Errorf("anthropic API error (%s): %s", apiResp.Error.Type, apiResp.Error.Message)
	}
	for _, block := range apiResp.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in anthropic response")
}

// --- git / gh helpers ---

// runGit runs a git command in the given directory.
func runGit(dir string, args ...string) error {
	return runCmd(dir, "git", args...)
}

// runGh runs a gh CLI command (no working directory needed).
func runGh(args ...string) error {
	return runCmd("", "gh", args...)
}

// runCmd executes an external command and returns a combined error on failure.
func runCmd(dir, name string, args ...string) error {
	// Import exec via os/exec indirection to keep the package testable.
	// We use a package-level variable so tests can swap the implementation.
	return defaultRunner(dir, name, args...)
}

package harness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ytnobody/madflow/internal/harness"
	"github.com/ytnobody/madflow/internal/lessons"
)

// newTestManager returns a Manager backed by a temporary directory.
func newTestManager(t *testing.T) *harness.Manager {
	t.Helper()
	dir := t.TempDir()
	return &harness.Manager{
		DataDir: dir,
		Owner:   "testowner",
		Repo:    "testrepo",
	}
}

// newScoringResult builds a minimal ScoringResult for tests.
func newScoringResult(issueID string, score int, failures []lessons.Failure) *lessons.ScoringResult {
	return &lessons.ScoringResult{
		IssueID:  issueID,
		Score:    score,
		Failures: failures,
	}
}

// --- ClassifyPattern ---

func TestClassifyPattern_DerivedIssue(t *testing.T) {
	failures := []lessons.Failure{
		{Description: "派生・修正Issueが発生した", Risk: lessons.RiskHigh, Points: 30},
	}
	got := harness.ClassifyPattern(failures)
	if got != "derived_issue" {
		t.Errorf("ClassifyPattern() = %q, want %q", got, "derived_issue")
	}
}

func TestClassifyPattern_ClarificationNeeded(t *testing.T) {
	failures := []lessons.Failure{
		{Description: "[Clarification Needed] コメントが存在した", Risk: lessons.RiskMedium, Points: 20},
	}
	got := harness.ClassifyPattern(failures)
	if got != "clarification_needed" {
		t.Errorf("ClassifyPattern() = %q, want %q", got, "clarification_needed")
	}
}

func TestClassifyPattern_SuperintendentDirectImpl(t *testing.T) {
	failures := []lessons.Failure{
		{Description: "Superintendentが直接実装した", Risk: lessons.RiskMedium, Points: 20},
	}
	got := harness.ClassifyPattern(failures)
	if got != "superintendent_direct_impl" {
		t.Errorf("ClassifyPattern() = %q, want %q", got, "superintendent_direct_impl")
	}
}

func TestClassifyPattern_MultiplePRs(t *testing.T) {
	failures := []lessons.Failure{
		{Description: "PRが2本以上作成された", Risk: lessons.RiskLow, Points: 15},
	}
	got := harness.ClassifyPattern(failures)
	if got != "multiple_prs" {
		t.Errorf("ClassifyPattern() = %q, want %q", got, "multiple_prs")
	}
}

func TestClassifyPattern_MultipleFailures(t *testing.T) {
	failures := []lessons.Failure{
		{Description: "[Clarification Needed] コメントが存在した", Risk: lessons.RiskMedium, Points: 20},
		{Description: "PRが2本以上作成された", Risk: lessons.RiskLow, Points: 15},
	}
	got := harness.ClassifyPattern(failures)
	if got != "multiple_failures" {
		t.Errorf("ClassifyPattern() = %q, want %q", got, "multiple_failures")
	}
}

func TestClassifyPattern_Unknown(t *testing.T) {
	failures := []lessons.Failure{
		{Description: "some unrecognized failure", Risk: lessons.RiskLow, Points: 10},
	}
	got := harness.ClassifyPattern(failures)
	if got != "unknown" {
		t.Errorf("ClassifyPattern() = %q, want %q", got, "unknown")
	}
}

func TestClassifyPattern_Empty(t *testing.T) {
	got := harness.ClassifyPattern(nil)
	if got != "unknown" {
		t.Errorf("ClassifyPattern(nil) = %q, want %q", got, "unknown")
	}
}

// --- ProcessScoringResult ---

func TestProcessScoringResult_NilResult(t *testing.T) {
	m := newTestManager(t)
	err := m.ProcessScoringResult(nil, "", "")
	if err != nil {
		t.Errorf("ProcessScoringResult(nil) returned error: %v", err)
	}
}

func TestProcessScoringResult_NoFailures(t *testing.T) {
	m := newTestManager(t)
	result := newScoringResult("test-001", 100, nil)
	err := m.ProcessScoringResult(result, "", "")
	if err != nil {
		t.Errorf("ProcessScoringResult with no failures returned error: %v", err)
	}
	// No case should be saved
	entries, _ := os.ReadDir(m.CasesDir())
	if len(entries) != 0 {
		t.Errorf("expected no case directories, got %d", len(entries))
	}
}

func TestProcessScoringResult_SavesCase(t *testing.T) {
	m := newTestManager(t)
	failures := []lessons.Failure{
		{Description: "派生・修正Issueが発生した", Risk: lessons.RiskHigh, Points: 30},
	}
	result := newScoringResult("issue-001", 70, failures)

	if err := m.ProcessScoringResult(result, "my prompt", "my output"); err != nil {
		t.Fatalf("ProcessScoringResult failed: %v", err)
	}

	// A case directory should exist
	entries, err := os.ReadDir(m.CasesDir())
	if err != nil {
		t.Fatalf("ReadDir CasesDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 case directory, got %d", len(entries))
	}

	caseDir := filepath.Join(m.CasesDir(), entries[0].Name())

	// metadata.json
	metaData, err := os.ReadFile(filepath.Join(caseDir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var fc harness.FailureCase
	if err := json.Unmarshal(metaData, &fc); err != nil {
		t.Fatalf("unmarshal metadata.json: %v", err)
	}
	if fc.IssueID != "issue-001" {
		t.Errorf("IssueID = %q, want %q", fc.IssueID, "issue-001")
	}
	if fc.Score != 70 {
		t.Errorf("Score = %d, want 70", fc.Score)
	}
	if fc.Pattern != "derived_issue" {
		t.Errorf("Pattern = %q, want %q", fc.Pattern, "derived_issue")
	}

	// prompt.txt
	promptData, err := os.ReadFile(filepath.Join(caseDir, "prompt.txt"))
	if err != nil {
		t.Fatalf("read prompt.txt: %v", err)
	}
	if string(promptData) != "my prompt" {
		t.Errorf("prompt.txt content = %q, want %q", string(promptData), "my prompt")
	}

	// output.txt
	outputData, err := os.ReadFile(filepath.Join(caseDir, "output.txt"))
	if err != nil {
		t.Fatalf("read output.txt: %v", err)
	}
	if string(outputData) != "my output" {
		t.Errorf("output.txt content = %q, want %q", string(outputData), "my output")
	}
}

func TestProcessScoringResult_EmptyPromptOutput(t *testing.T) {
	m := newTestManager(t)
	failures := []lessons.Failure{
		{Description: "PRが2本以上作成された", Risk: lessons.RiskLow, Points: 15},
	}
	result := newScoringResult("issue-002", 85, failures)

	if err := m.ProcessScoringResult(result, "", ""); err != nil {
		t.Fatalf("ProcessScoringResult failed: %v", err)
	}

	entries, err := os.ReadDir(m.CasesDir())
	if err != nil {
		t.Fatalf("ReadDir CasesDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 case directory, got %d", len(entries))
	}
	caseDir := filepath.Join(m.CasesDir(), entries[0].Name())

	// prompt.txt should not exist when prompt is empty
	if _, err := os.Stat(filepath.Join(caseDir, "prompt.txt")); !os.IsNotExist(err) {
		t.Errorf("expected prompt.txt to not exist, got err: %v", err)
	}
	// output.txt should not exist when output is empty
	if _, err := os.Stat(filepath.Join(caseDir, "output.txt")); !os.IsNotExist(err) {
		t.Errorf("expected output.txt to not exist, got err: %v", err)
	}
}

// --- updatePatterns / LoadPatterns ---

func TestProcessScoringResult_UpdatesPatterns(t *testing.T) {
	m := newTestManager(t)
	failures := []lessons.Failure{
		{Description: "派生・修正Issueが発生した", Risk: lessons.RiskHigh, Points: 30},
	}

	// Process two cases with the same pattern
	result1 := newScoringResult("issue-001", 70, failures)
	if err := m.ProcessScoringResult(result1, "", ""); err != nil {
		t.Fatalf("ProcessScoringResult #1 failed: %v", err)
	}
	result2 := newScoringResult("issue-002", 60, failures)
	if err := m.ProcessScoringResult(result2, "", ""); err != nil {
		t.Fatalf("ProcessScoringResult #2 failed: %v", err)
	}

	pf, err := harness.LoadPatterns(m.PatternsPath())
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	stats, ok := pf.Patterns["derived_issue"]
	if !ok {
		t.Fatalf("expected pattern %q in patterns.json", "derived_issue")
	}
	if stats.Count != 2 {
		t.Errorf("Count = %d, want 2", stats.Count)
	}
}

func TestLoadPatterns_NonExistent(t *testing.T) {
	pf, err := harness.LoadPatterns("/tmp/nonexistent-patterns-xyz.json")
	if err != nil {
		t.Fatalf("LoadPatterns on non-existent file: %v", err)
	}
	if pf == nil {
		t.Fatal("LoadPatterns returned nil PatternsFile")
	}
	if len(pf.Patterns) != 0 {
		t.Errorf("expected empty patterns, got %v", pf.Patterns)
	}
}

// --- Manager path helpers ---

func TestManagerPaths(t *testing.T) {
	m := &harness.Manager{DataDir: "/tmp/testdata"}
	if !strings.HasSuffix(m.HarnessDir(), "harness") {
		t.Errorf("HarnessDir() = %q, expected suffix 'harness'", m.HarnessDir())
	}
	if !strings.HasSuffix(m.CasesDir(), "cases") {
		t.Errorf("CasesDir() = %q, expected suffix 'cases'", m.CasesDir())
	}
	if !strings.HasSuffix(m.PatternsPath(), "patterns.json") {
		t.Errorf("PatternsPath() = %q, expected suffix 'patterns.json'", m.PatternsPath())
	}
}

// --- GenerateTestDraft ---

func TestGenerateTestDraft_NoAPIKey(t *testing.T) {
	m := &harness.Manager{
		DataDir: t.TempDir(),
		// AnthropicAPIKey intentionally left empty
	}
	fc := harness.FailureCase{
		ID:        "issue-001-12345",
		IssueID:   "issue-001",
		Timestamp: time.Now(),
		Score:     70,
		Failures: []lessons.Failure{
			{Description: "派生・修正Issueが発生した", Risk: lessons.RiskHigh, Points: 30},
		},
		Pattern: "derived_issue",
	}

	draft, err := m.GenerateTestDraft(fc)
	if err != nil {
		t.Fatalf("GenerateTestDraft: %v", err)
	}
	// Fallback template should produce a non-empty Go test stub
	if !strings.Contains(draft, "func Test") {
		t.Errorf("expected draft to contain 'func Test', got:\n%s", draft)
	}
	if !strings.Contains(draft, fc.ID) {
		t.Errorf("expected draft to contain case ID %q, got:\n%s", fc.ID, draft)
	}
}

// Package lessons implements the feedback loop that scores issue instruction
// quality after a PR merge, generates lessons from failures, and injects
// accumulated lessons into the Superintendent's patrol prompts.
package lessons

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RiskLevel represents the severity level of a lesson.
type RiskLevel string

const (
	RiskHigh   RiskLevel = "高"
	RiskMedium RiskLevel = "中"
	RiskLow    RiskLevel = "低"
)

// riskOrder maps risk levels to numeric priority (higher = more important to keep).
var riskOrder = map[RiskLevel]int{
	RiskHigh:   3,
	RiskMedium: 2,
	RiskLow:    1,
}

// Failure represents a quality failure detected during issue scoring.
type Failure struct {
	Description string
	Risk        RiskLevel
	Points      int // points deducted
}

// ScoringResult holds the result of scoring an issue's instruction quality.
type ScoringResult struct {
	IssueID  string
	Score    int
	Failures []Failure
}

// Lesson represents a single lesson entry.
type Lesson struct {
	Risk RiskLevel
	Text string
}

// maxLessons is the maximum number of lessons kept in the file.
const maxLessons = 15

// lessonsMgmtModel is the Anthropic model used for lesson generation and merging.
const lessonsMgmtModel = "claude-haiku-4-5"

// anthropicEndpoint is the Anthropic Messages API endpoint.
const anthropicEndpoint = "https://api.anthropic.com/v1/messages"

// anthropicAPIVersion is the Anthropic API version header value.
const anthropicAPIVersion = "2023-06-01"

// ParseLesson parses a lesson line in the format "[危険度] text".
// Returns an error for lines that do not match the expected format.
func ParseLesson(line string) (Lesson, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") {
		return Lesson{}, fmt.Errorf("invalid lesson format (missing leading '['): %q", line)
	}
	end := strings.Index(line, "]")
	if end == -1 {
		return Lesson{}, fmt.Errorf("invalid lesson format (missing ']'): %q", line)
	}
	risk := RiskLevel(line[1:end])
	text := strings.TrimSpace(line[end+1:])
	return Lesson{Risk: risk, Text: text}, nil
}

// FormatLesson formats a lesson as "[危険度] text".
func FormatLesson(l Lesson) string {
	return fmt.Sprintf("[%s] %s", l.Risk, l.Text)
}

// LoadLessons reads lessons from a file.
// Returns an empty slice (not an error) when the file does not exist.
func LoadLessons(path string) ([]Lesson, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read lessons file %s: %w", path, err)
	}

	var lessons []Lesson
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		l, err := ParseLesson(line)
		if err != nil {
			log.Printf("[lessons] skipping malformed line: %v", err)
			continue
		}
		lessons = append(lessons, l)
	}
	return lessons, scanner.Err()
}

// SaveLessons writes the given lessons to a file, overwriting any existing content.
func SaveLessons(path string, lessons []Lesson) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir for lessons file: %w", err)
	}
	var sb strings.Builder
	for _, l := range lessons {
		sb.WriteString(FormatLesson(l))
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0600)
}

// AppendLesson appends a single lesson to the file.
func AppendLesson(path string, lesson Lesson) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir for lessons file: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open lessons file: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, FormatLesson(lesson))
	return err
}

// Manager handles lesson scoring, generation, and management.
type Manager struct {
	// DataDir is the MADFLOW data directory (e.g. ~/.madflow/MADFLOW).
	DataDir string
	// AnthropicAPIKey is the API key for LLM-based lesson generation and merging.
	// If empty, template-based fallback is used for generation, and simple
	// risk-based trimming is used instead of LLM merging.
	AnthropicAPIKey string
	// FeaturePrefix is the feature branch prefix (e.g. "feature/issue-").
	FeaturePrefix string
}

// LessonsPath returns the absolute path to the lessons file.
func (m *Manager) LessonsPath() string {
	return filepath.Join(m.DataDir, "lessons.txt")
}

// InjectLessons returns a formatted string of all lessons suitable for
// prepending to the Superintendent's patrol prompt.
// Returns an empty string when no lessons exist.
func (m *Manager) InjectLessons() string {
	lessons, err := LoadLessons(m.LessonsPath())
	if err != nil {
		log.Printf("[lessons] InjectLessons: load failed: %v", err)
		return ""
	}
	if len(lessons) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 過去の失敗から学んだ教訓\n\n")
	sb.WriteString("以下は過去のIssue指示品質の採点で70点未満だったIssueから生成された教訓です。\n")
	sb.WriteString("Issueをエンジニアに割り当てる際は必ずこれらの教訓を参照し、同じ失敗を繰り返さないようにしてください：\n\n")
	for _, l := range lessons {
		sb.WriteString(FormatLesson(l))
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
	return sb.String()
}

// ProcessMergedIssue scores the issue, generates a lesson if needed, and
// manages the lesson count. It is a no-op for local issues (id prefix "local-").
func (m *Manager) ProcessMergedIssue(issueID, owner, repo string, issueNumber int) error {
	// Only process GitHub-synced issues
	if strings.HasPrefix(issueID, "local-") || owner == "" || repo == "" || issueNumber <= 0 {
		log.Printf("[lessons] skipping local/invalid issue %s", issueID)
		return nil
	}

	result, err := ScoreIssue(issueID, owner, repo, issueNumber)
	if err != nil {
		return fmt.Errorf("score issue %s: %w", issueID, err)
	}

	log.Printf("[lessons] issue %s scored %d/100 (failures: %d)", issueID, result.Score, len(result.Failures))

	if result.Score >= 70 || len(result.Failures) == 0 {
		log.Printf("[lessons] issue %s passed quality threshold (score=%d), no lesson generated", issueID, result.Score)
		return nil
	}

	// Generate a lesson
	lesson, err := m.generateLesson(result)
	if err != nil {
		log.Printf("[lessons] lesson generation failed for %s (using fallback): %v", issueID, err)
		lesson = fallbackLesson(result.Failures)
	}

	log.Printf("[lessons] generated lesson for %s: %s", issueID, FormatLesson(lesson))

	// Append the lesson to the file
	if err := AppendLesson(m.LessonsPath(), lesson); err != nil {
		return fmt.Errorf("append lesson: %w", err)
	}

	// Manage the 15-lesson limit
	if err := m.manageLessonsCount(); err != nil {
		log.Printf("[lessons] manageLessonsCount: %v (continuing)", err)
	}

	return nil
}

// ScoreIssue scores the instruction quality of an issue using GitHub API.
// Returns an error for local issues (owner == "").
func ScoreIssue(issueID, owner, repo string, issueNumber int) (*ScoringResult, error) {
	if owner == "" || repo == "" || issueNumber <= 0 {
		return nil, fmt.Errorf("cannot score local issue %q: missing GitHub coordinates", issueID)
	}

	result := &ScoringResult{
		IssueID: issueID,
		Score:   100,
	}

	fullRepo := fmt.Sprintf("%s/%s", owner, repo)

	// Check for derived/fix issues (-30, 高)
	if hasDerivedIssues(fullRepo, issueNumber) {
		result.Failures = append(result.Failures, Failure{
			Description: "派生・修正Issueが発生した",
			Risk:        RiskHigh,
			Points:      30,
		})
		result.Score -= 30
	}

	// Check for [Clarification Needed] comments (-20, 中)
	if hasClarificationNeeded(fullRepo, issueNumber) {
		result.Failures = append(result.Failures, Failure{
			Description: "[Clarification Needed] コメントが存在した",
			Risk:        RiskMedium,
			Points:      20,
		})
		result.Score -= 20
	}

	// Check for Superintendent direct implementation (-20, 中)
	featureHead := fmt.Sprintf("feature/issue-%s", issueID)
	if hasSuperintendentDirectImpl(fullRepo, featureHead) {
		result.Failures = append(result.Failures, Failure{
			Description: "Superintendentが直接実装した",
			Risk:        RiskMedium,
			Points:      20,
		})
		result.Score -= 20
	}

	// Check for multiple PRs (-15, 低)
	if hasMultiplePRs(fullRepo, featureHead) {
		result.Failures = append(result.Failures, Failure{
			Description: "PRが2本以上作成された",
			Risk:        RiskLow,
			Points:      15,
		})
		result.Score -= 15
	}

	if result.Score < 0 {
		result.Score = 0
	}

	return result, nil
}

// hasDerivedIssues checks whether any GitHub issues reference the given issue number.
func hasDerivedIssues(fullRepo string, issueNumber int) bool {
	query := fmt.Sprintf("%d in:body", issueNumber)
	cmd := exec.Command("gh", "issue", "list", "-R", fullRepo,
		"--search", query,
		"--json", "number",
		"--limit", "10",
		"--state", "all",
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	var issues []struct{ Number int }
	if err := json.Unmarshal(out, &issues); err != nil {
		return false
	}
	// Filter out the original issue itself
	for _, iss := range issues {
		if iss.Number != issueNumber {
			return true
		}
	}
	return false
}

// hasClarificationNeeded checks whether the issue has a [Clarification Needed] comment.
func hasClarificationNeeded(fullRepo string, issueNumber int) bool {
	endpoint := fmt.Sprintf("repos/%s/issues/%d/comments", fullRepo, issueNumber)
	cmd := exec.Command("gh", "api", endpoint, "--jq", ".[].body")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "[Clarification Needed]")
}

// hasSuperintendentDirectImpl checks whether any PR for this branch was
// implemented directly by the Superintendent (indicated by PR body text).
func hasSuperintendentDirectImpl(fullRepo, featureHead string) bool {
	cmd := exec.Command("gh", "pr", "list",
		"-R", fullRepo,
		"--search", fmt.Sprintf("head:%s", featureHead),
		"--json", "body",
		"--state", "all",
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	body := string(out)
	return strings.Contains(body, "Superintendent implemented directly") ||
		strings.Contains(body, "Superintendentが直接実装") ||
		strings.Contains(body, "The Superintendent implemented directly")
}

// hasMultiplePRs checks whether 2 or more PRs were created for this feature branch.
func hasMultiplePRs(fullRepo, featureHead string) bool {
	cmd := exec.Command("gh", "pr", "list",
		"-R", fullRepo,
		"--search", fmt.Sprintf("head:%s", featureHead),
		"--json", "number",
		"--state", "all",
		"--limit", "10",
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	var prs []struct{ Number int }
	if err := json.Unmarshal(out, &prs); err != nil {
		return false
	}
	return len(prs) >= 2
}

// generateLesson calls the Anthropic API to generate a 1-line lesson in Japanese.
// Returns an error when the API key is not available or the call fails.
func (m *Manager) generateLesson(result *ScoringResult) (Lesson, error) {
	apiKey := m.AnthropicAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return Lesson{}, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	// Determine the highest-risk failure
	highestRisk := RiskLow
	for _, f := range result.Failures {
		if riskOrder[f.Risk] > riskOrder[highestRisk] {
			highestRisk = f.Risk
		}
	}

	var failureLines strings.Builder
	for _, f := range result.Failures {
		fmt.Fprintf(&failureLines, "- [%s] %s\n", f.Risk, f.Description)
	}

	prompt := fmt.Sprintf(`あなたはソフトウェア開発プロジェクトの品質改善アドバイザーです。

以下のIssue指示品質の採点結果（%d/100点）を見て、同じ失敗を繰り返さないための教訓を1行の日本語で生成してください。

検出された失敗：
%s
条件：
- 教訓は「次回のIssue指示でどうすればよかったか」を具体的に述べること
- 1行、50文字以内で簡潔に
- 「〜すること」「〜を確認すること」などの行動指針の形式で
- 教訓テキストのみを出力（角括弧や危険度は含めない）

教訓：`, result.Score, failureLines.String())

	text, err := callAnthropicSimple(apiKey, prompt)
	if err != nil {
		return Lesson{}, err
	}

	text = strings.TrimSpace(text)
	// Remove any accidental "[高]" prefix the model might add
	if i := strings.Index(text, "] "); i >= 0 && strings.HasPrefix(text, "[") {
		text = strings.TrimSpace(text[i+2:])
	}

	return Lesson{Risk: highestRisk, Text: text}, nil
}

// fallbackLesson returns a template-based lesson when the LLM is unavailable.
func fallbackLesson(failures []Failure) Lesson {
	// Use the highest-risk failure to generate the lesson
	highestRisk := RiskLow
	var topFailure Failure
	for _, f := range failures {
		if riskOrder[f.Risk] > riskOrder[highestRisk] {
			highestRisk = f.Risk
			topFailure = f
		}
	}

	templates := map[string]string{
		"派生・修正Issueが発生した":                  "Issueの仕様を明確にしてからEngineerに渡し、派生Issueが発生しないようにすること",
		"[Clarification Needed] コメントが存在した": "仕様が曖昧なままEngineerに渡さず、先に仕様を人間に確認してから指示すること",
		"Superintendentが直接実装した":            "EngineerやOrchestratorが応答しない場合の対応手順を見直し、直接実装に頼らないようにすること",
		"PRが2本以上作成された":                     "重複PRが発生しないようブランチとPR管理を徹底し、作業開始前に既存PRを確認すること",
	}

	if text, ok := templates[topFailure.Description]; ok {
		return Lesson{Risk: highestRisk, Text: text}
	}
	return Lesson{Risk: highestRisk, Text: "Issueの指示品質を向上させ、エンジニアが迷わず実装できるよう仕様を明確にすること"}
}

// manageLessonsCount ensures there are at most maxLessons lessons.
// Uses LLM to merge similar lessons first, then falls back to trimming.
func (m *Manager) manageLessonsCount() error {
	lessons, err := LoadLessons(m.LessonsPath())
	if err != nil {
		return err
	}
	if len(lessons) <= maxLessons {
		return nil
	}

	log.Printf("[lessons] %d lessons exceed limit of %d, consolidating...", len(lessons), maxLessons)

	// Try LLM-based merging first
	apiKey := m.AnthropicAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	if apiKey != "" {
		merged, err := mergeLessonsWithLLM(apiKey, lessons)
		if err != nil {
			log.Printf("[lessons] LLM merging failed: %v, falling back to trim", err)
		} else {
			lessons = merged
			log.Printf("[lessons] LLM merging resulted in %d lessons", len(lessons))
		}
	}

	// If still over limit, trim by risk level
	if len(lessons) > maxLessons {
		lessons = trimLessons(lessons)
		log.Printf("[lessons] trimmed to %d lessons", len(lessons))
	}

	return SaveLessons(m.LessonsPath(), lessons)
}

// mergeLessonsWithLLM asks the Anthropic API to merge semantically similar lessons.
func mergeLessonsWithLLM(apiKey string, lessons []Lesson) ([]Lesson, error) {
	var sb strings.Builder
	for i, l := range lessons {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, FormatLesson(l))
	}

	prompt := fmt.Sprintf(`以下は%d件の教訓リストです。意味が近い教訓を統合して、できるだけ少ない件数にまとめてください。

元の教訓リスト：
%s
ルール：
- 意味が重複または非常に近い教訓を1行に統合する
- 統合後も危険度は元のうち最も高いものを使用する
- 統合により内容が失われないよう、統合後のテキストに重要な要点を含める
- 出力形式: 各行に "[危険度] 教訓テキスト" の形式で教訓を1件ずつ出力
- 危険度は「高」「中」「低」のいずれか
- テキストのみ出力（番号やコメント不要）

統合後の教訓リスト：`, len(lessons), sb.String())

	text, err := callAnthropicSimple(apiKey, prompt)
	if err != nil {
		return nil, err
	}

	var merged []Lesson
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		l, err := ParseLesson(line)
		if err != nil {
			log.Printf("[lessons] mergeLessonsWithLLM: skipping unparseable line %q: %v", line, err)
			continue
		}
		merged = append(merged, l)
	}

	if len(merged) == 0 {
		return nil, fmt.Errorf("LLM returned no parseable lessons")
	}

	// Safety check: merged should not exceed original count
	if len(merged) >= len(lessons) {
		return nil, fmt.Errorf("LLM did not reduce lesson count (%d -> %d)", len(lessons), len(merged))
	}

	return merged, nil
}

// trimLessons removes lowest-risk lessons until at most maxLessons remain.
// Within the same risk level, earlier (older) lessons are removed first.
func trimLessons(lessons []Lesson) []Lesson {
	if len(lessons) <= maxLessons {
		return lessons
	}

	// Count how many to remove
	toRemove := len(lessons) - maxLessons

	// Build a list sorted by risk ascending (lowest risk first = first to remove)
	// Preserve original order within same risk level.
	type indexedLesson struct {
		idx    int
		lesson Lesson
	}
	indexed := make([]indexedLesson, len(lessons))
	for i, l := range lessons {
		indexed[i] = indexedLesson{idx: i, lesson: l}
	}

	// Mark lessons for removal: prefer lowest risk, then oldest (smallest index)
	removed := make(map[int]bool)
	for risk := range []RiskLevel{RiskLow, RiskMedium, RiskHigh} {
		_ = risk // iterate in ascending priority
	}

	// Simple approach: iterate low→medium→high and mark for removal
	for _, checkRisk := range []RiskLevel{RiskLow, RiskMedium, RiskHigh} {
		for _, il := range indexed {
			if toRemove == 0 {
				break
			}
			if il.lesson.Risk == checkRisk {
				removed[il.idx] = true
				toRemove--
			}
		}
		if toRemove == 0 {
			break
		}
	}

	var result []Lesson
	for i, l := range lessons {
		if !removed[i] {
			result = append(result, l)
		}
	}
	return result
}

// --- Anthropic API helpers ---

type simpleAnthropicRequest struct {
	Model     string                   `json:"model"`
	MaxTokens int                      `json:"max_tokens"`
	Messages  []simpleAnthropicMessage `json:"messages"`
}

type simpleAnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type simpleAnthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// callAnthropicSimple makes a simple (non-agentic) call to the Anthropic API.
func callAnthropicSimple(apiKey, prompt string) (string, error) {
	reqBody := simpleAnthropicRequest{
		Model:     lessonsMgmtModel,
		MaxTokens: 1024,
		Messages: []simpleAnthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal anthropic request: %w", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodPost, anthropicEndpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create anthropic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

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

	var apiResp simpleAnthropicResponse
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

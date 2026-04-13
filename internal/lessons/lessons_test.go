package lessons

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLesson(t *testing.T) {
	tests := []struct {
		input    string
		wantRisk RiskLevel
		wantText string
		wantErr  bool
	}{
		{"[高] バグ修正Issueは症状への対処だけでなく再発防止策まで含めること", RiskHigh, "バグ修正Issueは症状への対処だけでなく再発防止策まで含めること", false},
		{"[中] 仕様が曖昧なままEngineerに渡さず、先に人間に確認を取ること", RiskMedium, "仕様が曖昧なままEngineerに渡さず、先に人間に確認を取ること", false},
		{"[低] PRを複数作成しないようにブランチ管理を徹底すること", RiskLow, "PRを複数作成しないようにブランチ管理を徹底すること", false},
		{"invalid line", "", "", true},
		{"", "", "", true},
		{"[高]", RiskHigh, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLesson(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseLesson(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseLesson(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.Risk != tt.wantRisk {
				t.Errorf("ParseLesson(%q).Risk = %q, want %q", tt.input, got.Risk, tt.wantRisk)
			}
			if got.Text != tt.wantText {
				t.Errorf("ParseLesson(%q).Text = %q, want %q", tt.input, got.Text, tt.wantText)
			}
		})
	}
}

func TestFormatLesson(t *testing.T) {
	l := Lesson{Risk: RiskHigh, Text: "テスト教訓"}
	got := FormatLesson(l)
	want := "[高] テスト教訓"
	if got != want {
		t.Errorf("FormatLesson() = %q, want %q", got, want)
	}
}

func TestLoadSaveLessons(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lessons.txt")

	// Load from non-existent file should return empty
	lessons, err := LoadLessons(path)
	if err != nil {
		t.Fatalf("LoadLessons non-existent: %v", err)
	}
	if len(lessons) != 0 {
		t.Errorf("LoadLessons non-existent: got %d lessons, want 0", len(lessons))
	}

	// Save and reload
	original := []Lesson{
		{Risk: RiskHigh, Text: "高い教訓"},
		{Risk: RiskMedium, Text: "中程度の教訓"},
		{Risk: RiskLow, Text: "低い教訓"},
	}

	if err := SaveLessons(path, original); err != nil {
		t.Fatalf("SaveLessons: %v", err)
	}

	loaded, err := LoadLessons(path)
	if err != nil {
		t.Fatalf("LoadLessons: %v", err)
	}
	if len(loaded) != len(original) {
		t.Fatalf("LoadLessons: got %d lessons, want %d", len(loaded), len(original))
	}
	for i, l := range loaded {
		if l.Risk != original[i].Risk || l.Text != original[i].Text {
			t.Errorf("LoadLessons[%d]: got %+v, want %+v", i, l, original[i])
		}
	}
}

func TestAppendLesson(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lessons.txt")

	if err := AppendLesson(path, Lesson{Risk: RiskHigh, Text: "first"}); err != nil {
		t.Fatalf("AppendLesson: %v", err)
	}
	if err := AppendLesson(path, Lesson{Risk: RiskMedium, Text: "second"}); err != nil {
		t.Fatalf("AppendLesson: %v", err)
	}

	lessons, err := LoadLessons(path)
	if err != nil {
		t.Fatalf("LoadLessons: %v", err)
	}
	if len(lessons) != 2 {
		t.Fatalf("got %d lessons, want 2", len(lessons))
	}
	if lessons[0].Text != "first" || lessons[1].Text != "second" {
		t.Errorf("unexpected lesson order: %v", lessons)
	}
}

func TestInjectLessons_Empty(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}

	result := m.InjectLessons()
	if result != "" {
		t.Errorf("InjectLessons with no lessons: got %q, want empty", result)
	}
}

func TestInjectLessons_WithLessons(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lessons.txt")

	lessons := []Lesson{
		{Risk: RiskHigh, Text: "重要な教訓"},
		{Risk: RiskMedium, Text: "中程度の教訓"},
	}
	if err := SaveLessons(path, lessons); err != nil {
		t.Fatalf("SaveLessons: %v", err)
	}

	m := &Manager{DataDir: dir}
	result := m.InjectLessons()

	if result == "" {
		t.Error("InjectLessons with lessons: got empty string")
	}
	if !strings.Contains(result, "[高] 重要な教訓") {
		t.Errorf("InjectLessons: missing high-risk lesson: %q", result)
	}
	if !strings.Contains(result, "[中] 中程度の教訓") {
		t.Errorf("InjectLessons: missing medium-risk lesson: %q", result)
	}
}

func TestTrimLessons(t *testing.T) {
	// Create 17 lessons: some high, some medium, some low
	// We need to remove 2 (17-15=2), and should remove lowest-risk first (oldest low-risk).
	lessons := []Lesson{
		{Risk: RiskHigh, Text: "high1"},
		{Risk: RiskHigh, Text: "high2"},
		{Risk: RiskHigh, Text: "high3"},
		{Risk: RiskHigh, Text: "high4"},
		{Risk: RiskHigh, Text: "high5"},
		{Risk: RiskMedium, Text: "med1"},
		{Risk: RiskMedium, Text: "med2"},
		{Risk: RiskMedium, Text: "med3"},
		{Risk: RiskMedium, Text: "med4"},
		{Risk: RiskMedium, Text: "med5"},
		{Risk: RiskLow, Text: "low1"}, // oldest low-risk → removed first
		{Risk: RiskLow, Text: "low2"}, // second oldest → removed second
		{Risk: RiskLow, Text: "low3"},
		{Risk: RiskLow, Text: "low4"},
		{Risk: RiskLow, Text: "low5"},
		{Risk: RiskLow, Text: "low6"},
		{Risk: RiskLow, Text: "low7"},
	}

	trimmed := trimLessons(lessons)
	if len(trimmed) != maxLessons {
		t.Errorf("trimLessons: got %d lessons, want %d", len(trimmed), maxLessons)
	}

	// The oldest 2 low-risk lessons (low1, low2) should have been removed.
	for _, l := range trimmed {
		if l.Risk == RiskLow && (l.Text == "low1" || l.Text == "low2") {
			t.Errorf("trimLessons: oldest low-risk lesson should have been removed: %+v", l)
		}
	}

	// All high and medium lessons should remain.
	highCount, medCount := 0, 0
	for _, l := range trimmed {
		switch l.Risk {
		case RiskHigh:
			highCount++
		case RiskMedium:
			medCount++
		}
	}
	if highCount != 5 {
		t.Errorf("trimLessons: got %d high-risk lessons, want 5", highCount)
	}
	if medCount != 5 {
		t.Errorf("trimLessons: got %d medium-risk lessons, want 5", medCount)
	}
}

func TestTrimLessons_ExcessLowRisk(t *testing.T) {
	// 20 low-risk lessons: trim to 15 (remove oldest 5)
	lessons := make([]Lesson, 20)
	for i := range lessons {
		lessons[i] = Lesson{Risk: RiskLow, Text: fmt.Sprintf("low%d", i+1)}
	}

	trimmed := trimLessons(lessons)
	if len(trimmed) != maxLessons {
		t.Errorf("trimLessons: got %d lessons, want %d", len(trimmed), maxLessons)
	}
	// low1..low5 should be removed, low6..low20 remain
	for _, l := range trimmed {
		for _, removed := range []string{"low1", "low2", "low3", "low4", "low5"} {
			if l.Text == removed {
				t.Errorf("trimLessons: expected %q to be removed", removed)
			}
		}
	}
}

func TestScoreIssue_LocalIssue(t *testing.T) {
	// Local issues should not be scored
	result, err := ScoreIssue("local-001", "", "", 0)
	if err == nil {
		t.Errorf("ScoreIssue local issue: expected error, got result=%+v", result)
	}
}

func TestFallbackLesson_DerivedIssue(t *testing.T) {
	failures := []Failure{
		{Description: "派生・修正Issueが発生", Risk: RiskHigh, Points: 30},
	}
	lesson := fallbackLesson(failures)
	if lesson.Risk != RiskHigh {
		t.Errorf("fallbackLesson: risk = %q, want 高", lesson.Risk)
	}
	if lesson.Text == "" {
		t.Error("fallbackLesson: empty text")
	}
}

func TestFallbackLesson_MultipleFailures(t *testing.T) {
	failures := []Failure{
		{Description: "Superintendentが直接実装した", Risk: RiskMedium, Points: 20},
		{Description: "PRが2本以上作成された", Risk: RiskLow, Points: 15},
	}
	lesson := fallbackLesson(failures)
	// The highest risk failure should determine the lesson risk
	if lesson.Risk != RiskMedium {
		t.Errorf("fallbackLesson: risk = %q, want 中", lesson.Risk)
	}
}

func TestManagerProcessMergedIssue_LocalIssue(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}

	// Local issues should be skipped gracefully
	err := m.ProcessMergedIssue("local-001", "", "", 0)
	if err != nil {
		t.Errorf("ProcessMergedIssue local-001: expected nil error, got %v", err)
	}

	// No lessons file should be created
	if _, err := os.Stat(filepath.Join(dir, "lessons.txt")); !os.IsNotExist(err) {
		t.Error("lessons.txt should not be created for local issues")
	}
}

func TestManageLessonsCount_Under15(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lessons.txt")
	m := &Manager{DataDir: dir}

	lessons := make([]Lesson, 10)
	for i := range lessons {
		lessons[i] = Lesson{Risk: RiskMedium, Text: "教訓"}
	}
	if err := SaveLessons(path, lessons); err != nil {
		t.Fatal(err)
	}

	// Should not error even without API key
	if err := m.manageLessonsCount(); err != nil {
		t.Errorf("manageLessonsCount under 15: unexpected error: %v", err)
	}

	loaded, _ := LoadLessons(path)
	if len(loaded) != 10 {
		t.Errorf("manageLessonsCount under 15: got %d lessons, want 10", len(loaded))
	}
}

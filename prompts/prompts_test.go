package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadDefault_Superintendent(t *testing.T) {
	data, err := ReadDefault("superintendent.md")
	if err != nil {
		t.Fatalf("ReadDefault(superintendent.md): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty content for superintendent.md")
	}
}

func TestReadDefault_Engineer(t *testing.T) {
	data, err := ReadDefault("engineer.md")
	if err != nil {
		t.Fatalf("ReadDefault(engineer.md): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty content for engineer.md")
	}
}

func TestReadDefault_Unknown(t *testing.T) {
	_, err := ReadDefault("unknown.md")
	if err == nil {
		t.Fatal("expected error for unknown prompt file")
	}
}

func TestWriteDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDefaults(dir); err != nil {
		t.Fatalf("WriteDefaults: %v", err)
	}

	for _, name := range []string{"superintendent.md", "engineer.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("expected non-empty %s", name)
		}
	}
}

// TestEngineerPrompt_ContainsAmbiguityHandling verifies that engineer.md includes
// the required section for handling ambiguous issue instructions.
func TestEngineerPrompt_ContainsAmbiguityHandling(t *testing.T) {
	data, err := ReadDefault("engineer.md")
	if err != nil {
		t.Fatalf("ReadDefault(engineer.md): %v", err)
	}
	content := string(data)

	mustContain := []string{
		// Section heading
		"曖昧",
		// Clarification flow keyword
		"確認",
		// Proceed without clarification keyword
		"自明",
	}

	for _, phrase := range mustContain {
		if !strings.Contains(content, phrase) {
			t.Errorf("engineer.md should contain %q for ambiguous issue handling guidance", phrase)
		}
	}
}

// TestEngineerPrompt_AmbiguityHandlingPosition verifies that the ambiguity
// handling guidance appears in the Issue Review section (Step 1).
func TestEngineerPrompt_AmbiguityHandlingPosition(t *testing.T) {
	data, err := ReadDefault("engineer.md")
	if err != nil {
		t.Fatalf("ReadDefault(engineer.md): %v", err)
	}
	content := string(data)

	issueReviewIdx := strings.Index(content, "### 1. Issue Review")
	if issueReviewIdx == -1 {
		t.Fatal("engineer.md must contain '### 1. Issue Review' section")
	}

	// The ambiguity section should appear after "### 1. Issue Review"
	ambiguityIdx := strings.Index(content[issueReviewIdx:], "曖昧")
	if ambiguityIdx == -1 {
		t.Error("ambiguity handling guidance should appear within or after the Issue Review section")
	}
}

func TestWriteDefaults_PreservesExisting(t *testing.T) {
	dir := t.TempDir()

	// Write a custom superintendent.md first
	custom := []byte("custom content")
	if err := os.WriteFile(filepath.Join(dir, "superintendent.md"), custom, 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteDefaults(dir); err != nil {
		t.Fatalf("WriteDefaults: %v", err)
	}

	// Custom file should be preserved
	got, err := os.ReadFile(filepath.Join(dir, "superintendent.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Errorf("expected custom content to be preserved, got %q", got)
	}

	// engineer.md (which was not pre-existing) should have been created
	if _, err := os.Stat(filepath.Join(dir, "engineer.md")); err != nil {
		t.Errorf("expected engineer.md to be created: %v", err)
	}
}

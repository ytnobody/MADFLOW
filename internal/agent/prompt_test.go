package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPrompt(t *testing.T) {
	// Create a temp prompts directory with a test template
	dir := t.TempDir()
	template := `# テスト
エージェント: {{AGENT_ID}}
チャットログ: {{CHATLOG_PATH}}
イシュー: {{ISSUES_DIR}}
ブランチ: {{DEVELOP_BRANCH}}
`
	if err := os.WriteFile(filepath.Join(dir, "superintendent.md"), []byte(template), 0644); err != nil {
		t.Fatal(err)
	}

	vars := PromptVars{
		AgentID:       "superintendent",
		ChatLogPath:   "/home/user/.madflow/my-app/chatlog.txt",
		IssuesDir:     "/home/user/.madflow/my-app/issues",
		DevelopBranch: "develop",
	}

	prompt, err := LoadPrompt(dir, RoleSuperintendent, vars)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(prompt, "{{AGENT_ID}}") {
		t.Error("AGENT_ID placeholder was not substituted")
	}
	if !strings.Contains(prompt, "superintendent") {
		t.Error("expected agent ID in prompt")
	}
	if !strings.Contains(prompt, "/home/user/.madflow/my-app/chatlog.txt") {
		t.Error("expected chatlog path in prompt")
	}
	if !strings.Contains(prompt, "develop") {
		t.Error("expected develop branch in prompt")
	}
}

func TestLoadPromptUnknownRole(t *testing.T) {
	_, err := LoadPrompt(t.TempDir(), Role("unknown"), PromptVars{})
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestSubstituteVars(t *testing.T) {
	content := "agent={{AGENT_ID}} team={{TEAM_NUM}} empty={{MAIN_BRANCH}}"
	vars := PromptVars{
		AgentID: "engineer-1",
		TeamNum: "1",
		// MainBranch is empty, should not replace
	}

	result := substituteVars(content, vars)

	if strings.Contains(result, "{{AGENT_ID}}") {
		t.Error("AGENT_ID not replaced")
	}
	if !strings.Contains(result, "engineer-1") {
		t.Error("expected engineer-1")
	}
	if !strings.Contains(result, "team=1") {
		t.Error("expected team=1")
	}
	// Empty var should leave placeholder
	if !strings.Contains(result, "{{MAIN_BRANCH}}") {
		t.Error("empty var should leave placeholder intact")
	}
}

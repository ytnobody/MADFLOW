package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/reset"
)

func TestParseDistilledMemo(t *testing.T) {
	raw := `STATE: 認証機能のAPIを実装中
DECISIONS: JWTトークンを使用
OPEN: トークン有効期限が未決
NEXT: テストを書く`

	memo := parseDistilledMemo("engineer-1", raw)

	if memo.AgentID != "engineer-1" {
		t.Errorf("expected agentID engineer-1, got %s", memo.AgentID)
	}
	if memo.CurrentState != "認証機能のAPIを実装中" {
		t.Errorf("unexpected state: %s", memo.CurrentState)
	}
	if memo.Decisions != "JWTトークンを使用" {
		t.Errorf("unexpected decisions: %s", memo.Decisions)
	}
	if memo.OpenIssues != "トークン有効期限が未決" {
		t.Errorf("unexpected open issues: %s", memo.OpenIssues)
	}
	if memo.NextStep != "テストを書く" {
		t.Errorf("unexpected next step: %s", memo.NextStep)
	}
}

func TestParseDistilledMemoFallback(t *testing.T) {
	raw := "予期しないフォーマットの応答"
	memo := parseDistilledMemo("pm", raw)

	if memo.CurrentState != raw {
		t.Errorf("expected fallback to raw, got %s", memo.CurrentState)
	}
}

func TestBuildInitialPrompt(t *testing.T) {
	agent := &Agent{
		ID:           AgentID{Role: RoleSuperintendent},
		OriginalTask: "Issue #001 の管理",
		ChatLog:      chatlog.New("/tmp/test/chatlog.txt"),
		MemosDir:     "/tmp/memos",
	}

	prompt := agent.buildInitialPrompt("")

	if !strings.Contains(prompt, "元の依頼内容") {
		t.Error("expected original task in prompt")
	}
	if !strings.Contains(prompt, "Issue #001 の管理") {
		t.Error("expected task content in prompt")
	}
	if !strings.Contains(prompt, "/tmp/test/chatlog.txt") {
		t.Error("expected chatlog path in prompt")
	}
	if strings.Contains(prompt, "作業メモ") {
		t.Error("should not contain memo section when memo is empty")
	}
}

func TestBuildInitialPromptWithMemo(t *testing.T) {
	agent := &Agent{
		ID:           AgentID{Role: RoleEngineer, TeamNum: 1},
		OriginalTask: "コード実装",
		ChatLog:      chatlog.New("/tmp/test/chatlog.txt"),
	}

	prompt := agent.buildInitialPrompt("前回の作業状態")

	if !strings.Contains(prompt, "作業メモ") {
		t.Error("expected memo section in prompt")
	}
	if !strings.Contains(prompt, "前回の作業状態") {
		t.Error("expected memo content in prompt")
	}
}

func TestBuildInitialPromptWithReplay(t *testing.T) {
	ag := &Agent{
		ID:           AgentID{Role: RoleEngineer, TeamNum: 1},
		OriginalTask: "コード実装",
		ChatLog:      chatlog.New("/tmp/test/chatlog.txt"),
	}

	missed := []chatlog.Message{
		{Raw: "[2026-02-21T10:00:00] [@engineer-1] PM: タスクを確認してください"},
		{Raw: "[2026-02-21T10:01:00] [@engineer-1] architect-1: 設計レビュー完了"},
	}

	prompt := ag.buildInitialPromptWithReplay("前回のメモ", missed)

	if !strings.Contains(prompt, "未処理メッセージ") {
		t.Error("expected missed messages section in prompt")
	}
	if !strings.Contains(prompt, "タスクを確認してください") {
		t.Error("expected first missed message in prompt")
	}
	if !strings.Contains(prompt, "設計レビュー完了") {
		t.Error("expected second missed message in prompt")
	}
	if !strings.Contains(prompt, "前回のメモ") {
		t.Error("expected memo content in prompt")
	}
}

func TestBuildInitialPromptWithReplayEmpty(t *testing.T) {
	ag := &Agent{
		ID:           AgentID{Role: RoleEngineer, TeamNum: 1},
		OriginalTask: "コード実装",
		ChatLog:      chatlog.New("/tmp/test/chatlog.txt"),
	}

	prompt := ag.buildInitialPromptWithReplay("メモ", nil)

	if strings.Contains(prompt, "未処理メッセージ") {
		t.Error("should not contain missed messages section when there are none")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Error("should not truncate short strings")
	}
	result := truncate("this is a long string", 10)
	if result != "this is a ..." {
		t.Errorf("unexpected truncation: %s", result)
	}
}

func TestResetTimerIntegration(t *testing.T) {
	timer := reset.NewTimer(50 * time.Millisecond)
	if timer.Expired() {
		t.Error("should not be expired")
	}
	time.Sleep(60 * time.Millisecond)
	if !timer.Expired() {
		t.Error("should be expired")
	}
	timer.Reset()
	if timer.Expired() {
		t.Error("should not be expired after reset")
	}
}

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	memo := parseDistilledMemo("superintendent", raw)

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

// TestBuildInitialPromptWithOriginalTaskStartsImmediately は、OriginalTask がある場合に
// 即座に実装開始を指示するプロンプトが生成されることを確認する。
// MADFLOW-077 の修正: エンジニアが確実に作業を開始できるよう、
// チャットログのメッセージ待ちではなく即座に実装開始を指示する。
func TestBuildInitialPromptWithOriginalTaskStartsImmediately(t *testing.T) {
	ag := &Agent{
		ID:           AgentID{Role: RoleEngineer, TeamNum: 1},
		OriginalTask: "Issue #test: テスト実装\n\nテスト用のイシューです。",
		ChatLog:      chatlog.New("/tmp/test/chatlog.txt"),
	}

	prompt := ag.buildInitialPrompt("")

	// OriginalTask がある場合は即座に実装開始を指示する
	if !strings.Contains(prompt, "実装を開始してください") {
		t.Error("expected prompt to contain '実装を開始してください' when OriginalTask is set")
	}
	// スタンバイ時の待機指示は含まれないはず
	if strings.Contains(prompt, "投稿されるのを待ち") {
		t.Error("expected prompt NOT to contain wait instruction when OriginalTask is set")
	}
}

// TestBuildInitialPromptWithoutOriginalTaskWaits は、OriginalTask がない場合に
// チャットログのメッセージを待つ指示が生成されることを確認する（スタンバイモード）。
func TestBuildInitialPromptWithoutOriginalTaskWaits(t *testing.T) {
	ag := &Agent{
		ID:      AgentID{Role: RoleEngineer, TeamNum: 1},
		ChatLog: chatlog.New("/tmp/test/chatlog.txt"),
		// OriginalTask は空
	}

	prompt := ag.buildInitialPrompt("")

	// OriginalTask がない場合はチャットログのメッセージを待つ
	if !strings.Contains(prompt, "投稿されるのを待ち") {
		t.Error("expected prompt to contain wait instruction when OriginalTask is empty")
	}
	// 実装開始の指示は含まれないはず
	if strings.Contains(prompt, "実装を開始してください") {
		t.Error("expected prompt NOT to contain immediate start instruction when OriginalTask is empty")
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

// mockProcess is a test double for Process.
type mockProcess struct {
	response string
	err      error
}

func (m *mockProcess) Send(ctx context.Context, prompt string) (string, error) {
	return m.response, m.err
}

func TestReadySignaledAfterRun(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(logPath, nil, 0644)
	memosDir := filepath.Join(dir, "memos")
	os.MkdirAll(memosDir, 0755)

	ag := NewAgent(AgentConfig{
		ID:            AgentID{Role: RoleSuperintendent},
		Role:          RoleSuperintendent,
		SystemPrompt:  "test",
		Model:         "test",
		ChatLogPath:   logPath,
		MemosDir:      memosDir,
		ResetInterval: time.Hour,
		Process:       &mockProcess{response: "ok"},
	})

	// Ready should not be signaled before Run
	select {
	case <-ag.Ready():
		t.Fatal("Ready should not be closed before Run")
	default:
		// expected
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Run in background; it will send initial prompt then block on chatlog watch
	go func() {
		ag.Run(ctx)
	}()

	// Wait for Ready signal
	select {
	case <-ag.Ready():
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Ready was not signaled within timeout")
	}

	cancel()
}

func TestReadySignaledOnSendError(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(logPath, nil, 0644)
	memosDir := filepath.Join(dir, "memos")
	os.MkdirAll(memosDir, 0755)

	ag := NewAgent(AgentConfig{
		ID:            AgentID{Role: RoleSuperintendent},
		Role:          RoleSuperintendent,
		SystemPrompt:  "test",
		Model:         "test",
		ChatLogPath:   logPath,
		MemosDir:      memosDir,
		ResetInterval: time.Hour,
		Process:       &mockProcess{err: context.Canceled},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	go func() {
		ag.Run(ctx)
	}()

	select {
	case <-ag.Ready():
		// success - Ready should be signaled even on error
	case <-time.After(2 * time.Second):
		t.Fatal("Ready was not signaled on send error")
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

// maxIterProcess is a mock Process that returns MaxIterationsError for the first N calls,
// then succeeds.
type maxIterProcess struct {
	callCount   atomic.Int32
	failCount   int32 // number of calls that return MaxIterationsError
	lastPrompts []string
}

func (m *maxIterProcess) Send(_ context.Context, prompt string) (string, error) {
	m.lastPrompts = append(m.lastPrompts, prompt)
	n := m.callCount.Add(1)
	if n <= m.failCount {
		return "", &MaxIterationsError{PartialResponse: fmt.Sprintf("partial-%d", n)}
	}
	return "completed", nil
}

// TestSendContinuation verifies that send() auto-continues on MaxIterationsError.
func TestSendContinuation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(logPath, nil, 0644)
	memosDir := filepath.Join(dir, "memos")
	os.MkdirAll(memosDir, 0755)

	proc := &maxIterProcess{failCount: 2}
	ag := NewAgent(AgentConfig{
		ID:            AgentID{Role: RoleEngineer, TeamNum: 1},
		Role:          RoleEngineer,
		SystemPrompt:  "test",
		Model:         "test",
		ChatLogPath:   logPath,
		MemosDir:      memosDir,
		ResetInterval: time.Hour,
		Process:       proc,
	})

	resp, err := ag.send(context.Background(), "initial prompt")
	if err != nil {
		t.Fatalf("expected success after continuations, got: %v", err)
	}
	if resp != "completed" {
		t.Errorf("expected 'completed', got %q", resp)
	}
	// Should have been called 3 times: initial + 2 continuations
	if proc.callCount.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", proc.callCount.Load())
	}
	// Second and third calls should use continuation prompt
	for i := 1; i < len(proc.lastPrompts); i++ {
		if proc.lastPrompts[i] != continuationPrompt {
			t.Errorf("call %d: expected continuation prompt, got %q", i+1, proc.lastPrompts[i])
		}
	}
}

// TestSendContinuationExhausted verifies that send() gives up after maxContinuations.
func TestSendContinuationExhausted(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(logPath, nil, 0644)
	memosDir := filepath.Join(dir, "memos")
	os.MkdirAll(memosDir, 0755)

	// Always fail with MaxIterationsError
	proc := &maxIterProcess{failCount: 100}
	ag := NewAgent(AgentConfig{
		ID:            AgentID{Role: RoleEngineer, TeamNum: 1},
		Role:          RoleEngineer,
		SystemPrompt:  "test",
		Model:         "test",
		ChatLogPath:   logPath,
		MemosDir:      memosDir,
		ResetInterval: time.Hour,
		Process:       proc,
	})

	_, err := ag.send(context.Background(), "initial prompt")
	if err == nil {
		t.Fatal("expected error after exhausting continuations")
	}
	if !isMaxIterationsError(err) {
		t.Errorf("expected MaxIterationsError, got: %T: %v", err, err)
	}
	// Should have been called maxContinuations+1 times (initial + 3 continuations)
	expected := int32(maxContinuations + 1)
	if proc.callCount.Load() != expected {
		t.Errorf("expected %d calls, got %d", expected, proc.callCount.Load())
	}
}

// retryCountProcess counts calls and fails the first N times with a non-rate-limit error.
type retryCountProcess struct {
	callCount atomic.Int32
	failCount int32
}

func (m *retryCountProcess) Send(_ context.Context, _ string) (string, error) {
	n := m.callCount.Add(1)
	if n <= m.failCount {
		return "", fmt.Errorf("transient error %d", n)
	}
	return "ok", nil
}

// TestSendWithRetry verifies initial retry logic in sendWithRetry.
func TestSendWithRetry(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(logPath, nil, 0644)
	memosDir := filepath.Join(dir, "memos")
	os.MkdirAll(memosDir, 0755)

	proc := &retryCountProcess{failCount: 2}
	ag := NewAgent(AgentConfig{
		ID:            AgentID{Role: RoleEngineer, TeamNum: 1},
		Role:          RoleEngineer,
		SystemPrompt:  "test",
		Model:         "test",
		ChatLogPath:   logPath,
		MemosDir:      memosDir,
		ResetInterval: time.Hour,
		Process:       proc,
	})

	resp, err := ag.sendWithRetry(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if resp != "ok" {
		t.Errorf("expected 'ok', got %q", resp)
	}
}

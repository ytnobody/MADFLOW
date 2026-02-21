package reset

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadMemo(t *testing.T) {
	dir := t.TempDir()

	memo := WorkMemo{
		AgentID:      "engineer-1",
		Timestamp:    time.Date(2026, 2, 21, 10, 8, 0, 0, time.UTC),
		CurrentState: "認証機能のAPIエンドポイントを実装中",
		Decisions:    "JWTトークンを使用する方針で決定",
		OpenIssues:   "トークンの有効期限の設定値が未決",
		NextStep:     "ログインエンドポイントのテストを書く",
	}

	path, err := SaveMemo(dir, memo)
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(path) != "engineer-1-20260221T100800.md" {
		t.Errorf("unexpected filename: %s", filepath.Base(path))
	}

	// Verify file exists and has content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if len(content) == 0 {
		t.Fatal("memo file is empty")
	}
}

func TestLoadLatestMemo(t *testing.T) {
	dir := t.TempDir()

	// Save two memos
	SaveMemo(dir, WorkMemo{
		AgentID:      "engineer-1",
		Timestamp:    time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC),
		CurrentState: "古いメモ",
	})
	SaveMemo(dir, WorkMemo{
		AgentID:      "engineer-1",
		Timestamp:    time.Date(2026, 2, 21, 10, 8, 0, 0, time.UTC),
		CurrentState: "新しいメモ",
	})
	// Different agent
	SaveMemo(dir, WorkMemo{
		AgentID:      "architect-1",
		Timestamp:    time.Date(2026, 2, 21, 10, 8, 0, 0, time.UTC),
		CurrentState: "別のエージェント",
	})

	content, err := LoadLatestMemo(dir, "engineer-1")
	if err != nil {
		t.Fatal(err)
	}
	if content == "" {
		t.Fatal("expected memo content")
	}
	// Should be the newer one
	if !contains(content, "新しいメモ") {
		t.Errorf("expected latest memo, got: %s", content)
	}
}

func TestLoadLatestMemoNotFound(t *testing.T) {
	dir := t.TempDir()
	content, err := LoadLatestMemo(dir, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Errorf("expected empty string, got %s", content)
	}
}

func TestLoadLatestMemoDirNotExist(t *testing.T) {
	content, err := LoadLatestMemo("/tmp/nonexistent-memos-dir", "test")
	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Errorf("expected empty string, got %s", content)
	}
}

func TestLoadLatestMemoWithTime(t *testing.T) {
	dir := t.TempDir()

	ts := time.Date(2026, 2, 21, 10, 8, 0, 0, time.UTC)
	SaveMemo(dir, WorkMemo{
		AgentID:      "engineer-1",
		Timestamp:    ts,
		CurrentState: "テスト状態",
	})

	content, memoTs, err := LoadLatestMemoWithTime(dir, "engineer-1")
	if err != nil {
		t.Fatal(err)
	}
	if content == "" {
		t.Fatal("expected memo content")
	}
	if !contains(content, "テスト状態") {
		t.Errorf("expected content with テスト状態, got: %s", content)
	}
	if !memoTs.Equal(ts) {
		t.Errorf("expected timestamp %v, got %v", ts, memoTs)
	}
}

func TestLoadLatestMemoWithTimeNotFound(t *testing.T) {
	dir := t.TempDir()
	content, ts, err := LoadLatestMemoWithTime(dir, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Errorf("expected empty content, got %s", content)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time, got %v", ts)
	}
}

func TestLoadLatestMemoWithTimePicksLatest(t *testing.T) {
	dir := t.TempDir()

	SaveMemo(dir, WorkMemo{
		AgentID:      "engineer-1",
		Timestamp:    time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC),
		CurrentState: "古いメモ",
	})
	latestTs := time.Date(2026, 2, 21, 10, 8, 0, 0, time.UTC)
	SaveMemo(dir, WorkMemo{
		AgentID:      "engineer-1",
		Timestamp:    latestTs,
		CurrentState: "新しいメモ",
	})

	content, ts, err := LoadLatestMemoWithTime(dir, "engineer-1")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(content, "新しいメモ") {
		t.Errorf("expected latest memo, got: %s", content)
	}
	if !ts.Equal(latestTs) {
		t.Errorf("expected timestamp %v, got %v", latestTs, ts)
	}
}

func TestTimer(t *testing.T) {
	timer := NewTimer(100 * time.Millisecond)

	if timer.Expired() {
		t.Error("timer should not be expired immediately")
	}

	remaining := timer.Remaining()
	if remaining <= 0 || remaining > 100*time.Millisecond {
		t.Errorf("unexpected remaining: %v", remaining)
	}

	time.Sleep(150 * time.Millisecond)

	if !timer.Expired() {
		t.Error("timer should be expired after interval")
	}

	if timer.Remaining() != 0 {
		t.Error("remaining should be 0 after expiry")
	}

	timer.Reset()
	if timer.Expired() {
		t.Error("timer should not be expired after reset")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

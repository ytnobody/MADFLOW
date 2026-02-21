package reset

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WorkMemo holds the distilled state of an agent at reset time.
type WorkMemo struct {
	AgentID      string
	Timestamp    time.Time
	CurrentState string
	Decisions    string
	OpenIssues   string
	NextStep     string
}

// SaveMemo writes a work memo to the memos directory.
func SaveMemo(memosDir string, memo WorkMemo) (string, error) {
	if err := os.MkdirAll(memosDir, 0755); err != nil {
		return "", fmt.Errorf("create memos dir: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.md",
		memo.AgentID,
		memo.Timestamp.Format("20060102T150405"),
	)
	path := filepath.Join(memosDir, filename)

	content := fmt.Sprintf(`# 作業メモ: %s
日時: %s

## 現在の状態
%s

## 決定事項
%s

## 未解決の課題
%s

## 次の一手
%s
`,
		memo.AgentID,
		memo.Timestamp.Format("2006-01-02 15:04:05"),
		memo.CurrentState,
		memo.Decisions,
		memo.OpenIssues,
		memo.NextStep,
	)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write memo: %w", err)
	}
	return path, nil
}

// LoadLatestMemo reads the most recent memo for the given agent.
func LoadLatestMemo(memosDir, agentID string) (string, error) {
	entries, err := os.ReadDir(memosDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read memos dir: %w", err)
	}

	prefix := agentID + "-"
	var latest string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			// Entries are sorted alphabetically; timestamp format sorts correctly
			latest = name
		}
	}

	if latest == "" {
		return "", nil
	}

	data, err := os.ReadFile(filepath.Join(memosDir, latest))
	if err != nil {
		return "", fmt.Errorf("read memo: %w", err)
	}
	return string(data), nil
}

// Timer manages the context reset countdown.
type Timer struct {
	interval time.Duration
	started  time.Time
}

func NewTimer(interval time.Duration) *Timer {
	return &Timer{
		interval: interval,
		started:  time.Now(),
	}
}

// Expired returns true if the reset interval has elapsed.
func (t *Timer) Expired() bool {
	return time.Since(t.started) >= t.interval
}

// Remaining returns the time until the next reset.
func (t *Timer) Remaining() time.Duration {
	remaining := t.interval - time.Since(t.started)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset restarts the timer.
func (t *Timer) Reset() {
	t.started = time.Now()
}

// DistillPrompt returns the prompt to send to Claude to extract a work memo.
const DistillPrompt = `あなたの現在の作業状態を以下の4項目で簡潔にまとめてください。各項目は1-3文で記述してください。

1. 現在の状態: 今何をしているか
2. 決定事項: これまでに決まったこと
3. 未解決の課題: まだ解決していない問題
4. 次の一手: 次に何をすべきか

以下のフォーマットで出力してください:
STATE: <現在の状態>
DECISIONS: <決定事項>
OPEN: <未解決の課題>
NEXT: <次の一手>`

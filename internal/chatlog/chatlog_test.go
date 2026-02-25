package chatlog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseMessage(t *testing.T) {
	line := "[2026-02-21T10:00:00] [@superintendent] 監督: Issue #001 がオープンされました。"
	msg, err := ParseMessage(line)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Recipient != "superintendent" {
		t.Errorf("expected recipient superintendent, got %s", msg.Recipient)
	}
	if msg.Sender != "監督" {
		t.Errorf("expected sender 監督, got %s", msg.Sender)
	}
	if msg.Body != "Issue #001 がオープンされました。" {
		t.Errorf("unexpected body: %s", msg.Body)
	}
	if msg.Timestamp.Year() != 2026 {
		t.Errorf("unexpected year: %d", msg.Timestamp.Year())
	}
}

func TestParseMessageInvalid(t *testing.T) {
	_, err := ParseMessage("invalid line")
	if err == nil {
		t.Fatal("expected error for invalid line")
	}
}

func TestFormatMessage(t *testing.T) {
	line := FormatMessage("superintendent", "監督", "テストメッセージ")
	msg, err := ParseMessage(line)
	if err != nil {
		t.Fatalf("formatted message should be parseable: %v", err)
	}
	if msg.Recipient != "superintendent" {
		t.Errorf("expected recipient superintendent, got %s", msg.Recipient)
	}
	if msg.Sender != "監督" {
		t.Errorf("expected sender 監督, got %s", msg.Sender)
	}
}

func TestPoll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	content := `[2026-02-21T10:00:00] [@superintendent] 監督: メッセージ1
[2026-02-21T10:00:01] [@engineer-1] superintendent: メッセージ2
[2026-02-21T10:00:02] [@superintendent] engineer-1: メッセージ3
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	msgs, err := cl.Poll("superintendent")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for superintendent, got %d", len(msgs))
	}
}

func TestPollFileNotExist(t *testing.T) {
	cl := New("/tmp/nonexistent-chatlog.txt")
	msgs, err := cl.Poll("superintendent")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	cl := New(path)

	// Append to a new file
	if err := cl.Append("superintendent", "orchestrator", "Hello superintendent"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Append("engineer-1", "orchestrator", "Hello engineer"); err != nil {
		t.Fatal(err)
	}

	// Verify messages are readable
	msgs, err := cl.Poll("superintendent")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for superintendent, got %d", len(msgs))
	}
	if msgs[0].Body != "Hello superintendent" {
		t.Errorf("expected body 'Hello superintendent', got %s", msgs[0].Body)
	}
	if msgs[0].Sender != "orchestrator" {
		t.Errorf("expected sender 'orchestrator', got %s", msgs[0].Sender)
	}

	// All messages
	all, err := cl.Poll("engineer-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 message for engineer-1, got %d", len(all))
	}
}

func TestAppendToExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	// Pre-populate
	if err := os.WriteFile(path, []byte("[2026-02-21T10:00:00] [@superintendent] 監督: existing\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	if err := cl.Append("superintendent", "orchestrator", "new message"); err != nil {
		t.Fatal(err)
	}

	msgs, err := cl.Poll("superintendent")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for superintendent, got %d", len(msgs))
	}
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	// Create empty file
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := cl.Watch(ctx, "superintendent")

	// Append a message after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		defer f.Close()
		f.WriteString("[2026-02-21T10:00:00] [@superintendent] 監督: watch test\n")
	}()

	select {
	case msg := <-ch:
		if msg.Body != "watch test" {
			t.Errorf("unexpected body: %s", msg.Body)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for message")
	}
}

func TestTruncate_NoTruncationNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	lines := []string{
		"[2026-02-21T10:00:00] [@superintendent] 監督: メッセージ1",
		"[2026-02-21T10:00:01] [@superintendent] 監督: メッセージ2",
		"[2026-02-21T10:00:02] [@superintendent] 監督: メッセージ3",
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	if err := cl.Truncate(10); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	// File content should be unchanged
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("file content should not change when maxLines > actual lines\ngot: %q\nwant: %q", string(data), content)
	}
}

func TestTruncate_TruncatesOldLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "[2026-02-21T10:00:00] [@superintendent] 監督: メッセージ"+strings.Repeat("0", 1)+string(rune('0'+i)))
	}
	// Simpler: just use numbered lines
	lines = []string{
		"[2026-02-21T10:00:01] [@superintendent] 監督: メッセージ1",
		"[2026-02-21T10:00:02] [@superintendent] 監督: メッセージ2",
		"[2026-02-21T10:00:03] [@superintendent] 監督: メッセージ3",
		"[2026-02-21T10:00:04] [@superintendent] 監督: メッセージ4",
		"[2026-02-21T10:00:05] [@superintendent] 監督: メッセージ5",
		"[2026-02-21T10:00:06] [@superintendent] 監督: メッセージ6",
		"[2026-02-21T10:00:07] [@superintendent] 監督: メッセージ7",
		"[2026-02-21T10:00:08] [@superintendent] 監督: メッセージ8",
		"[2026-02-21T10:00:09] [@superintendent] 監督: メッセージ9",
		"[2026-02-21T10:00:10] [@superintendent] 監督: メッセージ10",
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	if err := cl.Truncate(3); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	remaining := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(remaining) != 3 {
		t.Errorf("expected 3 lines remaining, got %d: %v", len(remaining), remaining)
	}
	// The last 3 lines (8, 9, 10) should be kept
	if remaining[0] != lines[7] {
		t.Errorf("expected line 8, got: %s", remaining[0])
	}
	if remaining[2] != lines[9] {
		t.Errorf("expected line 10, got: %s", remaining[2])
	}
}

func TestTruncate_FileNotExist(t *testing.T) {
	cl := New("/tmp/nonexistent-chatlog-truncate.txt")
	// Should return nil (not error) when file doesn't exist
	if err := cl.Truncate(100); err != nil {
		t.Errorf("expected nil error for nonexistent file, got: %v", err)
	}
}

func TestTruncate_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	if err := cl.Truncate(100); err != nil {
		t.Errorf("expected nil error for empty file, got: %v", err)
	}
}

// TestReadFrom_OffsetResetAfterTruncate verifies that readFrom skips to the
// end of file when truncation is detected, instead of replaying old messages.
// This prevents old TEAM_CREATE/TEAM_DISBAND commands from being re-processed
// every time the chatlog cleanup goroutine truncates the file.
func TestReadFrom_OffsetResetAfterTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	// Write initial content
	initialLines := []string{
		"[2026-02-21T10:00:01] [@superintendent] 監督: 古いメッセージ1",
		"[2026-02-21T10:00:02] [@superintendent] 監督: 古いメッセージ2",
		"[2026-02-21T10:00:03] [@superintendent] 監督: 古いメッセージ3",
		"[2026-02-21T10:00:04] [@superintendent] 監督: 古いメッセージ4",
		"[2026-02-21T10:00:05] [@superintendent] 監督: 古いメッセージ5",
	}
	content := strings.Join(initialLines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)

	// Set offset to end of file (simulating Watch having read all messages)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	offset := info.Size()

	// Truncate to keep only 2 lines (file shrinks)
	if err := cl.Truncate(2); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	// readFrom should detect truncation and skip to end-of-file instead of
	// re-reading from the beginning.  No messages should be returned.
	messages, newOffset, err := cl.readFrom(offset, "superintendent")
	if err != nil {
		t.Fatalf("readFrom failed: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("expected 0 messages after truncation (skip-to-end), got %d: %v", len(messages), messages)
	}
	// Offset must have been updated to the current (smaller) file size.
	truncatedInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if newOffset != truncatedInfo.Size() {
		t.Errorf("expected newOffset=%d (end of truncated file), got %d", truncatedInfo.Size(), newOffset)
	}

	// A new message appended AFTER the truncation should be picked up on the
	// next readFrom call.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	newMsg := "[2026-02-21T10:00:06] [@superintendent] 監督: 新しいメッセージ\n"
	f.WriteString(newMsg)
	f.Close()

	messages2, _, err := cl.readFrom(newOffset, "superintendent")
	if err != nil {
		t.Fatalf("second readFrom failed: %v", err)
	}
	if len(messages2) != 1 {
		t.Errorf("expected 1 new message after truncation, got %d", len(messages2))
	}
	if len(messages2) > 0 && messages2[0].Body != "新しいメッセージ" {
		t.Errorf("expected new message body '新しいメッセージ', got: %s", messages2[0].Body)
	}
}

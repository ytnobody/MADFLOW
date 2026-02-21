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
	line := "[2026-02-21T10:00:00] [@PM] 監督: Issue #001 がオープンされました。"
	msg, err := ParseMessage(line)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Recipient != "PM" {
		t.Errorf("expected recipient PM, got %s", msg.Recipient)
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
	line := FormatMessage("PM", "監督", "テストメッセージ")
	msg, err := ParseMessage(line)
	if err != nil {
		t.Fatalf("formatted message should be parseable: %v", err)
	}
	if msg.Recipient != "PM" {
		t.Errorf("expected recipient PM, got %s", msg.Recipient)
	}
	if msg.Sender != "監督" {
		t.Errorf("expected sender 監督, got %s", msg.Sender)
	}
}

func TestPoll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	content := `[2026-02-21T10:00:00] [@PM] 監督: メッセージ1
[2026-02-21T10:00:01] [@architect-1] PM: メッセージ2
[2026-02-21T10:00:02] [@PM] architect-1: メッセージ3
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	msgs, err := cl.Poll("PM")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for PM, got %d", len(msgs))
	}
}

func TestPollFileNotExist(t *testing.T) {
	cl := New("/tmp/nonexistent-chatlog.txt")
	msgs, err := cl.Poll("PM")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
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

	ch := cl.Watch(ctx, "PM")

	// Append a message after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		defer f.Close()
		f.WriteString("[2026-02-21T10:00:00] [@PM] 監督: watch test\n")
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
		"[2026-02-21T10:00:00] [@PM] 監督: メッセージ1",
		"[2026-02-21T10:00:01] [@PM] 監督: メッセージ2",
		"[2026-02-21T10:00:02] [@PM] 監督: メッセージ3",
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
		lines = append(lines, "[2026-02-21T10:00:00] [@PM] 監督: メッセージ"+strings.Repeat("0", 1)+string(rune('0'+i)))
	}
	// Simpler: just use numbered lines
	lines = []string{
		"[2026-02-21T10:00:01] [@PM] 監督: メッセージ1",
		"[2026-02-21T10:00:02] [@PM] 監督: メッセージ2",
		"[2026-02-21T10:00:03] [@PM] 監督: メッセージ3",
		"[2026-02-21T10:00:04] [@PM] 監督: メッセージ4",
		"[2026-02-21T10:00:05] [@PM] 監督: メッセージ5",
		"[2026-02-21T10:00:06] [@PM] 監督: メッセージ6",
		"[2026-02-21T10:00:07] [@PM] 監督: メッセージ7",
		"[2026-02-21T10:00:08] [@PM] 監督: メッセージ8",
		"[2026-02-21T10:00:09] [@PM] 監督: メッセージ9",
		"[2026-02-21T10:00:10] [@PM] 監督: メッセージ10",
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

func TestReadFrom_OffsetResetAfterTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	// Write initial content
	initialLines := []string{
		"[2026-02-21T10:00:01] [@PM] 監督: 古いメッセージ1",
		"[2026-02-21T10:00:02] [@PM] 監督: 古いメッセージ2",
		"[2026-02-21T10:00:03] [@PM] 監督: 古いメッセージ3",
		"[2026-02-21T10:00:04] [@PM] 監督: 古いメッセージ4",
		"[2026-02-21T10:00:05] [@PM] 監督: 古いメッセージ5",
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

	// Now append a new message
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	newMsg := "[2026-02-21T10:00:06] [@PM] 監督: 新しいメッセージ\n"
	f.WriteString(newMsg)
	f.Close()

	// readFrom should detect truncation (file smaller than offset) and reset offset to 0
	// It should then read from the beginning and return all 3 lines (2 old + 1 new)
	messages, newOffset, err := cl.readFrom(offset, "PM")
	if err != nil {
		t.Fatalf("readFrom failed: %v", err)
	}
	if newOffset == offset {
		t.Errorf("expected offset to change after truncation reset, offset=%d newOffset=%d", offset, newOffset)
	}
	// Should have 3 messages: 2 kept by truncate + 1 new
	if len(messages) != 3 {
		t.Errorf("expected 3 messages after offset reset, got %d: %v", len(messages), messages)
	}
	// Last message should be the new one
	if len(messages) > 0 && messages[len(messages)-1].Body != "新しいメッセージ" {
		t.Errorf("expected last message to be the new one, got: %s", messages[len(messages)-1].Body)
	}
}

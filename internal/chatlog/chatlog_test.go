package chatlog

import (
	"context"
	"os"
	"path/filepath"
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

func TestAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	cl := New(path)

	// Append to a new file
	if err := cl.Append("PM", "orchestrator", "Hello PM"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Append("superintendent", "orchestrator", "Hello super"); err != nil {
		t.Fatal(err)
	}

	// Verify messages are readable
	msgs, err := cl.Poll("PM")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for PM, got %d", len(msgs))
	}
	if msgs[0].Body != "Hello PM" {
		t.Errorf("expected body 'Hello PM', got %s", msgs[0].Body)
	}
	if msgs[0].Sender != "orchestrator" {
		t.Errorf("expected sender 'orchestrator', got %s", msgs[0].Sender)
	}

	// All messages
	all, err := cl.Poll("superintendent")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 message for superintendent, got %d", len(all))
	}
}

func TestAppendToExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	// Pre-populate
	if err := os.WriteFile(path, []byte("[2026-02-21T10:00:00] [@PM] 監督: existing\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	if err := cl.Append("PM", "orchestrator", "new message"); err != nil {
		t.Fatal(err)
	}

	msgs, err := cl.Poll("PM")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for PM, got %d", len(msgs))
	}
}

func TestPollSince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	content := `[2026-02-21T10:00:00] [@PM] 監督: メッセージ1
[2026-02-21T10:05:00] [@PM] architect-1: メッセージ2
[2026-02-21T10:10:00] [@PM] engineer-1: メッセージ3
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	since := time.Date(2026, 2, 21, 10, 5, 0, 0, time.UTC)
	msgs, err := cl.PollSince("PM", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages since 10:05, got %d", len(msgs))
	}
	if msgs[0].Body != "メッセージ2" {
		t.Errorf("expected メッセージ2, got %s", msgs[0].Body)
	}
	if msgs[1].Body != "メッセージ3" {
		t.Errorf("expected メッセージ3, got %s", msgs[1].Body)
	}
}

func TestPollSinceZeroTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	content := `[2026-02-21T10:00:00] [@PM] 監督: メッセージ1
[2026-02-21T10:05:00] [@PM] architect-1: メッセージ2
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	msgs, err := cl.PollSince("PM", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages with zero time, got %d", len(msgs))
	}
}

func TestPollSinceFuture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	content := `[2026-02-21T10:00:00] [@PM] 監督: メッセージ1
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cl := New(path)
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	msgs, err := cl.PollSince("PM", future)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages with future time, got %d", len(msgs))
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

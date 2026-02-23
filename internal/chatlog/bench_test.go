package chatlog

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkParseMessage(b *testing.B) {
	line := "[2026-01-01T00:00:00] [@recipient] sender: hello world benchmark message"
	for b.Loop() {
		_, _ = ParseMessage(line)
	}
}

func BenchmarkParseMessageInvalid(b *testing.B) {
	line := "this is not a valid chatlog line"
	for b.Loop() {
		_, _ = ParseMessage(line)
	}
}

func BenchmarkChatLogPoll(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "chatlog.txt")

	// Pre-fill with 100 messages
	f, err := os.Create(path)
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}
	for i := range 100 {
		fmt.Fprintf(f, "[2026-01-01T00:00:00] [@engineer-1] superintendent: message %d\n", i)
	}
	f.Close()

	cl := New(path)
	for b.Loop() {
		_, _ = cl.Poll("engineer-1")
	}
}

func BenchmarkFormatMessage(b *testing.B) {
	for b.Loop() {
		_ = FormatMessage("recipient", "sender", "hello world benchmark message")
	}
}

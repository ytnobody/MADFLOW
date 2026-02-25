package chatlog

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

var messagePattern = regexp.MustCompile(
	`^\[(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})\] \[@([^\]]+)\] ([^:]+): (.+)$`,
)

type Message struct {
	Timestamp time.Time
	Recipient string
	Sender    string
	Body      string
	Raw       string
}

type ChatLog struct {
	path string
}

func New(path string) *ChatLog {
	return &ChatLog{path: path}
}

func (c *ChatLog) Path() string {
	return c.path
}

// ParseMessage parses a single chatlog line into a Message.
func ParseMessage(line string) (Message, error) {
	matches := messagePattern.FindStringSubmatch(strings.TrimSpace(line))
	if matches == nil {
		return Message{}, fmt.Errorf("invalid message format: %s", line)
	}

	ts, err := time.Parse("2006-01-02T15:04:05", matches[1])
	if err != nil {
		return Message{}, fmt.Errorf("parse timestamp: %w", err)
	}

	return Message{
		Timestamp: ts,
		Recipient: matches[2],
		Sender:    matches[3],
		Body:      matches[4],
		Raw:       line,
	}, nil
}

// FormatMessage creates a formatted chatlog line.
func FormatMessage(recipient, sender, body string) string {
	ts := time.Now().Format("2006-01-02T15:04:05")
	return fmt.Sprintf("[%s] [@%s] %s: %s", ts, recipient, sender, body)
}

// Append writes a new formatted message to the chatlog file.
func (c *ChatLog) Append(recipient, sender, body string) error {
	f, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open chatlog for append: %w", err)
	}
	defer f.Close()

	line := FormatMessage(recipient, sender, body)
	if _, err := fmt.Fprintln(f, line); err != nil {
		return fmt.Errorf("write chatlog: %w", err)
	}
	return nil
}

// Poll reads all messages from the log file and filters by recipient.
func (c *ChatLog) Poll(recipient string) ([]Message, error) {
	f, err := os.Open(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open chatlog: %w", err)
	}
	defer f.Close()

	var messages []Message
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		msg, err := ParseMessage(line)
		if err != nil {
			continue // skip malformed lines
		}
		if msg.Recipient == recipient {
			messages = append(messages, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan chatlog: %w", err)
	}
	return messages, nil
}

// Watch monitors the chatlog file for new messages to the given recipient.
// It yields messages on the returned channel until ctx is cancelled.
func (c *ChatLog) Watch(ctx context.Context, recipient string) <-chan Message {
	ch := make(chan Message, 16)

	go func() {
		defer close(ch)

		// Start from end of file
		var offset int64
		if info, err := os.Stat(c.path); err == nil {
			offset = info.Size()
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				newMessages, newOffset, err := c.readFrom(offset, recipient)
				if err != nil {
					continue
				}
				offset = newOffset
				for _, msg := range newMessages {
					select {
					case ch <- msg:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch
}

// WatchAll monitors the chatlog for all new messages (no recipient filter).
func (c *ChatLog) WatchAll(ctx context.Context) <-chan Message {
	ch := make(chan Message, 16)

	go func() {
		defer close(ch)

		var offset int64
		if info, err := os.Stat(c.path); err == nil {
			offset = info.Size()
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				newMessages, newOffset, err := c.readFrom(offset, "")
				if err != nil {
					continue
				}
				offset = newOffset
				for _, msg := range newMessages {
					select {
					case ch <- msg:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch
}

// Truncate keeps only the latest maxLines lines of the chatlog file,
// removing older entries. It uses atomic write (temp file + rename) for safety.
// If the file does not exist or has fewer lines than maxLines, it does nothing.
func (c *ChatLog) Truncate(maxLines int) error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read chatlog: %w", err)
	}

	lines := strings.Split(string(data), "\n")

	// Remove trailing empty lines
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if len(lines) <= maxLines {
		return nil
	}

	removed := len(lines) - maxLines
	lines = lines[removed:]

	newContent := strings.Join(lines, "\n") + "\n"

	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("write temp chatlog: %w", err)
	}

	if err := os.Rename(tmpPath, c.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename chatlog: %w", err)
	}

	log.Printf("[chatlog] truncated %d lines, keeping latest %d lines", removed, maxLines)
	return nil
}

func (c *ChatLog) readFrom(offset int64, recipient string) ([]Message, int64, error) {
	f, err := os.Open(c.path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}

	if info.Size() < offset {
		// File was truncated (e.g., by Truncate()); skip to current end to
		// avoid reprocessing old messages (which could re-create orphaned teams
		// or fire duplicate commands).
		return nil, info.Size(), nil
	}
	if info.Size() == offset {
		return nil, offset, nil
	}

	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, err
	}

	var messages []Message
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		msg, err := ParseMessage(line)
		if err != nil {
			continue
		}
		if recipient == "" || msg.Recipient == recipient {
			messages = append(messages, msg)
		}
	}

	return messages, info.Size(), scanner.Err()
}

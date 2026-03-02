package integration

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/ytnobody/madflow/internal/agent"
	"github.com/ytnobody/madflow/internal/chatlog"
)

// MockProcess implements agent.Process for testing.
// It records all prompts sent to it and can be configured with
// scripted responses or a handler function.
type MockProcess struct {
	mu        sync.Mutex
	prompts   []string
	handler   func(ctx context.Context, prompt string) (string, error)
	responses []string
	callIdx   int
}

// NewMockProcess creates a mock that returns empty responses.
func NewMockProcess() *MockProcess {
	return &MockProcess{}
}

// WithResponses sets a sequence of responses to return in order.
func (m *MockProcess) WithResponses(responses ...string) *MockProcess {
	m.responses = responses
	return m
}

// WithHandler sets a function to handle prompts dynamically.
func (m *MockProcess) WithHandler(fn func(ctx context.Context, prompt string) (string, error)) *MockProcess {
	m.handler = fn
	return m
}

// Send implements agent.Process.
func (m *MockProcess) Send(ctx context.Context, prompt string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	m.mu.Lock()
	m.prompts = append(m.prompts, prompt)
	idx := m.callIdx
	m.callIdx++
	m.mu.Unlock()

	if m.handler != nil {
		return m.handler(ctx, prompt)
	}

	if idx < len(m.responses) {
		return m.responses[idx], nil
	}

	return "", nil
}

// Prompts returns all prompts sent to this mock.
func (m *MockProcess) Prompts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.prompts))
	copy(result, m.prompts)
	return result
}

// CallCount returns how many times Send was called.
func (m *MockProcess) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callIdx
}

func (m *MockProcess) Reset(ctx context.Context) error { return nil }
func (m *MockProcess) Close() error                    { return nil }

// Verify interface compliance.
var _ agent.Process = (*MockProcess)(nil)

// ChatLogWriter is a helper that writes formatted messages to a chatlog file.
type ChatLogWriter struct {
	path string
}

func NewChatLogWriter(path string) *ChatLogWriter {
	return &ChatLogWriter{path: path}
}

// Write appends a formatted message to the chatlog.
func (w *ChatLogWriter) Write(recipient, sender, body string) error {
	msg := chatlog.FormatMessage(recipient, sender, body)
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open chatlog: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, msg)
	return err
}

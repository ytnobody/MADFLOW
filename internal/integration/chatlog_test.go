package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ytnobody/madflow/internal/agent"
	"github.com/ytnobody/madflow/internal/chatlog"
)

// TestTwoAgentsChatLogCommunication tests that two agents can communicate
// via the shared chatlog. Agent A sends a message to Agent B, and Agent B
// processes it through its mock process.
func TestTwoAgentsChatLogCommunication(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chatlog.txt")
	memosDir := filepath.Join(dir, "memos")
	os.MkdirAll(memosDir, 0755)

	// Create the chatlog file
	os.WriteFile(logPath, nil, 0644)

	// Create mock processes
	mockPM := NewMockProcess()
	mockArch := NewMockProcess()

	// Agent PM
	agentPM := agent.NewAgent(agent.AgentConfig{
		ID:            agent.AgentID{Role: agent.RolePM},
		Role:          agent.RolePM,
		SystemPrompt:  "You are a PM",
		ChatLogPath:   logPath,
		MemosDir:      memosDir,
		ResetInterval: time.Hour,
		Process:       mockPM,
	})

	// Agent Architect (team 1)
	agentArch := agent.NewAgent(agent.AgentConfig{
		ID:            agent.AgentID{Role: agent.RoleArchitect, TeamNum: 1},
		Role:          agent.RoleArchitect,
		SystemPrompt:  "You are an architect",
		ChatLogPath:   logPath,
		MemosDir:      memosDir,
		ResetInterval: time.Hour,
		Process:       mockArch,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start both agents
	go agentPM.Run(ctx)
	go agentArch.Run(ctx)

	// Wait for agents to initialize (initial Send + Watch setup)
	time.Sleep(200 * time.Millisecond)

	// Write a message to PM via chatlog
	writer := NewChatLogWriter(logPath)
	writer.Write("pm", "superintendent", "新しいイシューが追加されました: Issue #local-001")

	// Wait for PM to process the message
	time.Sleep(800 * time.Millisecond)

	// PM should have received at least 2 calls: initial prompt + the message
	if mockPM.CallCount() < 2 {
		t.Errorf("expected PM to receive at least 2 calls, got %d", mockPM.CallCount())
	}

	// Write a message to architect
	writer.Write("architect-1", "pm", "Issue #local-001 の設計を開始してください")

	// Wait for architect to process
	time.Sleep(800 * time.Millisecond)

	// Architect should have received at least 2 calls: initial + the message
	if mockArch.CallCount() < 2 {
		t.Errorf("expected architect to receive at least 2 calls, got %d", mockArch.CallCount())
	}

	// Verify the architect received the right message
	prompts := mockArch.Prompts()
	found := false
	for _, p := range prompts {
		if strings.Contains(p, "Issue #local-001 の設計を開始してください") {
			found = true
			break
		}
	}
	if !found {
		t.Error("architect did not receive the expected message")
	}

	cancel()
}

// TestChatLogWatchFiltering tests that agents only receive messages addressed to them.
func TestChatLogWatchFiltering(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chatlog.txt")
	os.WriteFile(logPath, nil, 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cl := chatlog.New(logPath)

	// Watch for messages to "engineer-1" only
	ch := cl.Watch(ctx, "engineer-1")

	// Wait for watch to initialize and record initial offset
	time.Sleep(100 * time.Millisecond)

	writer := NewChatLogWriter(logPath)

	// Write messages to different recipients
	writer.Write("pm", "superintendent", "PM向けメッセージ")
	writer.Write("engineer-1", "architect-1", "エンジニア向けメッセージ")
	writer.Write("reviewer-1", "engineer-1", "レビュアー向けメッセージ")

	// Wait for watch to pick up (needs at least one polling tick of 500ms)
	time.Sleep(800 * time.Millisecond)

	// Only engineer-1 message should come through
	select {
	case msg := <-ch:
		if msg.Recipient != "engineer-1" {
			t.Errorf("expected recipient engineer-1, got %s", msg.Recipient)
		}
		if !strings.Contains(msg.Body, "エンジニア向けメッセージ") {
			t.Errorf("unexpected body: %s", msg.Body)
		}
	default:
		t.Error("expected to receive a message for engineer-1")
	}

	// Should not have more messages
	select {
	case msg := <-ch:
		t.Errorf("unexpected extra message: %v", msg)
	default:
		// Good - no extra messages
	}

	cancel()
}

// TestAgentContextReset tests the 8-minute context reset protocol
// with a very short interval for testing.
func TestAgentContextReset(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "chatlog.txt")
	memosDir := filepath.Join(dir, "memos")
	os.MkdirAll(memosDir, 0755)
	os.WriteFile(logPath, nil, 0644)

	callCount := 0
	mock := NewMockProcess().WithHandler(func(ctx context.Context, prompt string) (string, error) {
		callCount++
		// When asked to distill, return structured memo
		if strings.Contains(prompt, "STATE:") || strings.Contains(prompt, "蒸留") {
			return "STATE: テスト中\nDECISIONS: テスト決定\nOPEN: なし\nNEXT: 続行", nil
		}
		return "OK", nil
	})

	ag := agent.NewAgent(agent.AgentConfig{
		ID:            agent.AgentID{Role: agent.RoleEngineer, TeamNum: 1},
		Role:          agent.RoleEngineer,
		SystemPrompt:  "You are an engineer",
		ChatLogPath:   logPath,
		MemosDir:      memosDir,
		ResetInterval: 100 * time.Millisecond, // Very short for testing
		Process:       mock,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ag.Run(ctx)

	// Wait for initial prompt
	time.Sleep(200 * time.Millisecond)

	// Write a message to trigger processing after reset timer expires
	time.Sleep(150 * time.Millisecond) // Ensure timer expires

	writer := NewChatLogWriter(logPath)
	writer.Write("engineer-1", "architect-1", "実装を開始してください")

	// Wait for reset + message processing
	time.Sleep(1000 * time.Millisecond)

	// Check that memo file was created
	entries, err := os.ReadDir(memosDir)
	if err != nil {
		t.Fatalf("read memos dir: %v", err)
	}

	memoFound := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "engineer-1") {
			memoFound = true
			break
		}
	}

	if !memoFound {
		t.Error("expected memo file to be created after context reset")
	}

	cancel()
}

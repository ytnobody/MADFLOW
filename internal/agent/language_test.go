package agent

import (
	"strings"
	"testing"

	"github.com/ytnobody/madflow/internal/chatlog"
)

func TestGetMessagesEN(t *testing.T) {
	m := getMessages("en")
	if !strings.Contains(m.RoleIntro, "agent operating") {
		t.Errorf("expected English role intro, got %q", m.RoleIntro)
	}
	if !strings.Contains(m.Continuation, "interrupted") {
		t.Errorf("expected English continuation, got %q", m.Continuation)
	}
}

func TestGetMessagesJA(t *testing.T) {
	m := getMessages("ja")
	if !strings.Contains(m.RoleIntro, "エージェント") {
		t.Errorf("expected Japanese role intro, got %q", m.RoleIntro)
	}
	if !strings.Contains(m.Continuation, "中断") {
		t.Errorf("expected Japanese continuation, got %q", m.Continuation)
	}
}

func TestGetMessagesUnknownFallsBackToEN(t *testing.T) {
	m := getMessages("fr")
	en := getMessages("en")
	if m.RoleIntro != en.RoleIntro {
		t.Errorf("expected fallback to English, got %q", m.RoleIntro)
	}
}

func TestBuildMessagePromptEN(t *testing.T) {
	msgs := []chatlog.Message{{Raw: "test message"}}
	result := buildMessagePrompt(msgs, "en")
	if !strings.Contains(result, "new message") {
		t.Errorf("expected English message prompt, got %q", result)
	}
	if !strings.Contains(result, "test message") {
		t.Errorf("expected message content, got %q", result)
	}
}

func TestBuildMessagePromptJA(t *testing.T) {
	msgs := []chatlog.Message{{Raw: "テストメッセージ"}}
	result := buildMessagePrompt(msgs, "ja")
	if !strings.Contains(result, "チャットログ") {
		t.Errorf("expected Japanese message prompt, got %q", result)
	}
}

func TestBuildMessagePromptMultiple(t *testing.T) {
	msgs := []chatlog.Message{
		{Raw: "message 1"},
		{Raw: "message 2"},
	}
	result := buildMessagePrompt(msgs, "en")
	if !strings.Contains(result, "2 new messages") {
		t.Errorf("expected '2 new messages', got %q", result)
	}
}

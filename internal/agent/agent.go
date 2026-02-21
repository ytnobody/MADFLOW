package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/reset"
)

// Agent represents a running MADFLOW agent.
type Agent struct {
	ID             AgentID
	Process        Process
	ChatLog        *chatlog.ChatLog
	MemosDir       string
	ResetInterval  time.Duration
	SystemPrompt   string
	OriginalTask   string    // The initial task/context for this agent
	Dormancy       *Dormancy // shared dormancy state (optional, nil = no dormancy)
	ready          chan struct{}
	readyOnce      sync.Once
}

// AgentConfig holds configuration for creating an agent.
type AgentConfig struct {
	ID            AgentID
	Role          Role
	SystemPrompt  string
	Model         string
	WorkDir       string
	ChatLogPath   string
	MemosDir      string
	ResetInterval time.Duration
	OriginalTask  string
	Process       Process   // Optional: if nil, a ClaudeProcess is created
	Dormancy      *Dormancy // Optional: shared dormancy state
}

// NewAgent creates a new agent from configuration.
func NewAgent(cfg AgentConfig) *Agent {
	var proc Process
	if cfg.Process != nil {
		proc = cfg.Process
	} else {
		if strings.HasPrefix(cfg.Model, "gemini-") {
			proc = NewGeminiProcess(GeminiOptions{
				SystemPrompt: cfg.SystemPrompt,
				Model:        cfg.Model,
				WorkDir:      cfg.WorkDir,
			})
		} else {
			proc = NewClaudeProcess(ClaudeOptions{
				SystemPrompt: cfg.SystemPrompt,
				Model:        cfg.Model,
				WorkDir:      cfg.WorkDir,
			})
		}
	}

	return &Agent{
		ID:            cfg.ID,
		Process:       proc,
		ChatLog:       chatlog.New(cfg.ChatLogPath),
		MemosDir:      cfg.MemosDir,
		ResetInterval: cfg.ResetInterval,
		SystemPrompt:  cfg.SystemPrompt,
		OriginalTask:  cfg.OriginalTask,
		Dormancy:      cfg.Dormancy,
		ready:         make(chan struct{}),
	}
}

// Ready returns a channel that is closed when the agent has completed
// its initial startup (first prompt sent). Used by the orchestrator to
// wait for all agents to be ready before starting other subsystems.
func (a *Agent) Ready() <-chan struct{} {
	return a.ready
}

// markReady signals that the agent has completed its initial startup.
// Safe to call multiple times; only the first call has an effect.
func (a *Agent) markReady() {
	a.readyOnce.Do(func() { close(a.ready) })
}

// Run executes the agent's main loop:
//  1. Watch chatlog for messages addressed to this agent
//  2. Send messages to Claude for processing
//  3. Claude executes actions (writes to chatlog, edits files, etc.)
//  4. Reset context when timer expires
func (a *Agent) Run(ctx context.Context) error {
	timer := reset.NewTimer(a.ResetInterval)
	recipient := a.ID.String()

	log.Printf("[%s] agent started", recipient)

	// Load latest memo if resuming
	memo, _ := reset.LoadLatestMemo(a.MemosDir, recipient)

	// Initial prompt to start the agent
	initialPrompt := a.buildInitialPrompt(memo)
	_, initErr := a.send(ctx, initialPrompt)
	a.markReady()
	if initErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("[%s] initial send failed: %v", recipient, initErr)
	}

	// Watch for new messages
	msgCh := a.ChatLog.Watch(ctx, recipient)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] agent stopped", recipient)
			return ctx.Err()

		case msg, ok := <-msgCh:
			if !ok {
				return nil
			}

			// Check if reset is needed before processing
			if timer.Expired() {
				if err := a.performReset(ctx, timer); err != nil {
					log.Printf("[%s] reset failed: %v", recipient, err)
				}
			}

			// Build prompt with the incoming message
			prompt := fmt.Sprintf(
				"チャットログに新しいメッセージがあります:\n\n%s\n\n適切に対応してください。",
				msg.Raw,
			)

			response, err := a.send(ctx, prompt)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("[%s] send failed: %v", recipient, err)
				continue
			}

			if response != "" {
				log.Printf("[%s] response: %s", recipient, truncate(response, 200))
			}
		}
	}
}

// performReset executes the 8-minute context reset protocol.
func (a *Agent) performReset(ctx context.Context, timer *reset.Timer) error {
	recipient := a.ID.String()
	log.Printf("[%s] context reset triggered", recipient)

	// Ask Claude to distill current state
	distilled, err := a.send(ctx, reset.DistillPrompt)
	if err != nil {
		return fmt.Errorf("distill failed: %w", err)
	}

	// Parse and save the memo
	memo := parseDistilledMemo(recipient, distilled)
	memoPath, err := reset.SaveMemo(a.MemosDir, memo)
	if err != nil {
		return fmt.Errorf("save memo: %w", err)
	}
	log.Printf("[%s] memo saved: %s", recipient, filepath.Base(memoPath))

	// Rebuild process with fresh context
	memoContent, _ := os.ReadFile(memoPath)
	freshPrompt := a.buildInitialPrompt(string(memoContent))

	if _, err := a.send(ctx, freshPrompt); err != nil {
		return fmt.Errorf("fresh start failed: %w", err)
	}

	timer.Reset()
	log.Printf("[%s] context reset complete", recipient)
	return nil
}

func (a *Agent) buildInitialPrompt(memo string) string {
	var sb strings.Builder
	sb.WriteString("あなたは以下の役割で動作するエージェントです。\n\n")

	if a.OriginalTask != "" {
		sb.WriteString("## 元の依頼内容\n")
		sb.WriteString(a.OriginalTask)
		sb.WriteString("\n\n")
	}

	if memo != "" {
		sb.WriteString("## 直近の作業メモ（前回のコンテキストリセットから引き継ぎ）\n")
		sb.WriteString(memo)
		sb.WriteString("\n\n")
	}

	sb.WriteString("チャットログのパス: ")
	sb.WriteString(a.ChatLog.Path())
	sb.WriteString("\n\n")

	sb.WriteString("チャットログへの書き込みには以下のコマンドを使用してください:\n")
	sb.WriteString(fmt.Sprintf(
		`echo "[$(date +%%Y-%%m-%%dT%%H:%%M:%%S)] [@宛先] %s: メッセージ内容" >> %s`,
		a.ID.String(), a.ChatLog.Path(),
	))
	sb.WriteString("\n\n")

	sb.WriteString("自分宛のメンションがチャットログに投稿されるのを待ち、適切に対応してください。")

	return sb.String()
}

func parseDistilledMemo(agentID, raw string) reset.WorkMemo {
	memo := reset.WorkMemo{
		AgentID:   agentID,
		Timestamp: time.Now(),
	}

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "STATE:"):
			memo.CurrentState = strings.TrimSpace(strings.TrimPrefix(line, "STATE:"))
		case strings.HasPrefix(line, "DECISIONS:"):
			memo.Decisions = strings.TrimSpace(strings.TrimPrefix(line, "DECISIONS:"))
		case strings.HasPrefix(line, "OPEN:"):
			memo.OpenIssues = strings.TrimSpace(strings.TrimPrefix(line, "OPEN:"))
		case strings.HasPrefix(line, "NEXT:"):
			memo.NextStep = strings.TrimSpace(strings.TrimPrefix(line, "NEXT:"))
		}
	}

	// Fallback: if parsing failed, store entire response as current state
	if memo.CurrentState == "" && memo.Decisions == "" {
		memo.CurrentState = raw
	}

	return memo
}

// send wraps Process.Send with dormancy-aware rate limit handling.
// If a rate limit error is detected, it enters dormancy and retries after wake.
func (a *Agent) send(ctx context.Context, prompt string) (string, error) {
	for {
		if a.Dormancy != nil {
			if err := a.Dormancy.Wait(ctx); err != nil {
				return "", err
			}
		}

		resp, err := a.Process.Send(ctx, prompt)
		if err != nil && a.Dormancy != nil && IsRateLimitError(err) {
			log.Printf("[%s] rate limit detected, entering dormancy", a.ID.String())
			a.Dormancy.Enter(ctx, func(pctx context.Context) error {
				_, perr := a.Process.Send(pctx, "hello")
				return perr
			})
			continue
		}
		return resp, err
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

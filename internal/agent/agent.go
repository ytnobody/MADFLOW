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

type Agent struct {
	ID            AgentID
	Process       Process
	ChatLog       *chatlog.ChatLog
	MemosDir      string
	ResetInterval time.Duration
	SystemPrompt  string
	OriginalTask  string
	Dormancy      *Dormancy
	Throttle      *Throttle
	ready         chan struct{}
	readyOnce     sync.Once
}

type AgentConfig struct {
	ID            AgentID
	Role          Role
	SystemPrompt  string
	Model         string
	WorkDir       string
	ChatLogPath   string
	MemosDir      string
	ResetInterval time.Duration
	BashTimeout   time.Duration
	OriginalTask  string
	Process       Process
	Dormancy      *Dormancy
	Throttle      *Throttle
}

func NewAgent(cfg AgentConfig) *Agent {
	var proc Process
	if cfg.Process != nil {
		proc = cfg.Process
	} else {
		switch {
		case strings.HasPrefix(cfg.Model, "gemini-"):
			proc = NewGeminiAPIProcess(GeminiAPIOptions{
				SystemPrompt: cfg.SystemPrompt,
				Model:        cfg.Model,
				WorkDir:      cfg.WorkDir,
				BashTimeout:  cfg.BashTimeout,
			})
		case strings.HasPrefix(cfg.Model, "anthropic/"):
			proc = NewClaudeAPIProcess(ClaudeAPIOptions{
				SystemPrompt: cfg.SystemPrompt,
				Model:        cfg.Model,
				WorkDir:      cfg.WorkDir,
				BashTimeout:  cfg.BashTimeout,
			})
		default:
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
		Throttle:      cfg.Throttle,
		ready:         make(chan struct{}),
	}
}

func (a *Agent) Ready() <-chan struct{} { return a.ready }

func (a *Agent) markReady() { a.readyOnce.Do(func() { close(a.ready) }) }

func (a *Agent) Run(ctx context.Context) error {
	timer := reset.NewTimer(a.ResetInterval)
	recipient := a.ID.String()
	log.Printf("[%s] agent started", recipient)

	memo, _ := reset.LoadLatestMemo(a.MemosDir, recipient)
	_, initErr := a.send(ctx, a.buildInitialPrompt(memo))
	a.markReady()
	if initErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("[%s] initial send failed: %v", recipient, initErr)
	}

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
			if timer.Expired() {
				if err := a.performReset(ctx, timer); err != nil {
					log.Printf("[%s] reset failed: %v", recipient, err)
				}
			}
			response, err := a.send(ctx, fmt.Sprintf("チャットログに新しいメッセージがあります:\n\n%s\n\n適切に対応してください。", msg.Raw))
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

func (a *Agent) performReset(ctx context.Context, timer *reset.Timer) error {
	recipient := a.ID.String()
	log.Printf("[%s] context reset triggered", recipient)

	distilled, err := a.send(ctx, reset.DistillPrompt)
	if err != nil {
		return fmt.Errorf("distill failed: %w", err)
	}

	memo := parseDistilledMemo(recipient, distilled)
	memoPath, err := reset.SaveMemo(a.MemosDir, memo)
	if err != nil {
		return fmt.Errorf("save memo: %w", err)
	}
	log.Printf("[%s] memo saved: %s", recipient, filepath.Base(memoPath))

	memoContent, _ := os.ReadFile(memoPath)
	if _, err := a.send(ctx, a.buildInitialPrompt(string(memoContent))); err != nil {
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
	sb.WriteString(fmt.Sprintf(`echo "[$(date +%%Y-%%m-%%dT%%H:%%M:%%S)] [@宛先] %s: メッセージ内容" >> %s`, a.ID.String(), a.ChatLog.Path()))
	sb.WriteString("\n\n")
	if a.OriginalTask != "" {
		// イシューが割り当て済みの場合は即座に実装開始を指示する。
		// これにより、チャットログ経由の割り当てメッセージが届かなかった場合でも
		// エンジニアが作業を開始できる。
		sb.WriteString("上記の依頼内容に従い、すぐに実装を開始してください。実装完了後は監督に報告してください。その後もチャットログへのメンションを監視し、追加の指示に対応してください。")
	} else {
		sb.WriteString("自分宛のメンションがチャットログに投稿されるのを待ち、適切に対応してください。")
	}
	return sb.String()
}

func parseDistilledMemo(agentID, raw string) reset.WorkMemo {
	memo := reset.WorkMemo{AgentID: agentID, Timestamp: time.Now()}
	for _, line := range strings.Split(raw, "\n") {
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
	if memo.CurrentState == "" && memo.Decisions == "" {
		memo.CurrentState = raw
	}
	return memo
}

const (
	sendMaxRetries    = 3
	sendRetryBaseWait = 2 * time.Second
)

func (a *Agent) send(ctx context.Context, prompt string) (string, error) {
	for {
		if err := a.Throttle.Wait(ctx); err != nil {
			return "", err
		}
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
		if err != nil && !IsRateLimitError(err) {
			// Retry transient errors (network, API 500, etc.) with exponential backoff.
			resp, err = a.retrySend(ctx, prompt, err)
		}
		return resp, err
	}
}

// retrySend retries a failed send up to sendMaxRetries times with exponential backoff.
// It is only called for non-rate-limit errors; rate limits are handled by the dormancy system.
func (a *Agent) retrySend(ctx context.Context, prompt string, lastErr error) (string, error) {
	wait := sendRetryBaseWait
	for attempt := 1; attempt <= sendMaxRetries; attempt++ {
		log.Printf("[%s] send failed (attempt %d/%d): %v, retrying in %v", a.ID.String(), attempt, sendMaxRetries, lastErr, wait)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
		resp, err := a.Process.Send(ctx, prompt)
		if err == nil {
			return resp, nil
		}
		if IsRateLimitError(err) {
			// Hand off to the dormancy system on the next loop iteration.
			return "", err
		}
		lastErr = err
		wait *= 2
	}
	return "", lastErr
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

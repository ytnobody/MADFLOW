package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const baseConfig = `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "/tmp/test-app"

[agent]
context_reset_minutes = 5

[agent.models]
superintendent = "claude-opus-4-6"

[branches]
main = "main"
develop = "develop"
`

const updatedConfig = `
[project]
name = "test-app"

[[project.repos]]
name = "main"
path = "/tmp/test-app"

[agent]
context_reset_minutes = 10

[agent.models]
superintendent = "claude-opus-4-6"

[branches]
main = "main"
develop = "develop"
`

const invalidConfig = `
[project]
# Missing required name field

[agent]
context_reset_minutes = 5
`

func TestWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")

	// Write initial config.
	if err := os.WriteFile(path, []byte(baseConfig), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	w := NewWatcher(path)
	ch := w.Watch(ctx)

	// Give the watcher time to record the initial mod time.
	time.Sleep(100 * time.Millisecond)

	// Update the config file.
	// Use a small sleep to ensure the OS mod time advances.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte(updatedConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Expect to receive the updated config.
	select {
	case newCfg, ok := <-ch:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		if newCfg.Agent.ContextResetMinutes != 10 {
			t.Errorf("expected context_reset_minutes=10 after reload, got %d", newCfg.Agent.ContextResetMinutes)
		}
	case <-ctx.Done():
		t.Fatal("timeout: did not receive config update")
	}
}

func TestWatcher_InvalidConfigNotEmitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")

	// Write initial valid config.
	if err := os.WriteFile(path, []byte(baseConfig), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	w := NewWatcher(path)
	ch := w.Watch(ctx)

	// Allow watcher to snapshot initial mod time.
	time.Sleep(100 * time.Millisecond)

	// Write an invalid config (missing required project.name).
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte(invalidConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// No value should be emitted within the timeout.
	select {
	case cfg, ok := <-ch:
		if ok {
			t.Errorf("expected no update for invalid config, but received one with project.name=%q", cfg.Project.Name)
		}
		// Channel closed (ctx expired) is acceptable.
	case <-ctx.Done():
		// Expected: no update emitted for invalid config.
	}
}

func TestWatcher_NoChangeNoEmit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")

	// Write initial config.
	if err := os.WriteFile(path, []byte(baseConfig), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	w := NewWatcher(path)
	ch := w.Watch(ctx)

	// No file change; nothing should be emitted.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received unexpected config update when file was not changed")
		}
	case <-ctx.Done():
		// Expected: no update.
	}
}

func TestWatcher_ChannelClosedOnCtxCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "madflow.toml")
	if err := os.WriteFile(path, []byte(baseConfig), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := NewWatcher(path)
	ch := w.Watch(ctx)

	// Cancel immediately.
	cancel()

	// Channel should close.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after ctx cancel")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout: channel not closed after ctx cancel")
	}
}

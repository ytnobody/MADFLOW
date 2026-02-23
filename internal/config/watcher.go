package config

import (
	"context"
	"log"
	"os"
	"time"
)

const watchInterval = 500 * time.Millisecond

// Watcher monitors a config file for changes and emits validated new configs.
type Watcher struct {
	path string
}

// NewWatcher creates a new Watcher for the given config file path.
func NewWatcher(path string) *Watcher {
	return &Watcher{path: path}
}

// Watch polls the config file for changes and sends validated new configs to
// the returned channel. The channel is closed when ctx is cancelled.
// If the file changes but validation fails, the error is logged and the
// current config remains active (no value sent to channel).
func (w *Watcher) Watch(ctx context.Context) <-chan *Config {
	ch := make(chan *Config, 1)

	go func() {
		defer close(ch)

		// Record the current mod time so we only emit on actual changes.
		lastModTime := w.currentModTime()

		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				modTime := w.currentModTime()
				if modTime.IsZero() || modTime.Equal(lastModTime) {
					continue
				}

				// File changed; try to load and validate.
				newCfg, err := Load(w.path)
				if err != nil {
					log.Printf("[config watcher] reload failed (keeping current config): %v", err)
					// Update modTime so we don't spam the log on every tick.
					lastModTime = modTime
					continue
				}

				lastModTime = modTime
				log.Printf("[config watcher] config reloaded from %s", w.path)

				// Non-blocking send: if the consumer is slow we drop the older
				// pending update and replace it with the latest one.
				select {
				case ch <- newCfg:
				case <-ch: // drain stale pending update
					ch <- newCfg
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch
}

// currentModTime returns the modification time of the watched file.
// Returns zero time if the file cannot be stat'd.
func (w *Watcher) currentModTime() time.Time {
	info, err := os.Stat(w.path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

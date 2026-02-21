package cache_test

import (
	"testing"
	"time"

	"github.com/ytnobody/madflow/internal/cache"
)

func TestCacheEntry_IsExpired(t *testing.T) {
	t.Run("not expired", func(t *testing.T) {
		entry := &cache.CacheEntry{
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}
		if entry.IsExpired() {
			t.Error("entry should not be expired")
		}
	})

	t.Run("expired", func(t *testing.T) {
		entry := &cache.CacheEntry{
			ExpiresAt: time.Now().Add(-1 * time.Minute),
		}
		if !entry.IsExpired() {
			t.Error("entry should be expired")
		}
	})
}

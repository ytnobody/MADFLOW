package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

type CacheEntry struct {
	IssueID     string    `json:"issue_id"`
	CacheName   string    `json:"cache_name"`
	Model       string    `json:"model"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	TokenCount  int       `json:"token_count"`
	ContentHash string    `json:"content_hash"`
}

func (e *CacheEntry) IsExpired() bool { return time.Now().After(e.ExpiresAt) }

type Manager struct {
	apiKey   string
	model    string
	client   *genai.Client
	mu       sync.RWMutex
	caches   map[string]*CacheEntry
	storeDir string
	ttl      time.Duration
}

func NewManager(ctx context.Context, apiKey, model, storeDir string, ttlMinutes int) (*Manager, error) {
	if ttlMinutes <= 0 {
		ttlMinutes = 30
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}
	m := &Manager{apiKey: apiKey, model: model, client: client, caches: make(map[string]*CacheEntry), storeDir: storeDir, ttl: time.Duration(ttlMinutes) * time.Minute}
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		return nil, fmt.Errorf("create cache store dir: %w", err)
	}
	m.loadFromDisk(ctx)
	return m, nil
}

func (m *Manager) Client() *genai.Client { return m.client }

func (m *Manager) GetOrCreate(ctx context.Context, issueID string, content *CacheContent) (*CacheEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	newHash := (&Collector{}).Hash(content)
	existing, ok := m.caches[issueID]
	if ok && !existing.IsExpired() && existing.ContentHash == newHash {
		log.Printf("[cache] reusing cache for issue %s: %s", issueID, existing.CacheName)
		return existing, nil
	}
	if ok {
		log.Printf("[cache] invalidating outdated cache for issue %s", issueID)
		if err := m.invalidateLocked(ctx, issueID); err != nil {
			log.Printf("[cache] invalidate failed: %v", err)
		}
	}
	log.Printf("[cache] creating new cache for issue %s", issueID)
	entry, err := m.createCache(ctx, issueID, content, newHash)
	if err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}
	m.caches[issueID] = entry
	m.saveToDisk(issueID, entry)
	return entry, nil
}

func (m *Manager) Refresh(ctx context.Context, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.caches[issueID]
	if !ok || entry.IsExpired() {
		return fmt.Errorf("no valid cache found for issue %s", issueID)
	}
	newExpireTime := time.Now().Add(m.ttl)
	if _, err := m.client.Caches.Update(ctx, entry.CacheName, &genai.UpdateCachedContentConfig{ExpireTime: newExpireTime}); err != nil {
		return fmt.Errorf("update cache TTL: %w", err)
	}
	entry.ExpiresAt = newExpireTime
	m.saveToDisk(issueID, entry)
	log.Printf("[cache] refreshed cache for issue %s", issueID)
	return nil
}

func (m *Manager) Invalidate(ctx context.Context, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.invalidateLocked(ctx, issueID)
}

func (m *Manager) invalidateLocked(ctx context.Context, issueID string) error {
	entry, ok := m.caches[issueID]
	if !ok {
		return nil
	}
	if _, err := m.client.Caches.Delete(ctx, entry.CacheName, &genai.DeleteCachedContentConfig{}); err != nil && !isNotFoundError(err) {
		return fmt.Errorf("delete cache %s: %w", entry.CacheName, err)
	}
	delete(m.caches, issueID)
	m.deleteDiskEntry(issueID)
	return nil
}

func (m *Manager) Cleanup(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []string
	for issueID, entry := range m.caches {
		if entry.IsExpired() {
			if err := m.invalidateLocked(ctx, issueID); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) createCache(ctx context.Context, issueID string, content *CacheContent, hash string) (*CacheEntry, error) {
	var sb strings.Builder
	sb.WriteString("## プロジェクトコードベース\n\n")
	for _, f := range content.ProjectFiles {
		lang := extToLang(filepath.Ext(f.Path))
		fmt.Fprintf(&sb, "### %s\n```%s\n%s\n```\n\n", f.Path, lang, f.Content)
	}
	contents := []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: sb.String()}}}}
	config := &genai.CreateCachedContentConfig{TTL: m.ttl, Contents: contents}
	if content.SystemInstruction != "" {
		config.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: content.SystemInstruction}}}
	}
	result, err := m.client.Caches.Create(ctx, m.model, config)
	if err != nil {
		return nil, fmt.Errorf("caches.Create: %w", err)
	}
	tokenCount := 0
	if result.UsageMetadata != nil {
		tokenCount = int(result.UsageMetadata.TotalTokenCount)
	}
	expiresAt := time.Now().Add(m.ttl)
	if !result.ExpireTime.IsZero() {
		expiresAt = result.ExpireTime
	}
	return &CacheEntry{IssueID: issueID, CacheName: result.Name, Model: m.model, CreatedAt: time.Now(), ExpiresAt: expiresAt, TokenCount: tokenCount, ContentHash: hash}, nil
}

func (m *Manager) loadFromDisk(ctx context.Context) {
	entries, err := os.ReadDir(m.storeDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		issueID := strings.TrimSuffix(e.Name(), ".json")
		path := filepath.Join(m.storeDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var entry CacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.IsExpired() {
			os.Remove(path)
			continue
		}
		if _, err = m.client.Caches.Get(ctx, entry.CacheName, &genai.GetCachedContentConfig{}); err != nil {
			log.Printf("[cache] cache %s no longer exists, removing", entry.CacheName)
			os.Remove(path)
			continue
		}
		m.caches[issueID] = &entry
		log.Printf("[cache] loaded cache for issue %s: %s", issueID, entry.CacheName)
	}
}

func (m *Manager) saveToDisk(issueID string, entry *CacheEntry) {
	path := filepath.Join(m.storeDir, issueID+".json")
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		log.Printf("[cache] marshal failed for %s: %v", issueID, err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("[cache] write failed for %s: %v", issueID, err)
	}
}

func (m *Manager) deleteDiskEntry(issueID string) {
	os.Remove(filepath.Join(m.storeDir, issueID+".json"))
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "404") || strings.Contains(s, "not found") || strings.Contains(s, "Not Found")
}

func extToLang(ext string) string {
	m := map[string]string{".go": "go", ".md": "markdown", ".toml": "toml", ".json": "json", ".yaml": "yaml", ".yml": "yaml", ".sh": "bash", ".py": "python", ".js": "javascript", ".ts": "typescript"}
	if lang, ok := m[ext]; ok {
		return lang
	}
	return ""
}

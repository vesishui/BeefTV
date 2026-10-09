// Content-addressed upload cache.
//
// Reference media is re-sent on every generation, and the same picture is often
// reused across a canvas. Hashing the bytes lets an identical payload reuse the
// previously uploaded URL instead of paying for another upload.
//
// This mirrors the browser-side "public-upload-url-cache-v1" cache used by the
// earlier canvas work, with two differences: entries live in the workspace
// instead of localStorage, and a cached URL is re-verified before reuse because
// free hosts expire links.
package mediahost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheFileName stores the hash -> URL mapping under the workspace data dir.
const CacheFileName = "media-hosting-cache.json"

// DefaultCacheEntries bounds the cache so a long-lived workspace cannot grow
// without limit. 2000 entries covers far more than the browser-side 500.
const DefaultCacheEntries = 2000

// CacheEntry is one uploaded object.
type CacheEntry struct {
	URL       string `json:"url"`
	AssetID   string `json:"assetId,omitempty"`
	Provider  string `json:"provider"`
	Kind      string `json:"kind"`
	Bytes     int64  `json:"bytes"`
	UpdatedAt string `json:"updatedAt"`
}

// Cache persists upload results keyed by content hash.
type Cache struct {
	path    string
	limit   int
	mu      sync.Mutex
	entries map[string]CacheEntry
	loaded  bool
}

// NewCache builds a cache rooted at the workspace data directory.
func NewCache(dataDir string) (*Cache, error) {
	trimmed := strings.TrimSpace(dataDir)
	if trimmed == "" {
		return nil, errors.New("本地工作区数据目录不能为空")
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, fmt.Errorf("解析本地工作区数据目录失败: %w", err)
	}
	return &Cache{
		path:    filepath.Join(filepath.Clean(absolute), CacheFileName),
		limit:   DefaultCacheEntries,
		entries: map[string]CacheEntry{},
	}, nil
}

// HashContent returns the cache key for a payload. Identical bytes always map
// to the same key regardless of filename, so renaming a file still hits.
func HashContent(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Lookup returns the stored entry for a hash.
func (c *Cache) Lookup(hash string) (CacheEntry, bool) {
	if c == nil || hash == "" {
		return CacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(); err != nil {
		return CacheEntry{}, false
	}
	entry, ok := c.entries[hash]
	return entry, ok
}

// Store records an upload result and trims the cache to its limit.
func (c *Cache) Store(hash string, entry CacheEntry) error {
	if c == nil || hash == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(); err != nil {
		return err
	}
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	c.entries[hash] = entry
	c.trimLocked()
	return c.persistLocked()
}

// Forget drops a stale entry, used when a cached URL no longer resolves.
func (c *Cache) Forget(hash string) error {
	if c == nil || hash == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(); err != nil {
		return err
	}
	if _, ok := c.entries[hash]; !ok {
		return nil
	}
	delete(c.entries, hash)
	return c.persistLocked()
}

// Len reports how many entries are cached, for diagnostics.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(); err != nil {
		return 0
	}
	return len(c.entries)
}

func (c *Cache) loadLocked() error {
	if c.loaded {
		return nil
	}
	c.loaded = true
	payload, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var decoded map[string]CacheEntry
	if err := json.Unmarshal(payload, &decoded); err != nil {
		// A corrupt cache must never block generation; rebuild it.
		c.entries = map[string]CacheEntry{}
		return nil
	}
	c.entries = decoded
	return nil
}

func (c *Cache) trimLocked() {
	if c.limit <= 0 || len(c.entries) <= c.limit {
		return
	}
	type pair struct {
		hash string
		at   string
	}
	ordered := make([]pair, 0, len(c.entries))
	for hash, entry := range c.entries {
		ordered = append(ordered, pair{hash: hash, at: entry.UpdatedAt})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].at < ordered[j].at })
	for _, item := range ordered[:len(ordered)-c.limit] {
		delete(c.entries, item.hash)
	}
}

func (c *Cache) persistLocked() error {
	payload, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(c.path), ".media-cache-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, c.path)
}

// URLAlive performs a lightweight check that a previously uploaded URL still
// resolves. Free hosts expire links, so a hit is only reused when it answers.
func (u *Uploader) URLAlive(ctx context.Context, url string) bool {
	if strings.TrimSpace(url) == "" {
		return false
	}
	client := u.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		request, err := http.NewRequest(method, url, nil)
		if err != nil {
			return false
		}
		response, err := client.Do(request.WithContext(ctx))
		if err != nil {
			continue
		}
		response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return true
		}
	}
	return false
}

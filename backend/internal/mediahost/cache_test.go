package mediahost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHashContentIsStableAcrossNames(t *testing.T) {
	body := []byte("identical-bytes")
	if HashContent(body) != HashContent(append([]byte{}, body...)) {
		t.Fatal("相同内容必须得到相同 hash，文件名不应参与")
	}
	if HashContent(body) == HashContent([]byte("different")) {
		t.Fatal("不同内容不应撞 hash")
	}
}

func TestCacheLookupAndStoreRoundTrip(t *testing.T) {
	cache, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Lookup("missing"); ok {
		t.Fatal("空缓存不应有命中")
	}
	if err := cache.Store("abc", CacheEntry{URL: "https://cdn.example.com/a.png"}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	entry, ok := cache.Lookup("abc")
	if !ok {
		t.Fatal("写入后应能命中")
	}
	if entry.URL != "https://cdn.example.com/a.png" {
		t.Fatalf("URL 不一致: %q", entry.URL)
	}
	if entry.UpdatedAt == "" {
		t.Fatal("应记录写入时间，用于淘汰")
	}
}

func TestCachePersistsAcrossInstances(t *testing.T) {
	dataDir := t.TempDir()
	first, err := NewCache(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Store("persisted", CacheEntry{URL: "https://cdn.example.com/keep.png"}); err != nil {
		t.Fatal(err)
	}
	second, err := NewCache(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := second.Lookup("persisted")
	if !ok {
		t.Fatal("缓存必须持久化到磁盘，换实例后仍可命中")
	}
	if entry.URL != "https://cdn.example.com/keep.png" {
		t.Fatalf("持久化 URL 不一致: %q", entry.URL)
	}
}

func TestCacheTrimsToLimit(t *testing.T) {
	cache, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache.limit = 3
	for _, key := range []string{"a", "b", "c", "d", "e"} {
		if err := cache.Store(key, CacheEntry{URL: "https://cdn.example.com/" + key}); err != nil {
			t.Fatal(err)
		}
	}
	if got := cache.Len(); got > 3 {
		t.Fatalf("缓存应裁剪到 3 条，实际 %d 条", got)
	}
}

func TestCacheForgetRemovesEntry(t *testing.T) {
	cache, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Store("gone", CacheEntry{URL: "https://cdn.example.com/x.png"}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Forget("gone"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, ok := cache.Lookup("gone"); ok {
		t.Fatal("删除后不应命中")
	}
}

func TestUploadReusesCachedURLAvoidingSecondUpload(t *testing.T) {
	var uploads int32
	// serverURL is filled in after the test server starts; the handler runs
	// later, so the closure can read it safely.
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		atomic.AddInt32(&uploads, 1)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, header, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"url": serverURL + "/cdn/" + header.Filename})
	}))
	defer server.Close()
	serverURL = server.URL

	cache, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	uploader := NewUploader()
	uploader.Cache = cache
	uploader.Log = func(format string, args ...any) { hits = append(hits, format) }

	config := Config{Provider: ProviderCustom, Endpoint: server.URL}
	body := []byte("same-image-bytes")

	first, err := uploader.Upload(context.Background(), config, MediaKindImage, "a.png", "image/png", body)
	if err != nil {
		t.Fatalf("首次上传失败: %v", err)
	}
	second, err := uploader.Upload(context.Background(), config, MediaKindImage, "renamed.png", "image/png", body)
	if err != nil {
		t.Fatalf("第二次上传失败: %v", err)
	}

	if first != second {
		t.Fatalf("相同内容应复用同一 URL，得到 %q 和 %q", first, second)
	}
	if got := atomic.LoadInt32(&uploads); got != 1 {
		t.Fatalf("相同内容只应上传一次，实际上传 %d 次", got)
	}
	joined := strings.Join(hits, "\n")
	if !strings.Contains(joined, "命中缓存") {
		t.Fatalf("日志应打印缓存命中，实际日志:\n%s", joined)
	}
}

func TestUploadReUploadsWhenCachedURLCannotBeFetched(t *testing.T) {
	var uploads int32
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt32(&uploads, 1)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"url": serverURL + "/cdn/fresh.png"})
	}))
	defer server.Close()
	serverURL = server.URL

	cache, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("expired-image")
	// Pre-seed a cached URL that no longer resolves, as a free host link would
	// after its retention window.
	if err := cache.Store(HashContent(body), CacheEntry{URL: serverURL + "/cdn/expired.png"}); err != nil {
		t.Fatal(err)
	}

	uploader := NewUploader()
	uploader.Cache = cache
	uploader.Log = func(string, ...any) {}

	link, err := uploader.Upload(context.Background(), Config{Provider: ProviderCustom, Endpoint: server.URL}, MediaKindImage, "a.png", "image/png", body)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if strings.Contains(link, "expired") {
		t.Fatalf("死链必须重新上传，得到 %q", link)
	}
	if got := atomic.LoadInt32(&uploads); got != 1 {
		t.Fatalf("应重新上传一次，实际 %d 次", got)
	}
}

func TestUploadWorksWithoutCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.example.com/ok.png"})
	}))
	defer server.Close()

	// No Cache assigned: the upload path must still work.
	uploader := NewUploader()
	link, err := uploader.Upload(context.Background(), Config{Provider: ProviderCustom, Endpoint: server.URL}, MediaKindImage, "a.png", "image/png", []byte("x"))
	if err != nil {
		t.Fatalf("未配置缓存时上传不应失败: %v", err)
	}
	if link != "https://cdn.example.com/ok.png" {
		t.Fatalf("得到 %q", link)
	}
}

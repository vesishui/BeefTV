package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/mediahost"
)

// 派普自带免费图床 POST /v1/media/upload：multipart 的 file 字段 + Bearer，
// 返回体里的 url 可以直接回填到视频生成的参考图数组。
func TestPaipuMediaUploadPostsMultipartFileAndReturnsURL(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var mu sync.Mutex
	var calls []string
	var received []byte

	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/v1/media/upload" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer paipu_test" {
			t.Errorf("Authorization = %q，应为 Bearer paipu_test", got)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("Content-Type = %q，应为 multipart/form-data", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Errorf("解析 multipart 失败: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("缺少 file 字段: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		if header.Filename != "shot.png" {
			t.Errorf("文件名 = %q，应为 shot.png", header.Filename)
		}
		received, _ = io.ReadAll(file)
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.paipu.test/ref.png"})
	}))

	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	link, err := adapter.UploadForGeneration(context.Background(), generation.Config{
		BaseURL:       server.URL,
		APIKey:        "paipu_test",
		InterfaceType: paipuVideoProtocolID,
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("paipu-image-bytes"))
	if err != nil {
		t.Fatalf("派普图床上传失败: %v", err)
	}
	if link != "https://cdn.paipu.test/ref.png" {
		t.Fatalf("图床链接 = %q", link)
	}
	if string(received) != "paipu-image-bytes" {
		t.Fatalf("上游收到的字节不一致: %q", received)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(calls, "\n") != "POST /v1/media/upload" {
		t.Fatalf("派普图床调用序列错误: %v", calls)
	}
}

// 请求必须打到 /v1/media/upload；base URL 无论填厂商根地址还是带版本前缀，
// 都不能被注入第二个 /v1。
func TestPaipuMediaUploadKeepsSingleVersionPrefix(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var mu sync.Mutex
	var paths []string
	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.paipu.test/ref.png"})
	}))

	for _, base := range []string{server.URL, server.URL + "/v1"} {
		if _, err := uploadPaipuMedia(context.Background(), generation.Config{
			BaseURL: base,
			APIKey:  "paipu_test",
		}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("bytes")); err != nil {
			t.Fatalf("base=%q 上传失败: %v", base, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 {
		t.Fatalf("调用次数 = %d，应为 2", len(paths))
	}
	for _, path := range paths {
		if path != "/v1/media/upload" {
			t.Fatalf("端点路径 = %q，应为 /v1/media/upload", path)
		}
	}
}

// 文档示例是 {"url": ...}，但网关也可能包一层 data，两者都要能取到。
func TestPaipuMediaUploadReadsNestedResponseURL(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"url": "https://cdn.paipu.test/nested.png"}})
	}))

	link, err := uploadPaipuMedia(context.Background(), generation.Config{
		BaseURL: server.URL,
		APIKey:  "paipu_test",
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("bytes"))
	if err != nil {
		t.Fatalf("嵌套响应解析失败: %v", err)
	}
	if link != "https://cdn.paipu.test/nested.png" {
		t.Fatalf("链接 = %q", link)
	}
}

func TestPaipuMediaUploadRejectsUnusableLink(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "缺少 url", payload: `{"ok":true}`},
		{name: "空 url", payload: `{"url":"  "}`},
		{name: "带凭据", payload: `{"url":"https://user:pass@cdn.paipu.test/ref.png"}`},
		{name: "非 HTTP 协议", payload: `{"url":"ftp://cdn.paipu.test/ref.png"}`},
		{name: "相对路径", payload: `{"url":"/uploads/ref.png"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
			t.Setenv("NO_PROXY", "127.0.0.1,localhost")
			t.Setenv("no_proxy", "127.0.0.1,localhost")
			server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, test.payload)
			}))
			if _, err := uploadPaipuMedia(context.Background(), generation.Config{
				BaseURL: server.URL,
				APIKey:  "paipu_test",
			}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("bytes")); err == nil {
				t.Fatal("不可用的图床链接应被拒绝")
			}
		})
	}
}

// 派普图床只接收图片。参考视频和音频是生成接口的参数（videos[] / audios[]），
// 不是图床素材，绝不能推到 /v1/media/upload 去。
func TestPaipuImageHostIsNeverUsedForNonImageReferenceMedia(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var mu sync.Mutex
	var paipuCalls int
	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/media/upload":
			mu.Lock()
			paipuCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.paipu.test/ref"})
		case "/custom":
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://custom.test/ref"})
		default:
			http.NotFound(w, r)
		}
	}))

	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	if err := adapter.store.Write(mediahost.Config{
		Provider:        mediahost.ProviderCustom,
		Endpoint:        server.URL + "/custom",
		CustomFileField: "file",
	}); err != nil {
		t.Fatalf("写入备用图床配置失败: %v", err)
	}

	config := generation.Config{BaseURL: server.URL, APIKey: "paipu_test", InterfaceType: paipuVideoProtocolID}
	for _, test := range []struct {
		kind        mediahost.MediaKind
		filename    string
		contentType string
	}{
		{mediahost.MediaKindVideo, "clip.mp4", "video/mp4"},
		{mediahost.MediaKindAudio, "voice.mp3", "audio/mpeg"},
	} {
		link, err := adapter.UploadForGeneration(context.Background(), config, test.kind, test.filename, test.contentType, []byte("bytes"))
		if err != nil {
			t.Fatalf("%s 应回退到配置图床，却失败: %v", test.kind, err)
		}
		if link != "https://custom.test/ref" {
			t.Fatalf("%s 应使用配置图床，链接 = %q", test.kind, link)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if paipuCalls != 0 {
		t.Fatalf("非图片素材不应调用派普图床，调用了 %d 次", paipuCalls)
	}
}

// 图片仍然优先走派普图床，用户不配置任何图床也能用。
func TestPaipuHostingPolicyDoesNotRequireFallbackEnabled(t *testing.T) {
	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	decision, err := adapter.HostingPolicy(context.Background(), "paipu-channel", paipuVideoProtocolID)
	if err != nil {
		t.Fatalf("读取派普图床策略失败: %v", err)
	}
	if !decision.Host || decision.Reason != "protocol_requires_url_upstream_upload" {
		t.Fatalf("派普应优先使用官方图床: %#v", decision)
	}
}

func TestPaipuOfficialUploadSkipsFallbackWhenItSucceeds(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var fallbackCalls int
	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/media/upload":
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.paipu.test/ref.png"})
		case r.URL.Path == "/fallback":
			fallbackCalls++
			http.Error(w, "fallback must not run", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))

	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	if err := adapter.store.Write(mediahost.Config{
		Provider:        mediahost.ProviderCustom,
		Endpoint:        server.URL + "/fallback",
		CustomFileField: "file",
	}); err != nil {
		t.Fatalf("写入备用图床配置失败: %v", err)
	}
	link, err := adapter.UploadForGeneration(context.Background(), generation.Config{
		BaseURL:       server.URL,
		APIKey:        "paipu_test",
		InterfaceType: paipuVideoProtocolID,
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("bytes"))
	if err != nil {
		t.Fatalf("派普图床成功时不应失败: %v", err)
	}
	if link != "https://cdn.paipu.test/ref.png" {
		t.Fatalf("链接 = %q", link)
	}
	if fallbackCalls != 0 {
		t.Fatalf("派普图床成功后不应调用备用图床，调用了 %d 次", fallbackCalls)
	}
}

func TestPaipuOfficialFailureFallsBackToConfiguredHost(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	official := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"image host unavailable"}`, http.StatusBadGateway)
	}))
	fallback := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if string(body) != "image-bytes" {
			t.Errorf("备用图床收到的图片不一致: %q", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://fallback.test/ref.png"})
	}))

	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	if err := adapter.store.Write(mediahost.Config{
		Provider:        mediahost.ProviderCustom,
		Endpoint:        fallback.URL,
		CustomURLPath:   "url",
		CustomFileField: "file",
	}); err != nil {
		t.Fatalf("写入备用图床配置失败: %v", err)
	}

	link, err := adapter.UploadForGeneration(context.Background(), generation.Config{
		BaseURL:       official.URL,
		APIKey:        "paipu_test",
		InterfaceType: paipuVideoProtocolID,
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("image-bytes"))
	if err != nil {
		t.Fatalf("派普图床失败后备用图床也失败: %v", err)
	}
	if link != "https://fallback.test/ref.png" {
		t.Fatalf("备用图床链接 = %q", link)
	}
}

// 上游错误信息里可能带图床 URL 和签名查询串，不能原样透出到任务错误里。
func TestPaipuFailureRedactsHostedURLFromTaskError(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rejected https://img.paipu.test/private?signature=do-not-leak", http.StatusForbidden)
	}))

	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	_, err = adapter.UploadForGeneration(context.Background(), generation.Config{
		BaseURL:       server.URL,
		APIKey:        "paipu_test",
		InterfaceType: paipuVideoProtocolID,
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("image-bytes"))
	if err == nil {
		t.Fatal("图床失败且未配置后备图床时应返回错误")
	}
	if strings.Contains(err.Error(), "do-not-leak") || strings.Contains(err.Error(), "http://") {
		t.Fatalf("任务错误泄漏图床 URL: %v", err)
	}
}

func TestPaipuCacheReusesUploadedURLWithoutSecondUpload(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var mu sync.Mutex
	var uploadCalls int
	var server *httptest.Server
	server = newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/media/upload":
			mu.Lock()
			uploadCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"url": server.URL + "/media/ref.png"})
		case r.URL.Path == "/media/ref.png":
			_, _ = io.WriteString(w, "png")
		default:
			http.NotFound(w, r)
		}
	}))

	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	config := generation.Config{BaseURL: server.URL, APIKey: "paipu_test", InterfaceType: paipuVideoProtocolID}
	body := []byte("cache-me")
	first, err := adapter.UploadForGeneration(context.Background(), config, mediahost.MediaKindImage, "shot.png", "image/png", body)
	if err != nil {
		t.Fatalf("首次图床上传失败: %v", err)
	}
	second, err := adapter.UploadForGeneration(context.Background(), config, mediahost.MediaKindImage, "shot.png", "image/png", body)
	if err != nil {
		t.Fatalf("缓存命中失败: %v", err)
	}
	if first != second || first != server.URL+"/media/ref.png" {
		t.Fatalf("缓存链接不一致: first=%q second=%q", first, second)
	}
	mu.Lock()
	defer mu.Unlock()
	if uploadCalls != 1 {
		t.Fatalf("相同素材应只上传一次，实际上传 %d 次", uploadCalls)
	}
}

// 其它协议不受影响：仍然走用户配置的图床。
func TestNonPaipuProtocolsStillUseConfiguredHost(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var paipuCalls int
	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/media/upload":
			paipuCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.paipu.test/ref.png"})
		case "/custom":
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://custom.test/ref.png"})
		default:
			http.NotFound(w, r)
		}
	}))
	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	if err := adapter.store.Write(mediahost.Config{
		Provider:        mediahost.ProviderCustom,
		Endpoint:        server.URL + "/custom",
		CustomFileField: "file",
	}); err != nil {
		t.Fatalf("写入图床配置失败: %v", err)
	}
	link, err := adapter.UploadForGeneration(context.Background(), generation.Config{
		BaseURL:       server.URL,
		APIKey:        "other_test",
		InterfaceType: "seedance-videos-compatible",
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("bytes"))
	if err != nil {
		t.Fatalf("非派普协议上传失败: %v", err)
	}
	if link != "https://custom.test/ref.png" {
		t.Fatalf("非派普协议应使用配置图床，链接 = %q", link)
	}
	if paipuCalls != 0 {
		t.Fatalf("非派普协议不应调用派普图床，调用了 %d 次", paipuCalls)
	}
}

package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/mediahost"
)

func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听 IPv4 测试端口失败: %v", err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestSparkCineOfficialUploadUsesSignedPutAndComplete(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var mu sync.Mutex
	var uploaded []byte
	var calls []string

	var server *httptest.Server
	server = newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("申请上传请求不是 JSON: %v", err)
			}
			sha256, _ := request["sha256"].(string)
			if request["content_type"] != "image/png" ||
				request["filename"] != "shot.png" ||
				request["byte_length"] != float64(len("image-bytes")) ||
				len(sha256) != 64 {
				t.Errorf("申请上传参数不正确: %#v", request)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id": "asset-image-1",
				"upload": map[string]any{
					"url":     server.URL + "/signed/asset-image-1",
					"method":  "PUT",
					"headers": map[string]any{"X-Signed": "yes"},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/signed/asset-image-1":
			if r.Header.Get("X-Signed") != "yes" {
				t.Errorf("签名请求头未转发")
			}
			uploaded, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads/asset-image-1/complete":
			_ = json.NewEncoder(w).Encode(map[string]any{"asset": map[string]any{"url": "https://cdn.sparkcine.test/image.png"}})
		default:
			http.NotFound(w, r)
		}
	}))

	asset, err := uploadSparkCineAsset(context.Background(), generation.Config{
		BaseURL: server.URL,
		APIKey:  "sc_test",
	}, mediahost.MediaKindImage, "shot", "image/png", []byte("image-bytes"))
	if err != nil {
		t.Fatalf("官方上传失败: %v", err)
	}
	if asset.ID != "asset-image-1" || asset.URL != "https://cdn.sparkcine.test/image.png" {
		t.Fatalf("官方上传结果 = %#v", asset)
	}
	if string(uploaded) != "image-bytes" {
		t.Fatalf("PUT 字节不一致: %q", uploaded)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"POST /api/open/v1/uploads",
		"PUT /signed/asset-image-1",
		"POST /api/open/v1/uploads/asset-image-1/complete",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("官方上传调用顺序错误: %v", calls)
	}
}

func TestSparkCineFlatSignedURLIsPutBeforePublicURLIsReturned(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var server *httptest.Server
	var putCalls int
	server = newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "asset-flat-1",
				"url":     server.URL + "/signed/asset-flat-1?signature=temporary",
				"headers": map[string]any{"X-Signed": "yes"},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/signed/asset-flat-1":
			putCalls++
			if r.URL.Query().Get("signature") != "temporary" || r.Header.Get("X-Signed") != "yes" {
				t.Errorf("平铺签名上传信息未正确使用: url=%s headers=%v", r.URL.String(), r.Header)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads/asset-flat-1/complete":
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.sparkcine.test/asset.png"})
		default:
			http.NotFound(w, r)
		}
	}))

	asset, err := uploadSparkCineAsset(context.Background(), generation.Config{
		BaseURL: server.URL,
		APIKey:  "sc_test",
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("image-bytes"))
	if err != nil {
		t.Fatalf("平铺签名上传失败: %v", err)
	}
	if putCalls != 1 || asset.URL != "https://cdn.sparkcine.test/asset.png" {
		t.Fatalf("平铺签名上传结果错误: putCalls=%d asset=%#v", putCalls, asset)
	}
}

func TestSparkCineFailureRedactsSignedURLFromTaskError(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var server *httptest.Server
	server = newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id": "asset-fail-1",
				"upload":   map[string]any{"url": server.URL + "/signed/fail?signature=do-not-leak"},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/signed/fail":
			http.Error(w, "storage rejected "+server.URL+"/private?signature=do-not-leak", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}

	_, err = adapter.UploadForGeneration(context.Background(), generation.Config{
		BaseURL:       server.URL,
		APIKey:        "sc_test",
		InterfaceType: "sparkcine-h3-video",
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("image-bytes"))
	if err == nil {
		t.Fatal("官方上传失败且未配置后备图床时应返回错误")
	}
	if strings.Contains(err.Error(), "do-not-leak") || strings.Contains(err.Error(), "http://") {
		t.Fatalf("任务错误泄漏签名 URL: %v", err)
	}
}

func TestSparkCineOfficialFailureFallsBackToConfiguredHost(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	official := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "official unavailable", http.StatusBadGateway)
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
		if string(body) != "audio-bytes" {
			t.Errorf("备用图床收到的音频不一致: %q", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://fallback.test/audio.mp3"})
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
		APIKey:        "sc_test",
		InterfaceType: "sparkcine-h3-video",
	}, mediahost.MediaKindAudio, "voice.mp3", "audio/mpeg", []byte("audio-bytes"))
	if err != nil {
		t.Fatalf("官方失败后备用图床也失败: %v", err)
	}
	if link != "https://fallback.test/audio.mp3" {
		t.Fatalf("备用图床链接 = %q", link)
	}
}

func TestSparkCineHostingPolicyDoesNotRequireFallbackEnabled(t *testing.T) {
	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	decision, err := adapter.HostingPolicy(context.Background(), "", "sparkcine-h3-video")
	if err != nil {
		t.Fatalf("读取 SparkCine 策略失败: %v", err)
	}
	if !decision.Host || decision.Reason != "protocol_requires_url_upstream_upload" {
		t.Fatalf("SparkCine 应优先使用官方素材上传: %#v", decision)
	}
}

func TestOpenVideoHostingPolicyLoadsOfficialContractForLocalMedia(t *testing.T) {
	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	if err := adapter.store.Write(mediahost.Config{
		Provider:        mediahost.ProviderCustom,
		Endpoint:        "https://media-host.example/upload",
		CustomFileField: "file",
	}); err != nil {
		t.Fatalf("写入测试图床配置失败: %v", err)
	}

	// The local reference bytes never leave this test. The assertion is on the
	// hosting decision that prevents the UI from falling back to manual links.
	decision, err := adapter.HostingPolicy(context.Background(), "openvideo-channel", "openvideo-video")
	if err != nil {
		t.Fatalf("读取 OpenVideo 图床策略失败: %v", err)
	}
	if !decision.Host {
		t.Fatalf("OpenVideo 本地参考素材应自动托管，得到 %#v", decision)
	}
	if decision.Reason != "protocol_requires_url" {
		t.Fatalf("OpenVideo 应使用 requiresPublicMediaUrls 合同，得到 %#v", decision)
	}
}

// OpenVideo 的本地参考素材必须走自动图床，而不是退回「填写参考素材链接」弹窗。
// 该用例只读取已安装插件声明，不访问真实上游。
func TestOpenVideoHostingPolicyDoesNotAskForManualReferenceLinks(t *testing.T) {
	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	generation.SetMediaHostingProvider(adapter)
	t.Cleanup(func() { generation.SetMediaHostingProvider(nil) })
	if err := adapter.store.Write(mediahost.Config{
		Provider:        mediahost.ProviderCustom,
		Endpoint:        "https://media-host.example/upload",
		CustomFileField: "file",
	}); err != nil {
		t.Fatalf("写入测试图床配置失败: %v", err)
	}

	input := generation.Input{
		Mode:   "video",
		Config: generation.Config{ChannelID: "openvideo-channel", InterfaceType: "openvideo-video"},
		ReferenceImages: []generation.Media{
			{StorageKey: "resource:local-image"},
		},
	}
	policy := generation.MediaHydrationPolicyForHostedGeneration(context.Background(), input)
	if !policy.RequireURL {
		t.Fatalf("OpenVideo 本地参考图应要求自动转换为公网 URL，得到 %#v", policy)
	}
	if !policy.CanHydrateLocal || policy.UploadConfig == nil {
		t.Fatalf("OpenVideo 本地参考图应具备本地上传能力，得到 %#v", policy)
	}
	if err := generation.ValidateMediaTransport(context.Background(), input); err != nil {
		t.Fatalf("自动托管可用时不应要求用户手动填写链接: %v", err)
	}
}

func TestSparkCineOfficialUploadSkipsFallbackWhenItSucceeds(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var server *httptest.Server
	var fallbackCalls int
	server = newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id": "asset-1",
				"upload":   map[string]any{"url": server.URL + "/put"},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/put":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads/asset-1/complete":
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.sparkcine.test/asset.png"})
		case r.Method == http.MethodPost && r.URL.Path == "/fallback":
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
		APIKey:        "sc_test",
		InterfaceType: "sparkcine-h3-video",
	}, mediahost.MediaKindImage, "shot.png", "image/png", []byte("image-bytes"))
	if err != nil {
		t.Fatalf("官方上传成功时不应失败: %v", err)
	}
	if link != "https://cdn.sparkcine.test/asset.png" {
		t.Fatalf("链接 = %q", link)
	}
	if fallbackCalls != 0 {
		t.Fatalf("官方上传成功后不应调用备用图床，调用了 %d 次", fallbackCalls)
	}
}

func TestSparkCineCacheReusesAssetIDWithoutSecondPut(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("no_proxy", "127.0.0.1,localhost")
	var server *httptest.Server
	var startCalls, putCalls, assetCalls int
	server = newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads":
			startCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id": "asset-cache-1",
				"upload":   map[string]any{"url": server.URL + "/put"},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/put":
			putCalls++
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/open/v1/uploads/asset-cache-1/complete":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/api/open/v1/assets/asset-cache-1":
			assetCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.sparkcine.test/cache.png"})
		default:
			http.NotFound(w, r)
		}
	}))
	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	config := generation.Config{BaseURL: server.URL, APIKey: "sc_test", InterfaceType: "sparkcine-h3-video"}
	body := []byte("cache-me")
	first, err := adapter.UploadForGeneration(context.Background(), config, mediahost.MediaKindImage, "shot.png", "image/png", body)
	if err != nil {
		t.Fatalf("首次官方上传失败: %v", err)
	}
	second, err := adapter.UploadForGeneration(context.Background(), config, mediahost.MediaKindImage, "shot.png", "image/png", body)
	if err != nil {
		t.Fatalf("缓存命中失败: %v", err)
	}
	if first != second || first != "https://cdn.sparkcine.test/cache.png" {
		t.Fatalf("缓存链接不一致: first=%q second=%q", first, second)
	}
	if startCalls != 1 || putCalls != 1 || assetCalls != 2 {
		t.Fatalf("缓存未生效: start=%d put=%d asset=%d", startCalls, putCalls, assetCalls)
	}
}

// OpenVideo 是视频协议，但它的参考素材通常是图片或音频，而免费图床不支持
// 视频上传。托管决策必须按素材真实类型判断，否则会被“协议是视频”误导，
// 连图片也不托管，用户就会看到要求手填 HTTPS 链接的弹窗。
func TestOpenVideoHostingPolicyAllowsImageOnlyFreeHost(t *testing.T) {
	adapter, err := newMediaHostingAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("创建图床适配器失败: %v", err)
	}
	if err := adapter.store.Write(mediahost.Config{
		Provider:     mediahost.ProviderFree,
		FreeProvider: mediahost.FreeProviderUguu,
	}); err != nil {
		t.Fatalf("写入免费图床配置失败: %v", err)
	}

	decision, err := adapter.HostingPolicy(context.Background(), "openvideo-channel", "openvideo-video")
	if err != nil {
		t.Fatalf("读取 OpenVideo 图床策略失败: %v", err)
	}
	if !decision.Host {
		t.Fatalf("OpenVideo 参考素材应走图床托管，得到 %#v", decision)
	}
}

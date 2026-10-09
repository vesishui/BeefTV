package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 这两个协议以官方插件包形式随应用分发。以下用例从磁盘上的 plugin-packages
// 目录装载真实 registry，验证「渠道模型选择协议 ID」→「后端路由到声明式适配器」
// →「上游收到正确请求形状」→「成片下载」整条链路，而不只是清单本身自洽。
func installedDeclarativeVideoAdapter(t *testing.T, interfaceType string) (context.Context, providerConfig) {
	t.Helper()
	center, err := newPluginRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !center.registrySnapshot().IsCapability(interfaceType, "video") {
		t.Fatalf("官方插件包未提供视频协议 %q；请先运行 plugin-packages/build-packages.sh", interfaceType)
	}
	ctx := withProtocolRegistry(context.Background(), center.registrySnapshot())
	return ctx, providerConfig{APIKey: "synthetic-test-key", Model: "lec-xg-sd-2-5", APIFormat: "openai", InterfaceType: interfaceType}
}

func TestPaipuVideoProtocolRoutesInstalledPackageAsJSON(t *testing.T) {
	allowLoopbackProviderTest(t)
	ctx, config := installedDeclarativeVideoAdapter(t, "paipu-video")

	var createContentType string
	var createBody map[string]any
	createCalls := 0
	pollCalls := 0
	downloadCalls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos":
			createCalls++
			createContentType = r.Header.Get("Content-Type")
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &createBody); err != nil {
				// multipart 请求体无法解析为 JSON，正是需要回归的那个故障。
				t.Errorf("派普创建请求不是 JSON：content-type=%q body=%q", createContentType, truncateForTest(raw))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"video_task_example","status":"queued","model":"lec-xg-sd-2-5"}`))
		case "/v1/videos/video_task_example":
			pollCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"video_task_example","status":"succeeded","model":"lec-xg-sd-2-5"}`))
		case "/v1/videos/video_task_example/content":
			downloadCalls++
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config.BaseURL = server.URL

	adapter, ok := declarativeProtocolAdapterForContext(ctx, config.InterfaceType)
	if !ok {
		t.Fatal("paipu-video 声明式适配器不可用")
	}
	result, err := runProtocolAdapterTaskWithPolicy(ctx, canvasGenerationInput{
		Mode:            "video",
		Prompt:          "固定机位，晴朗湖面轻轻起伏。",
		Config:          config,
		ReferenceImages: []providerMedia{{URL: "https://cdn.example.com/reference-1.jpg"}},
	}, adapter, fastVideoPollPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result["mode"] != "video" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.HasPrefix(strings.ToLower(createContentType), "application/json") {
		t.Fatalf("上游收到的 Content-Type = %q，派普只接受 JSON", createContentType)
	}
	if createBody["model"] != "lec-xg-sd-2-5" || createBody["prompt"] != "固定机位，晴朗湖面轻轻起伏。" {
		t.Fatalf("创建请求体 = %#v", createBody)
	}
	if createBody["aspect_ratio"] != "16:9" {
		t.Fatalf("默认画幅 = %#v，want 16:9", createBody["aspect_ratio"])
	}
	images, ok := createBody["images"].([]any)
	if !ok || len(images) != 1 || images[0] != "https://cdn.example.com/reference-1.jpg" {
		t.Fatalf("images = %#v", createBody["images"])
	}
	for _, forbidden := range []string{"seconds", "size", "resolution_name", "input_reference"} {
		if _, exists := createBody[forbidden]; exists {
			t.Fatalf("请求体混入了 Sora multipart 字段 %q：%#v", forbidden, createBody)
		}
	}
	if createCalls != 1 || pollCalls != 1 || downloadCalls != 1 {
		t.Fatalf("calls = create:%d poll:%d download:%d, want 1/1/1", createCalls, pollCalls, downloadCalls)
	}
}

func TestShanhaiVideoProtocolRoutesInstalledPackageAsGenerationsRequest(t *testing.T) {
	allowLoopbackProviderTest(t)
	ctx, config := installedDeclarativeVideoAdapter(t, "shanhai-video")
	config.Model = "oc-model-qbdmeb"

	var createBody map[string]any
	var createContentType string
	downloadCalls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/generations":
			createContentType = r.Header.Get("Content-Type")
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &createBody); err != nil {
				t.Errorf("山海创建请求不是 JSON：content-type=%q body=%q", createContentType, truncateForTest(raw))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"run_abc","object":"generation","status":"queued"}`))
		case "/api/v1/tasks/run_abc":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"run_abc","object":"generation","status":"succeeded","output":{"url":"` + server.URL + `/api/v1/media/runs/run_abc","mime_type":"video/mp4","type":"video"}}`))
		case "/api/v1/media/runs/run_abc":
			downloadCalls++
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config.BaseURL = server.URL

	adapter, ok := declarativeProtocolAdapterForContext(ctx, config.InterfaceType)
	if !ok {
		t.Fatal("shanhai-video 声明式适配器不可用")
	}
	result, err := runProtocolAdapterTaskWithPolicy(ctx, canvasGenerationInput{
		Mode:            "video",
		Prompt:          "A cinematic mountain sunrise",
		Config:          config,
		ReferenceImages: []providerMedia{{URL: "https://cdn.example.com/reference-1.jpg"}},
	}, adapter, fastVideoPollPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result["mode"] != "video" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.HasPrefix(strings.ToLower(createContentType), "application/json") {
		t.Fatalf("上游收到的 Content-Type = %q", createContentType)
	}
	if createBody["model"] != "oc-model-qbdmeb" || createBody["media_type"] != "video" {
		t.Fatalf("创建请求体 = %#v", createBody)
	}
	inputs, ok := createBody["inputs"].([]any)
	if !ok || len(inputs) != 1 {
		t.Fatalf("inputs = %#v", createBody["inputs"])
	}
	first, ok := inputs[0].(map[string]any)
	if !ok || first["type"] != "image" || first["url"] != "https://cdn.example.com/reference-1.jpg" {
		t.Fatalf("inputs[0] = %#v", inputs[0])
	}
	if downloadCalls != 1 {
		t.Fatalf("成片下载次数 = %d, want 1", downloadCalls)
	}
}

func truncateForTest(raw []byte) string {
	const limit = 200
	if len(raw) > limit {
		return string(raw[:limit]) + "..."
	}
	return string(raw)
}

// 「怎么选」的验收点：协议必须出现在自定义渠道的协议目录里，否则界面上选不到。
func TestInstalledVideoProtocolsAreSelectableForCustomChannels(t *testing.T) {
	center, err := newPluginRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{pluginRuntime: center}
	wanted := map[string]string{
		"paipu-video":   "POST /v1/videos",
		"shanhai-video": "POST /api/v1/generations",
	}
	for _, item := range service.PluginProviderCatalog("user.custom-channel", "video", false) {
		path, ok := wanted[item.ID]
		if !ok {
			continue
		}
		if item.Create != path {
			t.Errorf("%s create = %q, want %q", item.ID, item.Create, path)
		}
		if item.Execution != "declarative" {
			t.Errorf("%s execution = %q, want declarative", item.ID, item.Execution)
		}
		if !item.RequiresPublicMediaURLs {
			t.Errorf("%s 必须声明 requiresPublicMediaUrls", item.ID)
		}
		if item.ContentType != "application/json" {
			t.Errorf("%s contentType = %q, want application/json", item.ID, item.ContentType)
		}
		delete(wanted, item.ID)
	}
	for id := range wanted {
		t.Errorf("自定义渠道的协议目录里没有 %s", id)
	}

	// 山海还提供图片与文本协议，供同一个渠道复用。
	for _, capability := range []string{"image", "text"} {
		found := false
		for _, item := range service.PluginProviderCatalog("user.custom-channel", capability, false) {
			if strings.HasPrefix(item.ID, "shanhai-") {
				found = true
			}
		}
		if !found {
			t.Errorf("山海画布缺少 %s 协议", capability)
		}
	}
}

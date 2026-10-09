package app

import (
	"context"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/protocol"
)

// 派普网关（api.paipu.net）只接受 JSON 请求体。历史故障是把视频模型绑定到
// [OI] Videos / Sora 的 multipart 模板，上游返回 400 invalid_params
// （"xigua video only supports JSON requests"）。本文件锁定正确的请求形状，
// 防止再次回退到 multipart。
func paipuVideoAdapter(t *testing.T) protocol.Adapter {
	t.Helper()
	return officialSourceProviderAdapter(t, "paipu-video", "paipu-video")
}

func paipuVideoRequest() protocol.GenerationRequest {
	return protocol.GenerationRequest{
		Capability:  protocol.CapabilityVideo,
		Model:       "lec-xg-sd-2-5",
		Prompt:      "固定机位，晴朗湖面轻轻起伏。",
		Duration:    4,
		AspectRatio: "9:16",
		Resolution:  "720p",
		Images: []protocol.MediaReference{
			{URL: "https://cdn.example.com/reference-1.jpg", Kind: "image", Order: 1},
			{URL: "https://cdn.example.com/reference-2.jpg", Kind: "image", Order: 2},
		},
		Audios: []protocol.MediaReference{
			{URL: "https://cdn.example.com/reference.mp3", Kind: "audio", Order: 1},
		},
	}
}

func TestPaipuVideoPluginSendsJSONBodyToVideosEndpoint(t *testing.T) {
	adapter := paipuVideoAdapter(t)
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{Request: paipuVideoRequest()})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Method != "POST" {
		t.Fatalf("method = %q, want POST", spec.Method)
	}
	if spec.Path != "/v1/videos" {
		t.Fatalf("path = %q, want /v1/videos", spec.Path)
	}
	if got := strings.ToLower(strings.TrimSpace(strings.Split(spec.ContentType, ";")[0])); got != "application/json" {
		t.Fatalf("contentType = %q, want application/json", spec.ContentType)
	}
	if len(spec.Files) != 0 {
		t.Fatalf("派普不接受 multipart 文件部分，得到 %d 个文件字段", len(spec.Files))
	}

	body := marshalProtocolBody(t, spec.Body)
	if body["model"] != "lec-xg-sd-2-5" {
		t.Fatalf("model = %#v", body["model"])
	}
	if body["prompt"] != "固定机位，晴朗湖面轻轻起伏。" {
		t.Fatalf("prompt = %#v", body["prompt"])
	}
	if body["aspect_ratio"] != "9:16" {
		t.Fatalf("aspect_ratio = %#v, want 9:16", body["aspect_ratio"])
	}
	if body["resolution"] != "720p" {
		t.Fatalf("resolution = %#v, want 720p", body["resolution"])
	}
	// duration 必须是整数秒，不能退化成字符串。
	if duration, ok := body["duration"].(float64); !ok || duration != 4 {
		t.Fatalf("duration = %#v (%T), want integer 4", body["duration"], body["duration"])
	}
	images, ok := body["images"].([]any)
	if !ok || len(images) != 2 {
		t.Fatalf("images = %#v, want 2 个公网地址", body["images"])
	}
	if images[0] != "https://cdn.example.com/reference-1.jpg" || images[1] != "https://cdn.example.com/reference-2.jpg" {
		t.Fatalf("images 顺序错误：%#v", images)
	}
	audios, ok := body["audios"].([]any)
	if !ok || len(audios) != 1 || audios[0] != "https://cdn.example.com/reference.mp3" {
		t.Fatalf("audios = %#v", body["audios"])
	}
	// 空素材数组必须整体省略，不能发空数组或 null。
	if _, exists := body["videos"]; exists {
		t.Fatalf("空 videos 必须省略：%#v", body["videos"])
	}
}

// 回归守卫：multipart 模板的字段名一旦出现，说明视频模型又被绑定到了
// [OI] Videos / Sora 协议。
func TestPaipuVideoPluginOmitsSoraMultipartFields(t *testing.T) {
	adapter := paipuVideoAdapter(t)
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{Request: paipuVideoRequest()})
	if err != nil {
		t.Fatal(err)
	}
	body := marshalProtocolBody(t, spec.Body)
	for _, forbidden := range []string{"seconds", "size", "resolution_name", "input_reference", "input_reference[]"} {
		if _, exists := body[forbidden]; exists {
			t.Fatalf("派普 JSON 请求体不应包含 %q：%#v", forbidden, body)
		}
	}
}

func TestPaipuVideoPluginAppliesDocumentedDefaults(t *testing.T) {
	adapter := paipuVideoAdapter(t)
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{
		Request: protocol.GenerationRequest{
			Capability: protocol.CapabilityVideo,
			Model:      "lec-xg-sd-2-5",
			Prompt:     "a cinematic mountain sunrise",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := marshalProtocolBody(t, spec.Body)
	if duration, ok := body["duration"].(float64); !ok || duration != 4 {
		t.Fatalf("未指定时长时 duration = %#v, want 4", body["duration"])
	}
	if body["aspect_ratio"] != "16:9" {
		t.Fatalf("未指定画幅时 aspect_ratio = %#v, want 16:9", body["aspect_ratio"])
	}
	if body["resolution"] != "720p" {
		t.Fatalf("未指定分辨率时 resolution = %#v, want 720p", body["resolution"])
	}
	for _, key := range []string{"images", "videos", "audios"} {
		if _, exists := body[key]; exists {
			t.Fatalf("无参考素材时 %q 必须省略：%#v", key, body[key])
		}
	}
}

// 派普的扩展字段（例如 face）通过 providerOptions.paipu-video.body 合并。
func TestPaipuVideoPluginMergesProviderOptionsBody(t *testing.T) {
	adapter := paipuVideoAdapter(t)
	request := paipuVideoRequest()
	request.ProviderOptions = map[string]map[string]any{
		"paipu-video": {"body": map[string]any{"face": true}},
	}
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body := marshalProtocolBody(t, spec.Body)
	if body["face"] != true {
		t.Fatalf("providerOptions 未合并进请求体：%#v", body)
	}
	if body["model"] != "lec-xg-sd-2-5" {
		t.Fatalf("合并扩展字段后丢失 model：%#v", body)
	}
}

// 轮询响应只返回 id/status；成片必须走同源的 /content 结果端点。
func TestPaipuVideoPluginPollsAndDownloadsFromContentEndpoint(t *testing.T) {
	adapter := paipuVideoAdapter(t)

	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"video_task_example","status":"queued","model":"lec-xg-sd-2-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "video_task_example" {
		t.Fatalf("taskId = %q", created.TaskID)
	}
	if created.Status != protocol.StatusPending {
		t.Fatalf("queued 应归一为 pending，得到 %q", created.Status)
	}

	pollSpec, err := adapter.BuildPoll(context.Background(), protocol.PollContext{TaskID: created.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if pollSpec.Path != "/v1/videos/video_task_example" {
		t.Fatalf("poll path = %q", pollSpec.Path)
	}

	// 查询返回 succeeded 但没有媒体地址：结果必须由 result 端点下载。
	polled, err := adapter.ParsePoll(context.Background(), protocol.PollContext{TaskID: created.TaskID},
		[]byte(`{"id":"video_task_example","status":"succeeded","model":"lec-xg-sd-2-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	if polled.Status != protocol.StatusSucceeded {
		t.Fatalf("succeeded 未识别，得到 %q", polled.Status)
	}
	if polled.Result != nil && len(polled.Result.Videos) > 0 {
		t.Fatalf("派普轮询不应返回视频地址：%#v", polled.Result.Videos)
	}

	resultAdapter, ok := adapter.(protocol.ResultAdapter)
	if !ok {
		t.Fatal("paipu-video 必须实现 ResultAdapter")
	}
	capability, ok := adapter.(protocol.ResultCapability)
	if !ok || !capability.ResultAvailable() {
		t.Fatal("paipu-video 必须声明结果下载端点可用")
	}
	resultSpec, err := resultAdapter.BuildResult(context.Background(), protocol.PollContext{TaskID: created.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if resultSpec.Path != "/v1/videos/video_task_example/content" {
		t.Fatalf("result path = %q", resultSpec.Path)
	}
}

func TestPaipuVideoPluginReportsUpstreamFailure(t *testing.T) {
	adapter := paipuVideoAdapter(t)
	polled, err := adapter.ParsePoll(context.Background(), protocol.PollContext{TaskID: "video_task_example"},
		[]byte(`{"id":"video_task_example","status":"failed","error":{"code":"invalid_params","message":"xigua video only supports JSON requests"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if polled.Status != protocol.StatusFailed {
		t.Fatalf("failed 未识别，得到 %q", polled.Status)
	}
	if !strings.Contains(polled.Message, "JSON") {
		t.Fatalf("上游错误信息未透传：%q", polled.Message)
	}
}

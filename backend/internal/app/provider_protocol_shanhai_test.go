package app

import (
	"context"
	"testing"

	"infinite-canvas/backend/internal/protocol"
)

// 山海画布（shanhai.vnshu.cn）不是 [OI] 兼容形状：请求体用 media_type + inputs +
// options，任务查询走 GET /tasks/{id}，成片地址由 output.url 返回。本文件锁定
// 这套形状，避免被改写成 /v1/videos 之类的兼容模板。
func shanhaiAdapter(t *testing.T, providerID string) protocol.Adapter {
	t.Helper()
	return officialSourceProviderAdapter(t, "shanhai-canvas", providerID)
}

func TestShanhaiVideoPluginBuildsGenerationsBody(t *testing.T) {
	adapter := shanhaiAdapter(t, "shanhai-video")
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{
		Request: protocol.GenerationRequest{
			Capability:  protocol.CapabilityVideo,
			Model:       "oc-model-qbdmeb",
			Prompt:      "A cinematic mountain sunrise",
			Duration:    15,
			AspectRatio: "16:9",
			Resolution:  "720p",
			Images: []protocol.MediaReference{
				{URL: "https://cdn.example.com/reference-1.jpg", Kind: "image", Order: 1},
				{URL: "https://cdn.example.com/reference-2.jpg", Kind: "image", Order: 2},
			},
			Audios: []protocol.MediaReference{
				{URL: "https://cdn.example.com/reference.mp3", Kind: "audio", Order: 1},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Method != "POST" || spec.Path != "/api/v1/generations" {
		t.Fatalf("%s %s, want POST /api/v1/generations", spec.Method, spec.Path)
	}
	if spec.ContentType != "application/json" {
		t.Fatalf("contentType = %q", spec.ContentType)
	}
	if len(spec.Files) != 0 {
		t.Fatalf("山海不接受 multipart 文件部分，得到 %d 个文件字段", len(spec.Files))
	}

	body := marshalProtocolBody(t, spec.Body)
	if body["model"] != "oc-model-qbdmeb" || body["prompt"] != "A cinematic mountain sunrise" {
		t.Fatalf("model/prompt = %#v / %#v", body["model"], body["prompt"])
	}
	if body["media_type"] != "video" {
		t.Fatalf("media_type = %#v, want video", body["media_type"])
	}

	inputs, ok := body["inputs"].([]any)
	if !ok || len(inputs) != 3 {
		t.Fatalf("inputs = %#v, want 3 项（2 图 + 1 音频）", body["inputs"])
	}
	first, ok := inputs[0].(map[string]any)
	if !ok || first["type"] != "image" || first["url"] != "https://cdn.example.com/reference-1.jpg" {
		t.Fatalf("inputs[0] = %#v", inputs[0])
	}
	if third, ok := inputs[2].(map[string]any); !ok || third["type"] != "audio" {
		t.Fatalf("inputs[2] = %#v", inputs[2])
	}

	options, ok := body["options"].(map[string]any)
	if !ok {
		t.Fatalf("options = %#v", body["options"])
	}
	if options["aspect_ratio"] != "16:9" || options["resolution"] != "720p" {
		t.Fatalf("options = %#v", options)
	}
	// 山海文档的 options.duration 是字符串，不是整数。
	if options["duration"] != "15" {
		t.Fatalf("options.duration = %#v (%T), want \"15\"", options["duration"], options["duration"])
	}
	// 旧版单图字段 input 不能与 inputs 同时出现。
	if _, exists := body["input"]; exists {
		t.Fatalf("不能同时传 input 与 inputs：%#v", body["input"])
	}
}

func TestShanhaiVideoPluginOmitsEmptyInputsAndOptions(t *testing.T) {
	adapter := shanhaiAdapter(t, "shanhai-video")
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{
		Request: protocol.GenerationRequest{
			Capability: protocol.CapabilityVideo,
			Model:      "oc-model-qbdmeb",
			Prompt:     "A cinematic mountain sunrise",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := marshalProtocolBody(t, spec.Body)
	if _, exists := body["inputs"]; exists {
		t.Fatalf("无参考素材时 inputs 必须整体省略：%#v", body["inputs"])
	}
	if _, exists := body["options"]; exists {
		t.Fatalf("无生成参数时 options 必须整体省略：%#v", body["options"])
	}
}

func TestShanhaiImagePluginUsesImageMediaTypeAndKeepsKebabResolution(t *testing.T) {
	adapter := shanhaiAdapter(t, "shanhai-image")
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{
		Request: protocol.GenerationRequest{
			Capability:  protocol.CapabilityImage,
			Model:       "shanhai-nano-banana-pro",
			Prompt:      "A moonlit mountain",
			AspectRatio: "16:9",
			Resolution:  "2K",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := marshalProtocolBody(t, spec.Body)
	if body["media_type"] != "image" {
		t.Fatalf("media_type = %#v, want image", body["media_type"])
	}
	options, ok := body["options"].(map[string]any)
	if !ok || options["resolution"] != "2K" {
		t.Fatalf("options = %#v", body["options"])
	}
	// 图片没有时长概念，不能凭空补一个 duration。
	if _, exists := options["duration"]; exists {
		t.Fatalf("图片请求不应包含 duration：%#v", options)
	}
}

func TestShanhaiTextPluginUsesTextMediaTypeAndReadsOutputText(t *testing.T) {
	adapter := shanhaiAdapter(t, "shanhai-text")
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{
		Request: protocol.GenerationRequest{
			Capability: protocol.CapabilityText,
			Model:      "shanhai-deepseek-v4-pro",
			Prompt:     "Summarize this paragraph",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := marshalProtocolBody(t, spec.Body)
	if body["media_type"] != "text" {
		t.Fatalf("media_type = %#v, want text", body["media_type"])
	}
	if _, exists := body["options"]; exists {
		t.Fatalf("文本请求不应包含 options：%#v", body["options"])
	}

	// 文本任务通常创建时就返回 succeeded 与 output.text。
	created, err := adapter.ParseCreate(context.Background(),
		[]byte(`{"id":"run_text","object":"generation","status":"succeeded","output":{"text":"一段摘要"},"usage":{"credits":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != protocol.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", created.Status)
	}
	if created.Result == nil || created.Result.Text != "一段摘要" {
		t.Fatalf("output.text 未映射：%#v", created.Result)
	}
}

func TestShanhaiVideoPluginPollsTasksAndMapsOutputURL(t *testing.T) {
	adapter := shanhaiAdapter(t, "shanhai-video")

	created, err := adapter.ParseCreate(context.Background(),
		[]byte(`{"id":"run_abc","object":"generation","status":"queued","model":"oc-model-qbdmeb","output":null,"usage":{"credits":12}}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "run_abc" {
		t.Fatalf("taskId = %q", created.TaskID)
	}
	if created.Status != protocol.StatusPending {
		t.Fatalf("queued 应归一为 pending，得到 %q", created.Status)
	}

	pollSpec, err := adapter.BuildPoll(context.Background(), protocol.PollContext{TaskID: created.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if pollSpec.Path != "/api/v1/tasks/run_abc" {
		t.Fatalf("poll path = %q", pollSpec.Path)
	}

	polled, err := adapter.ParsePoll(context.Background(), protocol.PollContext{TaskID: created.TaskID},
		[]byte(`{"id":"run_abc","object":"generation","status":"succeeded","output":{"url":"https://shanhai.vnshu.cn/api/v1/media/runs/run_abc","mime_type":"video/mp4","type":"video"},"usage":{"credits":12}}`))
	if err != nil {
		t.Fatal(err)
	}
	if polled.Status != protocol.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", polled.Status)
	}
	if polled.Result == nil || len(polled.Result.Videos) != 1 {
		t.Fatalf("output.url 未映射为视频结果：%#v", polled.Result)
	}
	if polled.Result.Videos[0].URL != "https://shanhai.vnshu.cn/api/v1/media/runs/run_abc" {
		t.Fatalf("video url = %q", polled.Result.Videos[0].URL)
	}
	// output.url 需要同一枚 Bearer Key 才能下载。
	if !polled.Result.Videos[0].Ephemeral {
		t.Fatal("output.url 是受鉴权地址，必须标记为 ephemeral")
	}

	failed, err := adapter.ParsePoll(context.Background(), protocol.PollContext{TaskID: created.TaskID},
		[]byte(`{"id":"run_abc","status":"failed","error":{"code":"invalid_params","message":"参数不合法"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != protocol.StatusFailed {
		t.Fatalf("failed 未识别，得到 %q", failed.Status)
	}
	if failed.Message != "参数不合法" {
		t.Fatalf("错误信息未透传：%q", failed.Message)
	}
}

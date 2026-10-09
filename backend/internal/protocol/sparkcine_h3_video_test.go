package protocol

import (
	"context"
	"strings"
	"testing"
)

// SparkCine H3 上游要求每个 text 项不超过 7000 字符，但允许多项按换行拼接。
// 长分镜提示词必须被切分成多个合规片段，且拼接后与原文一致。
func TestSparkCineH3SplitsLongPromptIntoLegalChunks(t *testing.T) {
	adapter := officialPackageAdapter(t, "sparkcine-h3-video.beeftv-plugin", "sparkcine-h3-video")

	prompt := strings.Repeat("飞龙掠过航母甲板。\n", 900)
	request := GenerationRequest{
		Capability:  CapabilityVideo,
		Model:       "h3-video",
		Prompt:      prompt,
		Duration:    15,
		AspectRatio: "16:9",
		Resolution:  "768P",
		Images: []MediaReference{
			{URL: "https://media.example/ref-1.webp", Order: 0},
			{URL: "https://media.example/ref-2.webp", Order: 1},
		},
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{BaseURL: "https://hi.hi.run", Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if create.Path != "/api/h3/v2/video_generation" {
		t.Fatalf("create path = %q", create.Path)
	}
	body := manifestTestBody(t, create)
	content, ok := body["content"].([]any)
	if !ok {
		t.Fatalf("content missing: %#v", body)
	}

	var textParts []string
	imageRoles := make([]any, 0, 2)
	for _, raw := range content {
		item, _ := raw.(map[string]any)
		switch item["type"] {
		case "text":
			text := manifestString(item["text"])
			if len([]rune(text)) > 7000 {
				t.Fatalf("text chunk exceeds upstream 7000 rune limit: %d", len([]rune(text)))
			}
			textParts = append(textParts, text)
		case "image_url":
			imageRoles = append(imageRoles, item["role"])
		}
	}
	if len(textParts) < 2 {
		t.Fatalf("long prompt was not split: %d chunk(s)", len(textParts))
	}
	// 切分保留原始换行，逐段直接拼接必须还原原文（上游按换行拼接语义等价）。
	if got := strings.Join(textParts, ""); got != prompt {
		t.Fatalf("split prompt does not reassemble to the original text")
	}
	for _, role := range imageRoles {
		if role != "reference_image" {
			t.Fatalf("plain h3-video image role = %#v, want reference_image", role)
		}
	}
	if got := body["duration"]; got != float64(15) {
		t.Fatalf("duration = %#v, want 15 (request.seconds alias must reach the plugin)", got)
	}
}

// 未指定时长时时长别名必须缺席，让插件回落到默认值，而不是把 0 发给上游。
func TestSparkCineH3OmitsDurationWhenUnset(t *testing.T) {
	adapter := officialPackageAdapter(t, "sparkcine-h3-video.beeftv-plugin", "sparkcine-h3-video")
	create, err := adapter.BuildCreate(context.Background(), RequestContext{BaseURL: "https://hi.hi.run", Request: GenerationRequest{
		Capability: CapabilityVideo,
		Model:      "h3-video",
		Prompt:     "short prompt",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := manifestTestBody(t, create)["duration"]; got != float64(6) {
		t.Fatalf("duration = %#v, want the plugin default 6", got)
	}
}

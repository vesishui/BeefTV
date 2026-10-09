package protocol

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenVideoVideoWireContract(t *testing.T) {
	adapter := officialPackageAdapter(t, "openvideo-video.beeftv-plugin", "openvideo-video")
	packageData, err := os.ReadFile(filepath.Join("..", "..", "..", "plugin-packages", "openvideo-video.beeftv-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := ParsePluginPackage(packageData)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.Manifest.Contributes.Providers) != 1 || plugin.Manifest.Contributes.Providers[0].BaseURL != "https://www.openvideo.top/api/v1" {
		t.Fatalf("OpenVideo 默认地址必须带 www 且证书匹配，得到 %#v", plugin.Manifest.Contributes.Providers)
	}
	request := GenerationRequest{
		Capability:  CapabilityVideo,
		Model:       "free-video-model",
		Prompt:      "a paper boat on a quiet river",
		Duration:    5,
		AspectRatio: "16:9",
		Resolution:  "720p",
		Images:      []MediaReference{{URL: "https://media.example/ref.webp", Order: 1}},
		ProviderOptions: map[string]map[string]any{
			"openvideo": {
				"style_id": "cinematic",
				"team_id":  "team-free",
			},
		},
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{BaseURL: "https://www.openvideo.top/api/v1", Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if create.Method != "POST" || create.Path != "/videos" || create.ContentType != "application/json" {
		t.Fatalf("create spec = %#v", create)
	}
	body := manifestTestBody(t, create)
	if body["object"] != "video" || body["model"] != request.Model || body["prompt"] != request.Prompt ||
		body["duration"] != float64(5) || body["ratio"] != request.AspectRatio ||
		body["resolution"] != request.Resolution || body["style_id"] != "cinematic" ||
		body["team_id"] != "team-free" {
		t.Fatalf("create body = %#v", body)
	}
	images, ok := body["images"].([]any)
	if !ok || len(images) != 1 || images[0] != request.Images[0].URL {
		t.Fatalf("images = %#v", body["images"])
	}

	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"ov-task-1","status":0,"state":"pending"}`))
	if err != nil || created.TaskID != "ov-task-1" || created.Status != StatusPending {
		t.Fatalf("create result = %#v, %v", created, err)
	}
	completed, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: created.TaskID}, []byte(`{"id":"ov-task-1","status":2,"state":"succeeded","result_url":"https://cdn.example/result.mp4","result_urls":["https://cdn.example/result.mp4"]}`))
	if err != nil || completed.Status != StatusSucceeded || completed.Result == nil || len(completed.Result.Videos) != 1 {
		t.Fatalf("poll result = %#v, %v", completed, err)
	}
	if completed.Result.Videos[0].URL != "https://cdn.example/result.mp4" || !completed.Result.Videos[0].Ephemeral {
		t.Fatalf("video result = %#v", completed.Result.Videos)
	}
}

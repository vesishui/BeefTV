package generation

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/mediahost"
)

type localUploadHostingProvider struct {
	kinds []mediahost.MediaKind
}

func (p *localUploadHostingProvider) HostingPolicy(context.Context, string, string) (mediahost.Decision, error) {
	return mediahost.Decision{Host: true, Reason: "protocol_requires_url_upstream_upload"}, nil
}

func (p *localUploadHostingProvider) UploadForGeneration(_ context.Context, config Config, kind mediahost.MediaKind, _ string, _ string, body []byte) (string, error) {
	p.kinds = append(p.kinds, kind)
	return "https://cdn.example/" + string(kind) + "/" + mediahost.HashContent(body), nil
}

type memoryMediaResources struct {
	media map[string]struct {
		info ResourceInfo
		data []byte
	}
}

func (r memoryMediaResources) Lookup(_ string, resourceID string) (ResourceInfo, error) {
	item, ok := r.media[resourceID]
	if !ok {
		return ResourceInfo{}, io.EOF
	}
	return item.info, nil
}

func (r memoryMediaResources) Open(_ string, resourceID string) (ResourceInfo, io.ReadCloser, error) {
	item, ok := r.media[resourceID]
	if !ok {
		return ResourceInfo{}, nil, io.EOF
	}
	return item.info, io.NopCloser(bytes.NewReader(item.data)), nil
}

func (memoryMediaResources) PublicURL(ResourceInfo, time.Time) (string, error) {
	return "", io.EOF
}

func (memoryMediaResources) HTTPSPublicURL(ResourceInfo, time.Time) (string, error) {
	return "", io.EOF
}

func (memoryMediaResources) LocalMode() bool { return true }

func TestSparkCineLocalImageAndAudioHydrateThroughHostingProvider(t *testing.T) {
	hosting := &localUploadHostingProvider{}
	SetMediaHostingProvider(hosting)
	defer SetMediaHostingProvider(nil)

	resources := memoryMediaResources{media: map[string]struct {
		info ResourceInfo
		data []byte
	}{
		"image-1": {
			info: ResourceInfo{ID: "image-1", UserID: "user-1", Status: "ready", Kind: "image", MimeType: "image/png", Provider: "local", Size: 5},
			data: []byte("image"),
		},
		"audio-1": {
			info: ResourceInfo{ID: "audio-1", UserID: "user-1", Status: "ready", Kind: "audio", MimeType: "audio/mpeg", Provider: "local", Size: 5},
			data: []byte("audio"),
		},
	}}
	input := Input{
		Mode:   "video",
		Prompt: "test",
		Config: Config{ChannelID: "sparkcine", InterfaceType: "sparkcine-h3-video", BaseURL: "https://hi.hi.run", APIKey: "test-key"},
		ReferenceImages: []Media{
			{ID: "image-node", Name: "frame.png", StorageKey: "resource:image-1"},
		},
		ReferenceAudios: []Media{
			{ID: "audio-node", Name: "voice.mp3", StorageKey: "resource:audio-1"},
		},
	}
	ctx := WithRuntime(context.Background(), Runtime{
		Resources: resources,
		Call:      CallMeta{UserID: "user-1"},
	})
	if err := ValidateMediaTransport(ctx, input); err != nil {
		t.Fatalf("本地素材应通过上传前置校验: %v", err)
	}
	policy := MediaHydrationPolicyForHostedGeneration(ctx, input)
	if !policy.RequireURL || !policy.CanHydrateLocal || policy.UploadConfig == nil {
		t.Fatalf("SparkCine hydration policy 缺少本地上传能力: %+v", policy)
	}
	if err := HydrateMedia(ctx, "user-1", &input, policy); err != nil {
		t.Fatalf("本地素材上传失败: %v", err)
	}

	if !strings.HasPrefix(input.ReferenceImages[0].URL, "https://cdn.example/image/") ||
		input.ReferenceImages[0].StorageKey != "" || input.ReferenceImages[0].DataURL != "" {
		t.Fatalf("图片未替换成公网 URL: %+v", input.ReferenceImages[0])
	}
	if !strings.HasPrefix(input.ReferenceAudios[0].URL, "https://cdn.example/audio/") ||
		input.ReferenceAudios[0].StorageKey != "" || input.ReferenceAudios[0].DataURL != "" {
		t.Fatalf("音频未替换成公网 URL: %+v", input.ReferenceAudios[0])
	}
	if len(hosting.kinds) != 2 || hosting.kinds[0] != mediahost.MediaKindImage || hosting.kinds[1] != mediahost.MediaKindAudio {
		t.Fatalf("上传素材类型识别错误: %v", hosting.kinds)
	}
}

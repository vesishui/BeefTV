package generation

import (
	"context"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/mediahost"
)

type stubHostingProvider struct {
	decision mediahost.Decision
	err      error
	// capturedChannel records what the hook forwarded, so the test can prove
	// the channel identifier reaches the provider.
	capturedChannel *string
}

func (s stubHostingProvider) HostingPolicy(_ context.Context, channelID string, _ string) (mediahost.Decision, error) {
	if s.capturedChannel != nil {
		*s.capturedChannel = channelID
	}
	return s.decision, s.err
}

func TestHostingHookIsInertWithoutProvider(t *testing.T) {
	SetMediaHostingProvider(nil)
	defer SetMediaHostingProvider(nil)

	input := Input{Config: Config{InterfaceType: "gemini-generate-content"}}
	got := MediaHydrationPolicyWithHosting(context.Background(), input)
	want := MediaHydrationPolicyFor(context.Background(), input)
	if got != want {
		t.Fatalf("未安装 provider 时必须与上游策略完全一致: got %+v want %+v", got, want)
	}
}

func TestHostingHookPromotesFlagsWhenDecisionHosts(t *testing.T) {
	SetMediaHostingProvider(stubHostingProvider{decision: mediahost.Decision{Host: true, Reason: "protocol_requires_url"}})
	defer SetMediaHostingProvider(nil)

	// A protocol the upstream policy does not already mark as URL-only.
	input := Input{Config: Config{InterfaceType: "openai-image"}}
	got := MediaHydrationPolicyWithHosting(context.Background(), input)
	if !got.RequireURL {
		t.Fatal("图床命中时必须把素材走 URL")
	}
	if !got.PreferURL {
		t.Fatal("PreferURL 也应为真")
	}
}

func TestHostingHookLeavesPolicyAloneWhenDecisionDeclines(t *testing.T) {
	SetMediaHostingProvider(stubHostingProvider{decision: mediahost.Decision{Host: false, Reason: "protocol_expects_inline"}})
	defer SetMediaHostingProvider(nil)

	input := Input{Config: Config{InterfaceType: "gemini-generate-content"}}
	got := MediaHydrationPolicyWithHosting(context.Background(), input)
	want := MediaHydrationPolicyFor(context.Background(), input)
	if got != want {
		t.Fatalf("拒绝托管时必须保持上游策略: got %+v want %+v", got, want)
	}
}

func TestHostingHookIgnoresProviderError(t *testing.T) {
	SetMediaHostingProvider(stubHostingProvider{err: errors.New("hosting lookup failed")})
	defer SetMediaHostingProvider(nil)

	input := Input{Config: Config{InterfaceType: "openai-image"}}
	got := MediaHydrationPolicyWithHosting(context.Background(), input)
	want := MediaHydrationPolicyFor(context.Background(), input)
	if got != want {
		t.Fatal("图床查询失败时必须回退到上游策略，不能阻断生成")
	}
}

func TestHostingHookKeepsMaskedImageEditsOnBytes(t *testing.T) {
	SetMediaHostingProvider(stubHostingProvider{decision: mediahost.Decision{Host: true, Reason: "protocol_requires_url"}})
	defer SetMediaHostingProvider(nil)

	mask := &Media{ID: "mask"}
	input := Input{Mode: "image", Mask: mask, Config: Config{InterfaceType: "openai-image"}}
	got := MediaHydrationPolicyWithHosting(context.Background(), input)
	if got.RequireURL || got.PreferURL {
		t.Fatalf("带遮罩的图片编辑必须保持字节传输: %+v", got)
	}
}

func TestHostingHookForwardsChannelID(t *testing.T) {
	var captured string
	SetMediaHostingProvider(stubHostingProvider{
		decision:        mediahost.Decision{Host: false, Reason: "hosting_disabled"},
		capturedChannel: &captured,
	})
	defer SetMediaHostingProvider(nil)

	input := Input{Config: Config{ChannelID: "channel-under-test", InterfaceType: "dashscope-wan-video"}}
	MediaHydrationPolicyWithHosting(context.Background(), input)
	if captured != "channel-under-test" {
		t.Fatalf("渠道路由信息必须传到图床决策，得到 %q", captured)
	}
}

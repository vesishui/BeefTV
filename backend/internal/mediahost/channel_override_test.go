package mediahost

import "testing"

func TestResolveForChannelWithoutOverrideFollowsGlobal(t *testing.T) {
	config := Normalize(Config{
		Provider: ProviderCustom,
		Endpoint: "https://global.example.com/upload",
	})
	got := config.ResolveForChannel("some-channel")
	if got.Provider != ProviderCustom || got.Endpoint != "https://global.example.com/upload" {
		t.Fatalf("无覆盖时必须跟随全局配置: %+v", got)
	}
}

func TestResolveForChannelInheritModeFollowsGlobal(t *testing.T) {
	config := Normalize(Config{
		Provider: ProviderCustom,
		Endpoint: "https://global.example.com/upload",
		ChannelOverrides: map[string]ChannelOverride{
			"chan-a": {Mode: "inherit"},
		},
	})
	got := config.ResolveForChannel("chan-a")
	if got.Provider != ProviderCustom {
		t.Fatalf("inherit 模式应跟随全局，得到 %q", got.Provider)
	}
}

func TestResolveForChannelOffDisablesHosting(t *testing.T) {
	config := Normalize(Config{
		Provider: ProviderCustom,
		Endpoint: "https://global.example.com/upload",
		ChannelOverrides: map[string]ChannelOverride{
			"chan-a": {Mode: "off"},
		},
	})
	if got := config.ResolveForChannel("chan-a"); got.Enabled() {
		t.Fatalf("off 模式必须关闭该渠道的图床，得到 provider=%q", got.Provider)
	}
	// 其他渠道不受影响。
	if got := config.ResolveForChannel("chan-b"); !got.Enabled() {
		t.Fatal("off 覆盖不应影响其他渠道")
	}
}

func TestResolveForChannelCustomOverridesEndpoint(t *testing.T) {
	config := Normalize(Config{
		Provider: ProviderCustom,
		Endpoint: "https://global.example.com/upload",
		CustomURLPath: "data.url",
		ChannelOverrides: map[string]ChannelOverride{
			"chan-a": {Mode: "custom", Endpoint: "https://chan-a.example.com/upload"},
		},
	})
	got := config.ResolveForChannel("chan-a")
	if got.Endpoint != "https://chan-a.example.com/upload" {
		t.Fatalf("渠道应使用自己的上传地址，得到 %q", got.Endpoint)
	}
	// 未覆盖的字段继续沿用全局配置。
	if got.CustomURLPath != "data.url" {
		t.Fatalf("未覆盖字段应继承全局，得到 %q", got.CustomURLPath)
	}
}

func TestResolveForChannelCustomProviderSwitchClearsForeignCredential(t *testing.T) {
	config := Normalize(Config{
		Provider:   ProviderCustom,
		Endpoint:   "https://global.example.com/upload",
		CustomToken: "global-token",
		ChannelOverrides: map[string]ChannelOverride{
			"chan-a": {Mode: "custom", Provider: string(ProviderAWSS3)},
		},
	})
	got := config.ResolveForChannel("chan-a")
	if got.Provider != ProviderAWSS3 {
		t.Fatalf("渠道应切换到 S3，得到 %q", got.Provider)
	}
	if got.CustomToken != "" {
		t.Fatal("切换 provider 后不能复用属于其他服务的凭据")
	}
}

func TestResolveForChannelUnknownModeFallsBackToGlobal(t *testing.T) {
	config := Normalize(Config{
		Provider: ProviderCustom,
		Endpoint: "https://global.example.com/upload",
		ChannelOverrides: map[string]ChannelOverride{
			"chan-a": {Mode: "nonsense"},
		},
	})
	got := config.ResolveForChannel("chan-a")
	if got.Provider != ProviderCustom || got.Endpoint != "https://global.example.com/upload" {
		t.Fatalf("未知模式应回退到全局配置: %+v", got)
	}
}

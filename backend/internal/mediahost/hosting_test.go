package mediahost

import (
	"strings"
	"testing"
)

func TestDefaultConfigIsDisabled(t *testing.T) {
	config := DefaultConfig()
	if config.Enabled() {
		t.Fatal("默认配置必须关闭图床，升级不能改变已有渠道的发送方式")
	}
	if config.Provider != ProviderNone {
		t.Fatalf("默认 provider = %q, want %q", config.Provider, ProviderNone)
	}
}

func TestNormalizeRejectsUnknownProvider(t *testing.T) {
	config := Normalize(Config{Provider: Provider("dropbox")})
	if config.Provider != ProviderNone {
		t.Fatalf("未知 provider 应回退到 none，得到 %q", config.Provider)
	}
}

func TestNormalizeFillsCustomDefaults(t *testing.T) {
	config := Normalize(Config{Provider: ProviderCustom, Endpoint: "  https://example.com/upload  "})
	if config.Endpoint != "https://example.com/upload" {
		t.Fatalf("endpoint 应去除空白，得到 %q", config.Endpoint)
	}
	if config.CustomFileField != "file" {
		t.Fatalf("默认文件字段 = %q, want file", config.CustomFileField)
	}
	if config.CustomAuthHeader != "Authorization" {
		t.Fatalf("默认鉴权头 = %q, want Authorization", config.CustomAuthHeader)
	}
}

func TestSupportsKindCoversAudioForStorageProviders(t *testing.T) {
	for _, provider := range []Provider{ProviderBuiltin, ProviderCustom, ProviderAliyunOSS, ProviderTencentCOS, ProviderCloudflareR2, ProviderAWSS3} {
		config := Normalize(Config{Provider: provider})
		if !config.SupportsKind(MediaKindAudio) {
			t.Fatalf("provider %q 必须支持音频，这是与小七图片图床的关键差异", provider)
		}
		if !config.SupportsKind(MediaKindImage) || !config.SupportsKind(MediaKindVideo) {
			t.Fatalf("provider %q 必须同时支持图片和视频", provider)
		}
	}
}

func TestFreeProviderCarriesImagesAndAudio(t *testing.T) {
	config := Normalize(Config{Provider: ProviderFree})
	if !config.SupportsKind(MediaKindImage) {
		t.Fatal("免费图床应支持图片")
	}
	// 实测 uguu 接受音频（litterbox 拒绝），所以这里允许音频，由上传路径
	// 报告具体 host 的拒绝原因。
	if !config.SupportsKind(MediaKindAudio) {
		t.Fatal("免费图床应允许音频，实际是否成功取决于具体 host")
	}
	if config.SupportsKind(MediaKindVideo) {
		t.Fatal("没有免费图床接受视频，必须报告不支持")
	}
}

func TestDisabledProviderSupportsNothing(t *testing.T) {
	config := DefaultConfig()
	for _, kind := range []MediaKind{MediaKindImage, MediaKindVideo, MediaKindAudio} {
		if config.SupportsKind(kind) {
			t.Fatalf("关闭状态不应支持 %q", kind)
		}
	}
}

func TestValidateReportsMissingFields(t *testing.T) {
	cases := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{"none is always valid", Config{Provider: ProviderNone}, ""},
		{"custom needs endpoint", Config{Provider: ProviderCustom}, "上传地址"},
		{"imgbb needs key", Config{Provider: ProviderFree, FreeProvider: FreeProviderImgBB}, "API Key"},
		{"storage needs bucket", Config{Provider: ProviderAWSS3}, "存储桶"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := Validate(testCase.config)
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("期望通过，得到 %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望错误包含 %q，但通过了校验", testCase.wantErr)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("错误 %q 未包含 %q", err.Error(), testCase.wantErr)
			}
		})
	}
}

func TestExtractUploadedURLAcceptsCommonShapes(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		path    string
		want    string
	}{
		{"plain text url", "https://cdn.example.com/a.png", "", "https://cdn.example.com/a.png"},
		{"imgbb shape", `{"data":{"url":"https://i.ibb.co/x/y.png"}}`, "", "https://i.ibb.co/x/y.png"},
		{"explicit path wins", `{"result":{"link":"https://a.example.com/one.png"},"url":"https://b.example.com/two.png"}`, "result.link", "https://a.example.com/one.png"},
		{"array index", `{"files":[{"url":"https://c.example.com/three.png"}]}`, "", "https://c.example.com/three.png"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := extractUploadedURL([]byte(testCase.payload), testCase.path)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("得到 %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestExtractUploadedURLRejectsUnusablePayload(t *testing.T) {
	if _, err := extractUploadedURL([]byte(`{"ok":true}`), ""); err == nil {
		t.Fatal("没有链接的响应必须报错，不能返回空 URL")
	}
	if _, err := extractUploadedURL([]byte("   "), ""); err == nil {
		t.Fatal("空响应必须报错")
	}
}

func TestResponseMessageSurfacesNestedError(t *testing.T) {
	message := responseMessage([]byte(`{"error":{"message":"invalid token"}}`))
	if message != "invalid token" {
		t.Fatalf("得到 %q, want invalid token", message)
	}
}

func TestObjectKeyKeepsExtensionAndKindPrefix(t *testing.T) {
	key := objectKey("beeftv", MediaKindAudio, "voice.mp3")
	if !strings.HasPrefix(key, "beeftv/audio/") {
		t.Fatalf("key 应以 beeftv/audio/ 开头，得到 %q", key)
	}
	if !strings.HasSuffix(key, ".mp3") {
		t.Fatalf("key 应保留扩展名，得到 %q", key)
	}
}

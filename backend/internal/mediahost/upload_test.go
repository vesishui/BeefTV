package mediahost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadCustomSendsMultipartAndReadsConfiguredPath(t *testing.T) {
	var sawField string
	var sawAuth string
	var sawBytes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("解析 multipart 失败: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("media")
		if err != nil {
			t.Errorf("缺少 media 字段: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		sawField = header.Filename
		buf := make([]byte, 1024)
		read, _ := file.Read(buf)
		sawBytes = read
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"payload": map[string]any{"location": "https://cdn.example.com/uploaded.mp3"}})
	}))
	defer server.Close()

	uploader := NewUploader()
	config := Config{
		Provider:         ProviderCustom,
		Endpoint:         server.URL,
		CustomFileField:  "media",
		CustomURLPath:    "payload.location",
		CustomToken:      "secret-token",
		CustomAuthHeader: "Authorization",
		CustomAuthScheme: "Bearer",
	}
	link, err := uploader.Upload(context.Background(), config, MediaKindAudio, "voice.mp3", "audio/mpeg", []byte("audio-bytes"))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if link != "https://cdn.example.com/uploaded.mp3" {
		t.Fatalf("得到链接 %q", link)
	}
	if sawField != "voice.mp3" {
		t.Fatalf("服务端收到的文件名 = %q, want voice.mp3", sawField)
	}
	if sawAuth != "Bearer secret-token" {
		t.Fatalf("鉴权头 = %q, want %q", sawAuth, "Bearer secret-token")
	}
	if sawBytes == 0 {
		t.Fatal("服务端没有收到文件内容")
	}
}

func TestUploadReportsUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"token expired"}`))
	}))
	defer server.Close()

	uploader := NewUploader()
	_, err := uploader.Upload(context.Background(), Config{Provider: ProviderCustom, Endpoint: server.URL}, MediaKindImage, "a.png", "image/png", []byte("x"))
	if err == nil {
		t.Fatal("上游 401 必须报错，不能当成成功")
	}
	if !strings.Contains(err.Error(), "token expired") {
		t.Fatalf("错误应包含上游信息，得到 %v", err)
	}
}

func TestUploadRefusesDisabledProvider(t *testing.T) {
	uploader := NewUploader()
	_, err := uploader.Upload(context.Background(), DefaultConfig(), MediaKindImage, "a.png", "image/png", []byte("x"))
	if err != ErrDisabled {
		t.Fatalf("关闭状态应返回 ErrDisabled，得到 %v", err)
	}
}

func TestUploadRefusesVideoOnFreeProvider(t *testing.T) {
	uploader := NewUploader()
	_, err := uploader.Upload(context.Background(), Config{Provider: ProviderFree}, MediaKindVideo, "clip.mp4", "video/mp4", []byte("x"))
	if err != ErrKindUnsupported {
		t.Fatalf("免费图床收视频时必须明确拒绝，得到 %v", err)
	}
}

func TestUploadRefusesEmptyBody(t *testing.T) {
	uploader := NewUploader()
	_, err := uploader.Upload(context.Background(), Config{Provider: ProviderCustom, Endpoint: "https://example.com/upload"}, MediaKindImage, "a.png", "image/png", nil)
	if err != ErrSourceUnreadable {
		t.Fatalf("空素材应返回 ErrSourceUnreadable，得到 %v", err)
	}
}

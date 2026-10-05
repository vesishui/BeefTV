package mediahost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestEndToEndUploadThenFetch drives the whole loop the product relies on:
// upload local bytes, receive a URL, then fetch that URL back and confirm the
// bytes are intact. This is the "上传 → 返回链接 → 转发继续生成" path.
func TestEndToEndUploadThenFetch(t *testing.T) {
	var mu sync.Mutex
	stored := map[string][]byte{}
	var baseURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/upload":
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer file.Close()
			payload, _ := io.ReadAll(file)
			name := "/media/" + header.Filename
			mu.Lock()
			stored[name] = payload
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"url": baseURL + name}})
		case r.Method == http.MethodGet && len(r.URL.Path) > len("/media/"):
			mu.Lock()
			payload, ok := stored[r.URL.Path]
			mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(payload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	baseURL = server.URL

	uploader := NewUploader()
	original := []byte("PK\x03\x04 fake audio payload for round trip")
	link, err := uploader.Upload(context.Background(), Config{
		Provider:        ProviderCustom,
		Endpoint:        server.URL + "/upload",
		CustomURLPath:   "data.url",
		CustomFileField: "file",
	}, MediaKindAudio, "reference.mp3", "audio/mpeg", original)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if link == "" {
		t.Fatal("上传成功但没有返回链接")
	}

	response, err := http.Get(link)
	if err != nil {
		t.Fatalf("回读链接失败: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("回读状态 = %d, want 200", response.StatusCode)
	}
	fetched, _ := io.ReadAll(response.Body)
	if string(fetched) != string(original) {
		t.Fatalf("回读内容与上传内容不一致: %q", string(fetched))
	}
}

// TestEndToEndUploadsEveryMediaKind proves images, video and audio all travel
// the same path, which is the capability the reference implementation lacked.
func TestEndToEndUploadsEveryMediaKind(t *testing.T) {
	var seen []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		mu.Lock()
		seen = append(seen, header.Filename)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn.example.com/" + header.Filename})
	}))
	defer server.Close()

	uploader := NewUploader()
	cases := []struct {
		kind        MediaKind
		filename    string
		contentType string
	}{
		{MediaKindImage, "shot.png", "image/png"},
		{MediaKindVideo, "clip.mp4", "video/mp4"},
		{MediaKindAudio, "voice.mp3", "audio/mpeg"},
	}
	for _, testCase := range cases {
		if _, err := uploader.Upload(context.Background(), Config{Provider: ProviderCustom, Endpoint: server.URL}, testCase.kind, testCase.filename, testCase.contentType, []byte("payload-"+string(testCase.kind))); err != nil {
			t.Fatalf("%s 上传失败: %v", testCase.kind, err)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("应有 3 次上传，实际 %d 次：%v", len(seen), seen)
	}
}

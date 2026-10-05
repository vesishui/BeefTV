package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"infinite-canvas/backend/internal/mediahost"
	"infinite-canvas/backend/internal/workspace"

	"github.com/gin-gonic/gin"
)

func newMediaHostingRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	router := gin.New()
	group := router.Group("/api")
	group.Use(func(c *gin.Context) {
		c.Set(workspaceContextKey, workspace.Context{ID: "local", DataDir: dataDir})
		c.Next()
	})
	RegisterMediaHostingRoutes(group)
	return router, dataDir
}

func TestMediaHostingGetReturnsDisabledDefault(t *testing.T) {
	router, _ := newMediaHostingRouter(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/workspace/media-hosting", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200；body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Config  mediahost.Config `json:"config"`
			Enabled bool             `json:"enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if envelope.Data.Enabled {
		t.Fatal("默认必须是关闭状态")
	}
	if envelope.Data.Config.Provider != mediahost.ProviderNone {
		t.Fatalf("provider = %q, want none", envelope.Data.Config.Provider)
	}
}

func TestMediaHostingPutThenGetRoundTrip(t *testing.T) {
	router, _ := newMediaHostingRouter(t)
	body, _ := json.Marshal(map[string]any{
		"config": mediahost.Config{
			Provider:        mediahost.ProviderCustom,
			Endpoint:        "https://img.example.com/upload",
			CustomToken:     "tok-123",
			CustomURLPath:   "data.url",
			CustomFileField: "file",
		},
	})
	put := httptest.NewRecorder()
	router.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/workspace/media-hosting", bytes.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("写入状态码 = %d；body=%s", put.Code, put.Body.String())
	}

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/workspace/media-hosting", nil))
	var envelope struct {
		Data struct {
			Config  mediahost.Config `json:"config"`
			Enabled bool             `json:"enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !envelope.Data.Enabled {
		t.Fatal("写入后应为启用状态")
	}
	if envelope.Data.Config.Endpoint != "https://img.example.com/upload" {
		t.Fatalf("endpoint 未持久化: %q", envelope.Data.Config.Endpoint)
	}
}

func TestMediaHostingGetNeverLeaksSecret(t *testing.T) {
	router, _ := newMediaHostingRouter(t)
	body, _ := json.Marshal(map[string]any{
		"config": mediahost.Config{
			Provider:    mediahost.ProviderCustom,
			Endpoint:    "https://img.example.com/upload",
			CustomToken: "super-secret-value",
		},
	})
	put := httptest.NewRecorder()
	router.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/workspace/media-hosting", bytes.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("写入失败: %s", put.Body.String())
	}

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/workspace/media-hosting", nil))
	if bytes.Contains(get.Body.Bytes(), []byte("super-secret-value")) {
		t.Fatalf("响应泄露了密钥: %s", get.Body.String())
	}
	if !bytes.Contains(get.Body.Bytes(), []byte(mediahost.RedactedSecret)) {
		t.Fatalf("已设置密钥应返回占位符，实际: %s", get.Body.String())
	}
}

func TestMediaHostingPutKeepsSecretWhenPlaceholderReturned(t *testing.T) {
	router, dataDir := newMediaHostingRouter(t)
	first, _ := json.Marshal(map[string]any{
		"config": mediahost.Config{Provider: mediahost.ProviderCustom, Endpoint: "https://a.example.com/u", CustomToken: "original-token"},
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/workspace/media-hosting", bytes.NewReader(first)))

	// The browser sends the placeholder back on a later save.
	second, _ := json.Marshal(map[string]any{
		"config": mediahost.Config{Provider: mediahost.ProviderCustom, Endpoint: "https://b.example.com/u", CustomToken: mediahost.RedactedSecret},
	})
	put := httptest.NewRecorder()
	router.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/workspace/media-hosting", bytes.NewReader(second)))
	if put.Code != http.StatusOK {
		t.Fatalf("二次写入失败: %s", put.Body.String())
	}

	store, err := mediahost.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if stored.CustomToken != "original-token" {
		t.Fatalf("占位符不应覆盖已存密钥，得到 %q", stored.CustomToken)
	}
	if stored.Endpoint != "https://b.example.com/u" {
		t.Fatalf("endpoint 应更新为新的值，得到 %q", stored.Endpoint)
	}
}

func TestMediaHostingPutRejectsInvalidConfig(t *testing.T) {
	router, _ := newMediaHostingRouter(t)
	body, _ := json.Marshal(map[string]any{"config": mediahost.Config{Provider: mediahost.ProviderCustom}})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/workspace/media-hosting", bytes.NewReader(body)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("缺少上传地址应返回 400，得到 %d", recorder.Code)
	}
}

func TestMediaHostingTestUploadRejectsWhenDisabled(t *testing.T) {
	router, _ := newMediaHostingRouter(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/workspace/media-hosting/test", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("未启用时应返回 400，得到 %d", recorder.Code)
	}
}

func TestMediaHostingTestUploadSucceedsAgainstStubProvider(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"url": "https://cdn.example.com/probe.png"}})
	}))
	defer provider.Close()

	router, _ := newMediaHostingRouter(t)
	body, _ := json.Marshal(map[string]any{
		"config": mediahost.Config{
			Provider:        mediahost.ProviderCustom,
			Endpoint:        provider.URL,
			CustomFileField: "file",
			CustomURLPath:   "data.url",
		},
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/workspace/media-hosting", bytes.NewReader(body)))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/workspace/media-hosting/test?kind=audio", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("测试上传失败: %d %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			URL  string `json:"url"`
			Kind string `json:"kind"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.URL != "https://cdn.example.com/probe.png" {
		t.Fatalf("url = %q", envelope.Data.URL)
	}
	if envelope.Data.Kind != "audio" {
		t.Fatalf("kind = %q, want audio（音频必须能走图床）", envelope.Data.Kind)
	}
}

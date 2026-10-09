package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/app"
	"infinite-canvas/backend/internal/workspace"

	"github.com/gin-gonic/gin"
)

func newModelConfigTestRouterWithDir(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	store, err := workspace.NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewLocal(nil, dir)
	router := gin.New()
	router.Use(WorkspaceMiddleware(workspace.Context{ID: "local", DataDir: dir}))
	router.Use(RuntimeDependenciesMiddleware(RuntimeDependencies{ProviderConfig: store}))
	RegisterWorkspaceRoutes(router.Group("/api"), service)
	return router, dir
}

// 用户遇到的正是这种文件：内容完好，但密文用带填充的 base64 写成。
// 保存接口过去会一直返回「系统处理失败，请稍后重试」。
func TestWorkspaceModelConfigSavesOverPaddedCiphertext(t *testing.T) {
	router, dir := newModelConfigTestRouterWithDir(t)
	if first := putModelConfig(t, router, 0, "first-secret"); first.Code != http.StatusOK {
		t.Fatalf("seed put status = %d body=%s", first.Code, first.Body.String())
	}
	padStoredModelConfig(t, filepath.Join(dir, workspace.LocalProviderConfigFile))

	saved := putModelConfig(t, router, 1, "second-secret")
	if saved.Code != http.StatusOK {
		t.Fatalf("save over padded ciphertext status = %d body=%s", saved.Code, saved.Body.String())
	}
	if revision := modelConfigResponseData(t, saved)["revision"]; revision != float64(2) {
		t.Fatalf("committed revision = %#v", revision)
	}
	body, err := os.ReadFile(filepath.Join(dir, workspace.LocalProviderConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "=") {
		t.Fatal("rewritten config should be canonical unpadded form")
	}
	if bytes.Contains(body, []byte("second-secret")) {
		t.Fatal("config stored a plaintext credential")
	}
}

// 配置真的读不出来时必须给出可操作的原因，而不是笼统的「系统处理失败」。
func TestWorkspaceModelConfigReportsUnreadableConfig(t *testing.T) {
	router, dir := newModelConfigTestRouterWithDir(t)
	if seed := putModelConfig(t, router, 0, "seed-secret"); seed.Code != http.StatusOK {
		t.Fatalf("seed put status = %d body=%s", seed.Code, seed.Body.String())
	}
	for _, name := range []string{workspace.LocalProviderConfigFile, workspace.LocalProviderConfigFile + ".bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	recorder := httptest.NewRecorder()
	body, err := json.Marshal(map[string]any{
		"expectedRevision": 1,
		"config":           map[string]any{"channels": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/workspace/model-config", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	message := recorder.Body.String()
	if !strings.Contains(message, "本地模型配置无法读取") {
		t.Fatalf("expected an actionable message, got %s", message)
	}
	if strings.Contains(message, "系统处理失败") {
		t.Fatalf("unreadable config surfaced an unactionable error: %s", message)
	}
	if strings.Contains(message, "seed-secret") {
		t.Fatalf("error response leaked a credential: %s", message)
	}
}

func padStoredModelConfig(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	encoded, _ := envelope["encryptedConfig"].(string)
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, "enc:v1:"))
	if err != nil {
		t.Fatal(err)
	}
	padded := "enc:v1:" + base64.StdEncoding.EncodeToString(raw)
	if !strings.Contains(padded, "=") {
		t.Fatal("fixture must actually be padded")
	}
	envelope["encryptedConfig"] = padded
	next, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, next, 0o600); err != nil {
		t.Fatal(err)
	}
}

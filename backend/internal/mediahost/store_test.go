package mediahost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("构建 store 失败: %v", err)
	}
	return store
}

func TestReadFallsBackToDisabledDefault(t *testing.T) {
	store := newTestStore(t)
	config, err := store.Read()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if config.Enabled() {
		t.Fatal("没有配置文件时必须返回关闭状态")
	}
}

func TestWriteThenReadRoundTripsConfig(t *testing.T) {
	store := newTestStore(t)
	want := Config{
		Provider:         ProviderCustom,
		Endpoint:         "https://img.example.com/upload",
		CustomFileField:  "media",
		CustomURLPath:    "data.url",
		CustomToken:      "super-secret-token",
		CustomAuthHeader: "X-Api-Key",
		CustomAuthScheme: "",
	}
	if err := store.Write(want); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	got, err := store.Read()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Provider != ProviderCustom || got.Endpoint != want.Endpoint {
		t.Fatalf("配置往返不一致: %+v", got)
	}
	if got.CustomToken != "super-secret-token" {
		t.Fatalf("密钥未正确解密，得到 %q", got.CustomToken)
	}
}

func TestWriteEncryptsSecretsAtRest(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(Config{Provider: ProviderCustom, Endpoint: "https://x.example.com/u", CustomToken: "plaintext-token-should-not-appear"}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, ConfigFileName))
	if err != nil {
		t.Fatalf("读取落盘文件失败: %v", err)
	}
	if strings.Contains(string(raw), "plaintext-token-should-not-appear") {
		t.Fatal("密钥以明文落盘，这是安全问题")
	}
	if !strings.Contains(string(raw), "enc:v1:") {
		t.Fatalf("密钥未被加密，文件内容: %s", string(raw))
	}
}

func TestRedactHidesSecretsAndMarksThemSet(t *testing.T) {
	config := Config{Provider: ProviderFree, FreeProvider: FreeProviderImgBB, ImgBBApiKey: "real-key"}
	redacted := Redact(config)
	if redacted.ImgBBApiKey != RedactedSecret {
		t.Fatalf("脱敏后应为占位符，得到 %q", redacted.ImgBBApiKey)
	}
}

func TestRedactLeavesUnsetSecretsEmpty(t *testing.T) {
	redacted := Redact(Config{Provider: ProviderCustom, Endpoint: "https://x.example.com/u"})
	if redacted.CustomToken != "" {
		t.Fatalf("未设置的密钥不应变成占位符，得到 %q", redacted.CustomToken)
	}
}

func TestMergeRedactedSecretsPreservesStoredValue(t *testing.T) {
	existing := Config{Provider: ProviderCustom, Endpoint: "https://x.example.com/u", CustomToken: "stored-token"}
	incoming := Config{Provider: ProviderCustom, Endpoint: "https://x.example.com/u", CustomToken: RedactedSecret}
	merged := MergeRedactedSecrets(incoming, existing)
	if merged.CustomToken != "stored-token" {
		t.Fatalf("占位符应还原为已存密钥，得到 %q", merged.CustomToken)
	}
}

func TestMergeRedactedSecretsAcceptsNewValue(t *testing.T) {
	existing := Config{Provider: ProviderCustom, Endpoint: "https://x.example.com/u", CustomToken: "old-token"}
	incoming := Config{Provider: ProviderCustom, Endpoint: "https://x.example.com/u", CustomToken: "new-token"}
	merged := MergeRedactedSecrets(incoming, existing)
	if merged.CustomToken != "new-token" {
		t.Fatalf("新密钥应被采纳，得到 %q", merged.CustomToken)
	}
}

func TestWriteRejectsInvalidConfig(t *testing.T) {
	store := newTestStore(t)
	if err := store.Write(Config{Provider: ProviderCustom}); err == nil {
		t.Fatal("缺少上传地址时必须拒绝写入")
	}
}

func TestConcurrentStoresShareGuard(t *testing.T) {
	dataDir := t.TempDir()
	first, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if first.mu != second.mu {
		t.Fatal("同一目录的 store 应共享互斥锁，避免并发写入互相覆盖")
	}
}

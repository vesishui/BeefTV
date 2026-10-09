package workspace

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 把主文件重新编码成带填充的 base64，模拟外部脚本写出的形式。
func padStoredConfig(t *testing.T, path string) {
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
	envelope["encryptedConfig"] = "enc:v1:" + base64.StdEncoding.EncodeToString(raw)
	padded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, padded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProviderConfigReadsPaddedCiphertext(t *testing.T) {
	dir := t.TempDir()
	store, err := NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocalModelConfig([]byte(`{"channels":[{"id":"custom","apiKey":"private-api"}]}`)); err != nil {
		t.Fatal(err)
	}
	padStoredConfig(t, filepath.Join(dir, LocalProviderConfigFile))

	reopened, err := NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	effective, health, err := reopened.LoadEffectiveModelConfig()
	if err != nil {
		t.Fatalf("padded ciphertext must stay readable: %v", err)
	}
	if health != ConfigHealthReady {
		t.Fatalf("health = %s, want %s", health, ConfigHealthReady)
	}
	if effective.Revision != 1 {
		t.Fatalf("revision = %d, want 1", effective.Revision)
	}
	// 保存必须继续可用，且下一次写入会自愈成规范形式。
	revision, err := reopened.SaveLocalModelConfigRevision([]byte(`{"channels":[{"id":"custom","apiKey":"rotated"}]}`), 1)
	if err != nil {
		t.Fatalf("save over padded ciphertext failed: %v", err)
	}
	if revision != 2 {
		t.Fatalf("revision = %d, want 2", revision)
	}
	body, err := os.ReadFile(filepath.Join(dir, LocalProviderConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "=") {
		t.Fatal("rewritten config should be canonical unpadded form")
	}
}

// 损坏的主文件绝不能被静默替换：那会丢掉仍可人工修复的内容。
// 保存必须失败，并给出可操作的原因而不是「系统处理失败」。
func TestSaveRefusesToReplaceCorruptPrimary(t *testing.T) {
	dir := t.TempDir()
	store, err := NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocalModelConfig([]byte(`{"channels":[{"id":"custom","apiKey":"first"}]}`)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, LocalProviderConfigFile)
	corrupt := []byte("{not json")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	broken, err := NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = broken.SaveLocalModelConfig([]byte(`{"channels":[{"id":"custom","apiKey":"replacement"}]}`))
	if !errors.Is(err, ErrProviderConfigUnreadable) {
		t.Fatalf("save error = %v, want ErrProviderConfigUnreadable", err)
	}
	if strings.Contains(err.Error(), "系统处理失败") {
		t.Fatal("save surfaced an unactionable error")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(corrupt) {
		t.Fatal("corrupt primary was modified")
	}
}

// 主文件与备份都读不出来时必须明确报错，让接口能给出可操作的提示，
// 而不是「系统处理失败，请稍后重试」。
func TestUnreadableConfigReportsActionableError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocalModelConfig([]byte(`{"channels":[]}`)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{LocalProviderConfigFile, LocalProviderConfigFile + ".bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	broken, err := NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _, loadErr := broken.LoadEffectiveModelConfig()
	if !errors.Is(loadErr, ErrProviderConfigUnreadable) {
		t.Fatalf("load error = %v, want ErrProviderConfigUnreadable", loadErr)
	}
	if err := broken.SaveLocalModelConfig([]byte(`{"channels":[]}`)); !errors.Is(err, ErrProviderConfigUnreadable) {
		t.Fatalf("save error = %v, want ErrProviderConfigUnreadable", err)
	}
	// 密钥绝不能被重建：否则原始密文会被永久作废。
	if _, err := os.Stat(filepath.Join(dir, ".settings-key")); err != nil {
		t.Fatalf("workspace key went missing: %v", err)
	}
}

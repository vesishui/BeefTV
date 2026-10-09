package localcrypto

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	encrypted, err := Encrypt(dir, "private-api-key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, Prefix) {
		t.Fatalf("ciphertext %q is missing the version prefix", encrypted)
	}
	// The canonical on-disk form stays unpadded.
	if strings.Contains(strings.TrimPrefix(encrypted, Prefix), "=") {
		t.Fatal("canonical ciphertext must not be padded")
	}
	plain, err := Decrypt(dir, encrypted)
	if err != nil || plain != "private-api-key" {
		t.Fatalf("round trip = %q err:%v", plain, err)
	}
}

// 外部工具与历史修复脚本写过带填充的 base64。只接受无填充形式会让一份内容
// 完好的配置永远读不出来，保存接口则持续返回「系统处理失败」。
func TestDecryptAcceptsPaddedPayload(t *testing.T) {
	dir := t.TempDir()
	encrypted, err := Encrypt(dir, "private-api-key")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encrypted, Prefix))
	if err != nil {
		t.Fatal(err)
	}
	padded := Prefix + base64.StdEncoding.EncodeToString(raw)
	if !strings.Contains(padded, "=") {
		t.Fatal("fixture must actually be padded")
	}
	plain, err := Decrypt(dir, padded)
	if err != nil || plain != "private-api-key" {
		t.Fatalf("padded ciphertext rejected: %q err:%v", plain, err)
	}
}

func TestDecryptRejectsMalformedPayloads(t *testing.T) {
	dir := t.TempDir()
	if _, err := Encrypt(dir, "seed"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"plain-text", Prefix + "not base64!!", Prefix + "AAAA", "enc:v2:AAAA"} {
		if _, err := Decrypt(dir, value); err == nil {
			t.Fatalf("malformed ciphertext %q accepted", value)
		}
	}
	if plain, err := Decrypt(dir, ""); err != nil || plain != "" {
		t.Fatalf("empty value = %q err:%v", plain, err)
	}
}

// Decrypt 绝不能为了读取而新建密钥，否则已有密文会被静默作废。
func TestDecryptNeverCreatesKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := Decrypt(dir, Prefix+"AAAA"); err == nil {
		t.Fatal("expected failure without a key")
	}
	if _, err := os.Stat(filepath.Join(dir, ".settings-key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("decrypt created a replacement key")
	}
}

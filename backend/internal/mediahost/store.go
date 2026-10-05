package mediahost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"infinite-canvas/backend/internal/localcrypto"
)

// ConfigFileName is the workspace file holding media-hosting settings.
const ConfigFileName = "media-hosting.json"

// RedactedSecret is the placeholder returned to the browser in place of an
// encrypted secret, so the UI can show "already set" without receiving it.
const RedactedSecret = "__BEEFTV_REDACTED__"

// secretFields lists the config keys encrypted at rest. A key offered in the
// browser URL or kept in a canvas document would leak to the model provider.
var secretFields = []string{"imgbbApiKey", "customToken", "accessKeyId", "secretAccessKey", "sessionToken"}

// Store persists media-hosting configuration under the workspace data
// directory. Secrets are encrypted with the same workspace key used for
// provider settings.
type Store struct {
	dataDir string
	mu      *sync.Mutex
}

var storeGuards sync.Map // canonical dir -> *sync.Mutex

// NewStore builds a store for a workspace data directory.
func NewStore(dataDir string) (*Store, error) {
	trimmed := strings.TrimSpace(dataDir)
	if trimmed == "" {
		return nil, errors.New("本地工作区数据目录不能为空")
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, fmt.Errorf("解析本地工作区数据目录失败: %w", err)
	}
	canonical := filepath.Clean(absolute)
	guard, _ := storeGuards.LoadOrStore(canonical, &sync.Mutex{})
	return &Store{dataDir: canonical, mu: guard.(*sync.Mutex)}, nil
}

func (s *Store) path() string {
	return filepath.Join(s.dataDir, ConfigFileName)
}

// Read returns the stored configuration, falling back to the disabled default
// when no file exists yet. Secrets come back decrypted for internal use; the
// browser-facing view is built by Redact.
func (s *Store) Read() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *Store) readLocked() (Config, error) {
	payload, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("读取图床配置失败: %w", err)
	}
	if len(payload) == 0 {
		return DefaultConfig(), nil
	}
	var stored map[string]any
	if err := json.Unmarshal(payload, &stored); err != nil {
		return Config{}, fmt.Errorf("图床配置损坏: %w", err)
	}
	config := DefaultConfig()
	encoded, err := json.Marshal(stored)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(encoded, &config); err != nil {
		return Config{}, fmt.Errorf("图床配置损坏: %w", err)
	}
	for _, field := range secretFields {
		raw, ok := stored[field]
		if !ok {
			continue
		}
		value, _ := raw.(string)
		if value == "" {
			continue
		}
		plaintext, err := localcrypto.Decrypt(s.dataDir, value)
		if err != nil {
			// A config written before encryption, or with a rotated key, is
			// surfaced as "not set" instead of blocking the whole workspace.
			setSecretField(&config, field, "")
			continue
		}
		setSecretField(&config, field, plaintext)
	}
	return Normalize(config), nil
}

// Write encrypts secrets and persists the configuration atomically.
func (s *Store) Write(config Config) error {
	normalized := Normalize(config)
	if err := Validate(normalized); err != nil {
		return err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	var stored map[string]any
	if err := json.Unmarshal(encoded, &stored); err != nil {
		return err
	}
	for _, field := range secretFields {
		value := getSecretField(normalized, field)
		if value == "" {
			delete(stored, field)
			continue
		}
		ciphertext, err := localcrypto.Encrypt(s.dataDir, value)
		if err != nil {
			return fmt.Errorf("加密图床密钥失败: %w", err)
		}
		stored[field] = ciphertext
	}
	payload, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dataDir, 0o700); err != nil {
		return fmt.Errorf("创建本地工作区目录失败: %w", err)
	}
	temporary, err := os.CreateTemp(s.dataDir, ".media-hosting-*")
	if err != nil {
		return fmt.Errorf("写入图床配置失败: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return fmt.Errorf("写入图床配置失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path())
}

// Redact replaces decrypted secrets with a placeholder so the value can travel
// to the browser without exposing the key itself.
func Redact(config Config) Config {
	redacted := config
	for _, field := range secretFields {
		if getSecretField(redacted, field) != "" {
			setSecretField(&redacted, field, RedactedSecret)
		}
	}
	return redacted
}

// MergeRedactedSecrets preserves stored secrets when the browser sends back
// the placeholder, mirroring how provider settings are handled.
func MergeRedactedSecrets(incoming Config, existing Config) Config {
	merged := Normalize(incoming)
	for _, field := range secretFields {
		if getSecretField(merged, field) == RedactedSecret {
			setSecretField(&merged, field, getSecretField(existing, field))
		}
	}
	return merged
}

func getSecretField(config Config, field string) string {
	switch field {
	case "imgbbApiKey":
		return config.ImgBBApiKey
	case "customToken":
		return config.CustomToken
	case "accessKeyId":
		return config.AccessKeyID
	case "secretAccessKey":
		return config.SecretAccessKey
	case "sessionToken":
		return config.SessionToken
	default:
		return ""
	}
}

func setSecretField(config *Config, field string, value string) {
	switch field {
	case "imgbbApiKey":
		config.ImgBBApiKey = value
	case "customToken":
		config.CustomToken = value
	case "accessKeyId":
		config.AccessKeyID = value
	case "secretAccessKey":
		config.SecretAccessKey = value
	case "sessionToken":
		config.SessionToken = value
	}
}

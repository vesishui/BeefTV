package mediahost

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	uploadTimeout      = 2 * time.Minute
	maxUploadBytes     = 200 * 1024 * 1024
	objectKeyTimestamp = "20060102T150405Z"
)

// Uploader sends media to whichever provider the configuration selects.
type Uploader struct {
	client *http.Client
	// Now is injectable so tests can pin signature timestamps.
	Now func() time.Time
	// Cache reuses the URL produced for identical bytes. Optional.
	Cache *Cache
	// Log receives one line per decision so operators can see cache hits
	// without attaching a debugger. Optional.
	Log func(format string, args ...any)
}

// NewUploader builds an uploader with sane transport defaults.
func NewUploader() *Uploader {
	return &Uploader{
		client: &http.Client{Timeout: uploadTimeout},
		Now:    time.Now,
	}
}

func (u *Uploader) logf(format string, args ...any) {
	if u.Log != nil {
		u.Log(format, args...)
	}
}

// Upload stores the media and returns a URL the upstream provider can fetch.
//
// filename and contentType describe the bytes; providers that only accept
// images reject other kinds through SupportsKind before this runs.
func (u *Uploader) Upload(ctx context.Context, config Config, kind MediaKind, filename string, contentType string, body []byte) (string, error) {
	normalized := Normalize(config)
	if normalized.Provider == ProviderNone {
		return "", ErrDisabled
	}
	if !normalized.SupportsKind(kind) {
		return "", ErrKindUnsupported
	}
	if len(body) == 0 {
		return "", ErrSourceUnreadable
	}
	if len(body) > maxUploadBytes {
		return "", fmt.Errorf("素材超过 %s，无法通过图床上传", fileSizeLimit(maxUploadBytes))
	}
	if filename == "" {
		filename = "beeftv-media"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Content-addressed reuse: the same bytes re-sent on a later generation
	// should not be uploaded again.
	hash := HashContent(body)
	if cached, ok := u.Cache.Lookup(hash); ok {
		started := time.Now()
		if u.URLAlive(ctx, cached.URL) {
			u.logf("[图床] 命中缓存 %s → %s (存活=true, %dms)", shortHash(hash), safeURLHost(cached.URL), time.Since(started).Milliseconds())
			return cached.URL, nil
		}
		u.logf("[图床] 缓存失效 %s → %s (存活=false)，重新上传", shortHash(hash), safeURLHost(cached.URL))
		_ = u.Cache.Forget(hash)
	}

	started := time.Now()
	link, err := u.uploadNew(ctx, normalized, kind, filename, contentType, body)
	if err != nil {
		u.logf("[图床] 上传失败 provider=%s kind=%s: %s", normalized.Provider, kind, redactUploadLogText(err.Error()))
		return "", err
	}
	u.logf("[图床] 上传成功 provider=%s kind=%s → %s (%dms)", normalized.Provider, kind, safeURLHost(link), time.Since(started).Milliseconds())
	if storeErr := u.Cache.Store(hash, CacheEntry{
		URL:      link,
		Provider: string(normalized.Provider),
		Kind:     string(kind),
		Bytes:    int64(len(body)),
	}); storeErr != nil {
		// A cache write failure must not fail a generation that already has a
		// usable URL.
		u.logf("[图床] 缓存写入失败: %v", storeErr)
	}
	return link, nil
}

func safeURLHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return "公网链接"
	}
	return parsed.Host
}

func redactUploadLogText(value string) string {
	return uploadLogURLPattern.ReplaceAllString(value, "[URL]")
}

var uploadLogURLPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

// uploadNew performs the provider-specific upload without cache involvement.
func (u *Uploader) uploadNew(ctx context.Context, normalized Config, kind MediaKind, filename string, contentType string, body []byte) (string, error) {
	switch normalized.Provider {
	case ProviderBuiltin:
		// The builtin provider serves the existing local resource through a
		// signed URL; it never receives bytes here. Reaching this branch means
		// a caller bypassed the hydration policy.
		return "", errors.New("内置图床不接收上传，请通过素材签名链接访问")
	case ProviderFree:
		return u.uploadFree(ctx, normalized, filename, contentType, body)
	case ProviderCustom:
		return u.uploadMultipart(ctx, normalized, filename, contentType, body)
	case ProviderAliyunOSS:
		return u.uploadS3Compatible(ctx, normalized, filename, contentType, body, s3StyleAliyun)
	case ProviderTencentCOS:
		return u.uploadS3Compatible(ctx, normalized, filename, contentType, body, s3StyleTencent)
	case ProviderCloudflareR2:
		return u.uploadS3Compatible(ctx, normalized, filename, contentType, body, s3StyleCloudflareR2)
	case ProviderAWSS3:
		return u.uploadS3Compatible(ctx, normalized, filename, contentType, body, s3StyleAWSS3)
	default:
		return "", fmt.Errorf("未知图床类型 %q", normalized.Provider)
	}
}

func shortHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}

func (u *Uploader) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *Uploader) do(ctx context.Context, request *http.Request) (*http.Response, error) {
	client := u.client
	if client == nil {
		client = &http.Client{Timeout: uploadTimeout}
	}
	return client.Do(request.WithContext(ctx))
}

func (u *Uploader) uploadFree(ctx context.Context, config Config, filename, contentType string, body []byte) (string, error) {
	candidates := []FreeProvider{config.FreeProvider}
	if config.FreeProvider == FreeProviderAuto {
		candidates = []FreeProvider{}
		if strings.TrimSpace(config.ImgBBApiKey) != "" {
			candidates = append(candidates, FreeProviderImgBB)
		}
		candidates = append(candidates, FreeProviderLitterbox, FreeProviderUguu)
	}

	var failures []string
	for _, candidate := range candidates {
		url, err := u.uploadFreeOnce(ctx, config, candidate, filename, contentType, body)
		if err == nil {
			return url, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %s", candidate, err))
	}
	if len(failures) == 1 {
		return "", fmt.Errorf("免费图床上传失败：%s", failures[0])
	}
	return "", fmt.Errorf("免费图床均不可用：%s", strings.Join(failures, "；"))
}

func (u *Uploader) uploadFreeOnce(ctx context.Context, config Config, provider FreeProvider, filename, contentType string, body []byte) (string, error) {
	var endpoint string
	form := &bytes.Buffer{}
	writer := multipart.NewWriter(form)

	appendFile := func(field string) error {
		part, err := writer.CreateFormFile(field, filename)
		if err != nil {
			return err
		}
		_, err = part.Write(body)
		return err
	}

	switch provider {
	case FreeProviderImgBB:
		if strings.TrimSpace(config.ImgBBApiKey) == "" {
			return "", fmt.Errorf("未配置 ImgBB API Key")
		}
		target, err := url.Parse("https://api.imgbb.com/1/upload")
		if err != nil {
			return "", err
		}
		query := target.Query()
		query.Set("key", config.ImgBBApiKey)
		target.RawQuery = query.Encode()
		endpoint = target.String()
		if err := appendFile("image"); err != nil {
			return "", err
		}
	case FreeProviderUguu:
		endpoint = "https://uguu.se/upload.php"
		if err := appendFile("files[]"); err != nil {
			return "", err
		}
	default:
		endpoint = "https://litterbox.catbox.moe/resources/internals/api.php"
		if err := writer.WriteField("reqtype", "fileupload"); err != nil {
			return "", err
		}
		if err := writer.WriteField("time", config.FreeExpiry); err != nil {
			return "", err
		}
		if err := appendFile("fileToUpload"); err != nil {
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	request, err := http.NewRequest(http.MethodPost, endpoint, form)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := u.do(ctx, request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("上游返回 %d：%s", response.StatusCode, responseMessage(payload))
	}
	link, err := extractUploadedURL(payload, config.CustomURLPath)
	if err != nil {
		return "", err
	}
	return link, nil
}

func (u *Uploader) uploadMultipart(ctx context.Context, config Config, filename, contentType string, body []byte) (string, error) {
	form := &bytes.Buffer{}
	writer := multipart.NewWriter(form)
	field := config.CustomFileField
	if field == "" {
		field = "file"
	}
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(body); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	request, err := http.NewRequest(http.MethodPost, config.Endpoint, form)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if token := strings.TrimSpace(config.CustomToken); token != "" {
		header := strings.TrimSpace(config.CustomAuthHeader)
		if header == "" {
			header = "Authorization"
		}
		value := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(config.CustomAuthScheme), token}, " "))
		request.Header.Set(header, value)
	}
	response, err := u.do(ctx, request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("自定义图床返回 %d：%s", response.StatusCode, responseMessage(payload))
	}
	return extractUploadedURL(payload, config.CustomURLPath)
}

// responseMessage surfaces an upstream error without dumping a whole body.
func responseMessage(payload []byte) string {
	text := strings.TrimSpace(string(payload))
	if text == "" {
		return "服务无响应"
	}
	var parsed map[string]any
	if json.Unmarshal(payload, &parsed) == nil {
		for _, key := range []string{"message", "error", "msg", "detail"} {
			if value, ok := parsed[key]; ok {
				switch typed := value.(type) {
				case string:
					if strings.TrimSpace(typed) != "" {
						return truncate(typed, 300)
					}
				case map[string]any:
					if nested, ok := typed["message"].(string); ok && strings.TrimSpace(nested) != "" {
						return truncate(nested, 300)
					}
				}
			}
		}
	}
	return truncate(text, 300)
}

// extractUploadedURL reads the URL out of an upload response. A configured
// dotted path wins; otherwise common shapes are tried so most self-hosted
// endpoints work without extra configuration.
func extractUploadedURL(payload []byte, dottedPath string) (string, error) {
	text := strings.TrimSpace(string(payload))
	if text == "" {
		return "", fmt.Errorf("图床返回了空响应")
	}
	if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		return text, nil
	}

	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("无法识别图床返回内容：%s", truncate(text, 200))
	}
	if dottedPath != "" {
		if value, ok := lookupDotted(decoded, dottedPath); ok {
			if link, ok := value.(string); ok && strings.HasPrefix(link, "http") {
				return link, nil
			}
		}
	}
	for _, candidate := range []string{
		"data.links.url", "data.url", "data.image.url", "data.display_url",
		"url", "link", "location", "data.0.url", "files.0.url", "result.url",
	} {
		if value, ok := lookupDotted(decoded, candidate); ok {
			if link, ok := value.(string); ok && strings.HasPrefix(link, "http") {
				return link, nil
			}
		}
	}
	return "", fmt.Errorf("图床返回中没有可用的链接")
}

func lookupDotted(root any, dotted string) (any, bool) {
	current := root
	for _, segment := range strings.Split(dotted, ".") {
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[segment]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index := 0
			if _, err := fmt.Sscanf(segment, "%d", &index); err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func truncate(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "..."
}

func fileSizeLimit(bytes int) string {
	const unit = 1024 * 1024
	return fmt.Sprintf("%dMB", bytes/unit)
}

// objectKey builds a collision-resistant storage path for an upload.
func objectKey(prefix string, kind MediaKind, filename string) string {
	extension := strings.ToLower(path.Ext(filename))
	if extension == "" {
		extension = extensionForKind(kind)
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		random = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return path.Join(prefix, string(kind), hex.EncodeToString(random)+extension)
}

func extensionForKind(kind MediaKind) string {
	switch kind {
	case MediaKindImage:
		return ".png"
	case MediaKindVideo:
		return ".mp4"
	case MediaKindAudio:
		return ".mp3"
	default:
		return ".bin"
	}
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

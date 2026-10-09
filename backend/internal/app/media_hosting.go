package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/mediahost"
	"infinite-canvas/backend/internal/outbound"
)

// mediaHostingAdapter answers the generation layer's media-hosting question by
// reading the workspace media-hosting configuration.
//
// The configuration is re-read per request rather than cached at startup so a
// settings change takes effect on the next generation without a restart. The
// file is small and the read is cheap; misses fall back to upstream behaviour.
type mediaHostingAdapter struct {
	dataDir string

	mu       sync.Mutex
	store    *mediahost.Store
	uploader *mediahost.Uploader
	cache    *mediahost.Cache
}

// installMediaHosting wires the media-hosting decision source into the
// generation layer. It is called from the application composition root.
func (s *Service) installMediaHosting() {
	if s == nil {
		return
	}
	adapter, err := newMediaHostingAdapter(s.DataDir())
	if err != nil {
		// Media hosting is optional; a workspace without it keeps working.
		log.Printf("[图床] 初始化跳过: %v", err)
		return
	}
	generation.SetMediaHostingProvider(adapter)
}

func newMediaHostingAdapter(dataDir string) (*mediaHostingAdapter, error) {
	store, err := mediahost.NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	cache, err := mediahost.NewCache(dataDir)
	if err != nil {
		return nil, err
	}
	uploader := mediahost.NewUploader()
	uploader.Cache = cache
	uploader.Log = func(format string, args ...any) {
		log.Printf(format, args...)
	}
	return &mediaHostingAdapter{dataDir: dataDir, store: store, uploader: uploader, cache: cache}, nil
}

// HostingPolicy implements generation.MediaHostingProvider.
func (a *mediaHostingAdapter) HostingPolicy(ctx context.Context, channelID string, interfaceType string) (mediahost.Decision, error) {
	if a == nil {
		return mediahost.Decision{}, nil
	}
	// Declarative protocols ship as plugin packages, so the manifest that
	// declares requiresPublicMediaUrls is not in the context until the official
	// adapter is loaded. Resolve it first; otherwise a protocol such as
	// OpenVideo falls through to the manual HTTPS-link dialog.
	ctx = ensureOfficialProtocolAdapter(ctx, interfaceType)
	if strings.TrimSpace(interfaceType) == sparkCineProtocolID || strings.TrimSpace(interfaceType) == paipuVideoProtocolID {
		// These upstreams ship their own asset ingest, which the channel's own
		// key drives. The configured provider remains only the fallback, so
		// disabling it must not bring back the manual HTTPS-link dialog.
		return mediahost.Decision{
			Host:   true,
			Reason: "protocol_requires_url_upstream_upload",
		}, nil
	}
	if a.store == nil {
		return mediahost.Decision{}, nil
	}
	config, err := a.store.Read()
	if err != nil {
		return mediahost.Decision{}, err
	}
	// A per-channel override can turn hosting on or off independently of the
	// global setting.
	scoped := config.ResolveForChannel(channelID)
	if !scoped.Enabled() {
		return mediahost.Decision{Reason: "hosting_disabled"}, nil
	}
	// The plugin manifest declares whether the protocol needs a public URL.
	// Reading it here keeps upstream parameter differences authoritative.
	requiresPublicURL := false
	if adapter, ok := protocolAdapterForContext(ctx, interfaceType); ok {
		requiresPublicURL = adapter.Metadata().RequiresPublicMediaURLs
	}
	contract := mediahost.ProtocolContractFor(interfaceType, requiresPublicURL)
	// 视频协议不代表参考素材就是视频：OpenVideo 这类接口的参考素材通常是图片
	// 或音频，而真正上传时会按素材自身的 MIME 判断 kind。这里不能用协议名猜出来
	// 的 kind 去卡图床能力，否则会把「免费图床不支持视频」误判成「连图片也不能
	// 托管」，用户就会看到要求手填 HTTPS 链接的弹窗。
	if contract.RequiresPublicURL {
		for _, candidate := range []mediahost.MediaKind{mediahost.MediaKindImage, mediahost.MediaKindAudio, mediahost.MediaKindVideo} {
			if decision := mediahost.Decide(scoped, contract, candidate, true); decision.Host {
				return decision, nil
			}
		}
	}
	kind := mediahost.KindForChannelInterface(interfaceType)
	return mediahost.Decide(scoped, contract, kind, true), nil
}

// Protocol IDs whose upstream ships its own reference-media ingest. For these
// the channel's existing credentials upload the bytes, so reference media works
// with no separate image-host configuration at all.
const (
	sparkCineProtocolID  = "sparkcine-h3-video"
	paipuVideoProtocolID = "paipu-video"
)

// UploadForGeneration tries a protocol-owned ingest first, then the user's
// configured fallback host. The method intentionally lives in the app layer:
// generation only knows the narrow optional interface, while configuration
// and credentials stay here.
func (a *mediaHostingAdapter) UploadForGeneration(ctx context.Context, config generation.Config, kind mediahost.MediaKind, filename, contentType string, body []byte) (string, error) {
	if a == nil {
		return "", mediahost.ErrDisabled
	}
	switch strings.TrimSpace(config.InterfaceType) {
	case sparkCineProtocolID:
		return a.uploadForSparkCine(ctx, config, kind, filename, contentType, body)
	case paipuVideoProtocolID:
		return a.uploadForPaipu(ctx, config, kind, filename, contentType, body)
	}
	if a.store == nil {
		return "", mediahost.ErrDisabled
	}
	return a.Upload(ctx, kind, filename, contentType, body)
}

func (a *mediaHostingAdapter) uploadForSparkCine(ctx context.Context, config generation.Config, kind mediahost.MediaKind, filename, contentType string, body []byte) (string, error) {
	hash := mediahost.HashContent(body)
	if a.cache != nil {
		if cached, ok := a.cache.Lookup("sparkcine:" + hash); ok && cached.AssetID != "" {
			if link, err := sparkCineAssetURL(ctx, config, cached.AssetID); err == nil {
				log.Printf("[图床] SparkCine 命中缓存 %s", hash[:8])
				return link, nil
			}
			_ = a.cache.Forget("sparkcine:" + hash)
		}
	}

	started := time.Now()
	asset, officialErr := uploadSparkCineAsset(ctx, config, kind, filename, contentType, body)
	if officialErr == nil {
		log.Printf("[图床] SparkCine 官方素材上传成功 kind=%s (%dms)", kind, time.Since(started).Milliseconds())
		// complete 响应有时只回 asset_id 而没有可访问 URL；此时用详情接口
		// 补取公网地址，避免把空 URL 发给上游导致参数校验失败。
		if strings.TrimSpace(asset.URL) == "" && strings.TrimSpace(asset.ID) != "" {
			if link, err := sparkCineAssetURL(ctx, config, asset.ID); err == nil && strings.TrimSpace(link) != "" {
				asset.URL = link
			}
		}
		if strings.TrimSpace(asset.URL) == "" {
			return "", fmt.Errorf("SparkCine 官方上传成功但未返回可访问的素材地址")
		}
		if a.cache != nil {
			_ = a.cache.Store("sparkcine:"+hash, mediahost.CacheEntry{
				AssetID:  asset.ID,
				Provider: "sparkcine",
				Kind:     string(kind),
				Bytes:    int64(len(body)),
			})
		}
		return asset.URL, nil
	}
	safeOfficialErr := redactSparkCineError(officialErr)
	log.Printf("[图床] SparkCine 官方素材上传失败 kind=%s: %s；回退配置图床", kind, safeOfficialErr)

	if a.store == nil {
		return "", fmt.Errorf("SparkCine 官方上传失败：%w；未配置备用图床", safeOfficialErr)
	}
	workspaceConfig, err := a.store.Read()
	if err != nil {
		return "", fmt.Errorf("读取图床配置失败：%w（官方上传失败：%v）", err, safeOfficialErr)
	}
	scoped := workspaceConfig.ResolveForChannel(config.ChannelID)
	fallbackLink, fallbackErr := a.uploader.Upload(ctx, scoped, kind, filename, contentType, body)
	if fallbackErr != nil {
		return "", fmt.Errorf("SparkCine 官方上传失败：%v；备用图床上传失败：%w", safeOfficialErr, redactSparkCineError(fallbackErr))
	}
	log.Printf("[图床] SparkCine 备用图床上传成功 kind=%s", kind)
	return fallbackLink, nil
}

// paipuUploadPath is 派普's own free image host. It takes a multipart `file`
// field and answers {"url": "..."}; the returned link is public, so it can be
// sent straight back to paipu as a reference image.
const paipuUploadPath = "/v1/media/upload"

// uploadForPaipu hosts reference images on 派普's own upload endpoint using the
// channel's API key, then falls back to the workspace image host.
//
// This endpoint is an image host and nothing else: the docs describe it as
// "上传图片后获取可直接用于图片和视频生成任务的 HTTPS 图片 URL", and it rejects
// other kinds. Reference videos and audio are a separate matter — they are
// generation parameters (`POST /v1/videos` carries `videos[]` and `audios[]`),
// not uploads — so they are handed to the user's configured host rather than
// pushed at this endpoint.
func (a *mediaHostingAdapter) uploadForPaipu(ctx context.Context, config generation.Config, kind mediahost.MediaKind, filename, contentType string, body []byte) (string, error) {
	if kind != mediahost.MediaKindImage {
		return a.uploadPaipuFallback(ctx, config, kind, filename, contentType, body,
			fmt.Errorf("派普图床只接收图片，%s 参考素材不能上传到派普图床", kind))
	}

	hash := mediahost.HashContent(body)
	if a.cache != nil {
		if cached, ok := a.cache.Lookup("paipu:" + hash); ok && cached.URL != "" {
			if a.uploader.URLAlive(ctx, cached.URL) {
				log.Printf("[图床] 派普命中缓存 %s", hash[:8])
				return cached.URL, nil
			}
			_ = a.cache.Forget("paipu:" + hash)
		}
	}

	started := time.Now()
	link, officialErr := uploadPaipuMedia(ctx, config, kind, filename, contentType, body)
	if officialErr == nil {
		log.Printf("[图床] 派普官方素材上传成功 kind=%s (%dms)", kind, time.Since(started).Milliseconds())
		if a.cache != nil {
			_ = a.cache.Store("paipu:"+hash, mediahost.CacheEntry{
				URL:      link,
				Provider: "paipu",
				Kind:     string(kind),
				Bytes:    int64(len(body)),
			})
		}
		return link, nil
	}
	safeOfficialErr := redactUploadError(officialErr)
	log.Printf("[图床] 派普官方素材上传失败 kind=%s: %s；回退配置图床", kind, safeOfficialErr)
	return a.uploadPaipuFallback(ctx, config, kind, filename, contentType, body, safeOfficialErr)
}

// uploadPaipuFallback sends bytes that 派普's own host cannot take to the
// workspace image host, keeping the original cause in the message.
func (a *mediaHostingAdapter) uploadPaipuFallback(ctx context.Context, config generation.Config, kind mediahost.MediaKind, filename, contentType string, body []byte, cause error) (string, error) {
	safeCause := redactUploadError(cause)
	if a.store == nil {
		return "", fmt.Errorf("派普图床上传失败：%w；未配置备用图床", safeCause)
	}
	workspaceConfig, err := a.store.Read()
	if err != nil {
		return "", fmt.Errorf("读取图床配置失败：%w（派普上传失败：%v）", err, safeCause)
	}
	scoped := workspaceConfig.ResolveForChannel(config.ChannelID)
	fallbackLink, fallbackErr := a.uploader.Upload(ctx, scoped, kind, filename, contentType, body)
	if fallbackErr != nil {
		return "", fmt.Errorf("派普图床上传失败：%v；备用图床上传失败：%w", safeCause, redactUploadError(fallbackErr))
	}
	log.Printf("[图床] 派普备用图床上传成功 kind=%s", kind)
	return fallbackLink, nil
}

// uploadPaipuMedia posts one file to 派普's image host and returns the public
// URL from the response.
func uploadPaipuMedia(ctx context.Context, config generation.Config, kind mediahost.MediaKind, filename, contentType string, body []byte) (string, error) {
	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.APIKey) == "" {
		return "", errors.New("派普缺少 Base URL 或 API Key")
	}
	if filename == "" {
		filename = "beeftv-reference"
	}
	filename = filepath.Base(filename)
	if filepath.Ext(filename) == "" {
		filename += mediaExtensionForUpload(kind, contentType)
	}
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}

	form := &bytes.Buffer{}
	writer := multipart.NewWriter(form)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(body); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	target := generation.APIURL(config.BaseURL, paipuUploadPath)
	if _, err := outbound.ValidateOutboundURL(target); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, form)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+config.APIKey)
	outbound.ApplyOutboundHeaders(request, config.Headers)

	response, err := outbound.OutboundHTTPClient(5 * time.Minute).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d：%s", response.StatusCode, truncateMediaHosting(string(payload), 300))
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("返回内容不是 JSON：%w", err)
	}
	link := firstJSONPathURL(decoded, "url", "data.url", "public_url", "data.public_url", "download_url", "data.download_url")
	if link == "" {
		return "", errors.New("派普图床未返回可访问的 URL")
	}
	return validatePublicMediaURL(link)
}

// validatePublicMediaURL rejects a hosted link the upstream could not fetch:
// only credential-free HTTP(S) addresses are usable as reference media.
func validatePublicMediaURL(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return "", errors.New("图床返回的素材链接必须是无凭据的 HTTP(S) 地址")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("图床返回的素材链接必须是无凭据的 HTTP(S) 地址")
	}
	return parsed.String(), nil
}

// Upload stores media for the active workspace configuration. Callers that want
// a URL for arbitrary bytes (for example a settings probe) use this.
func (a *mediaHostingAdapter) Upload(ctx context.Context, kind mediahost.MediaKind, filename, contentType string, body []byte) (string, error) {
	if a == nil || a.store == nil {
		return "", mediahost.ErrDisabled
	}
	config, err := a.store.Read()
	if err != nil {
		return "", err
	}
	return a.uploader.Upload(ctx, config, kind, filename, contentType, body)
}

const sparkCineUploadPath = "/api/open/v1/uploads"

type sparkCineAsset struct {
	ID  string
	URL string
}

func uploadSparkCineAsset(ctx context.Context, config generation.Config, kind mediahost.MediaKind, filename, contentType string, body []byte) (sparkCineAsset, error) {
	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.APIKey) == "" {
		return sparkCineAsset{}, errors.New("SparkCine 缺少 Base URL 或 API Key")
	}
	if filename == "" {
		filename = "beeftv-reference"
	}
	filename = filepath.Base(filename)
	if filepath.Ext(filename) == "" {
		filename += mediaExtensionForUpload(kind, contentType)
	}
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	hash := sha256.Sum256(body)
	start, err := sparkCineJSON(ctx, config, http.MethodPost, sparkCineUploadPath, map[string]any{
		"filename":     filename,
		"content_type": contentType,
		"byte_length":  len(body),
		"sha256":       hex.EncodeToString(hash[:]),
	})
	if err != nil {
		return sparkCineAsset{}, fmt.Errorf("申请签名上传地址失败：%w", err)
	}
	assetID := firstJSONPathString(start, "asset_id", "id", "asset.id", "upload.asset_id", "data.asset_id", "data.id", "data.asset.id", "data.upload.asset_id")
	if assetID == "" {
		return sparkCineAsset{}, errors.New("官方上传未返回 asset_id")
	}
	// The official script accepts both {upload:{url}} and a flat {url}
	// ticket. A top-level url is the signed PUT target, not a public asset URL.
	uploadURL := firstJSONPathURL(start, "upload.url", "uploadUrl", "data.upload.url", "data.uploadUrl", "url", "data.url")
	if uploadURL == "" {
		if publicURL := firstJSONPathURL(start, "public_url", "download_url", "asset.url", "asset.public_url", "asset.download_url", "file.url", "file.download_url", "data.public_url", "data.download_url", "data.asset.url", "data.asset.public_url", "data.asset.download_url", "data.file.url", "data.file.download_url"); publicURL != "" {
			link, err := validateSparkCineAssetURL(publicURL)
			return sparkCineAsset{ID: assetID, URL: link}, err
		}
		link, assetErr := sparkCineAssetURL(ctx, config, assetID)
		if assetErr != nil {
			return sparkCineAsset{}, fmt.Errorf("官方上传未返回签名 PUT 地址，读取已存在资产失败：%w", assetErr)
		}
		return sparkCineAsset{ID: assetID, URL: link}, nil
	}
	parsed, err := outbound.ValidateOutboundURL(uploadURL)
	if err != nil {
		return sparkCineAsset{}, fmt.Errorf("签名 PUT 地址无效：%w", err)
	}
	headers := firstJSONPathMap(start, "upload.headers", "headers", "data.upload.headers", "data.headers")
	method := firstJSONPathString(start, "upload.method", "method", "data.upload.method", "data.method")
	if method == "" {
		method = http.MethodPut
	}
	if !strings.EqualFold(method, http.MethodPut) {
		return sparkCineAsset{}, fmt.Errorf("官方签名上传使用了不支持的方法 %q", method)
	}
	putRequest, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), parsed.String(), bytes.NewReader(body))
	if err != nil {
		return sparkCineAsset{}, err
	}
	for name, value := range headers {
		if text, ok := value.(string); ok && text != "" {
			putRequest.Header.Set(name, text)
		}
	}
	if putRequest.Header.Get("Content-Type") == "" {
		putRequest.Header.Set("Content-Type", contentType)
	}
	putResponse, err := outbound.OutboundHTTPClient(5 * time.Minute).Do(putRequest)
	if err != nil {
		return sparkCineAsset{}, fmt.Errorf("上传素材字节失败：%w", err)
	}
	putBody, readErr := io.ReadAll(io.LimitReader(putResponse.Body, 1<<20))
	putResponse.Body.Close()
	if readErr != nil {
		return sparkCineAsset{}, readErr
	}
	if putResponse.StatusCode < 200 || putResponse.StatusCode >= 300 {
		return sparkCineAsset{}, fmt.Errorf("上传素材字节返回 HTTP %d：%s", putResponse.StatusCode, truncateMediaHosting(string(putBody), 240))
	}

	complete, err := sparkCineJSON(ctx, config, http.MethodPost, sparkCineUploadPath+"/"+urlPathEscape(assetID)+"/complete", nil)
	if err != nil {
		return sparkCineAsset{}, fmt.Errorf("确认官方素材上传失败：%w", err)
	}
	if link := firstJSONPathURL(complete, "url", "public_url", "download_url", "asset.url", "asset.public_url", "asset.download_url", "file.url", "file.download_url", "data.url", "data.public_url", "data.download_url", "data.asset.url", "data.asset.public_url", "data.asset.download_url", "data.file.url", "data.file.download_url"); link != "" {
		publicURL, err := validateSparkCineAssetURL(link)
		return sparkCineAsset{ID: assetID, URL: publicURL}, err
	}

	link, err := sparkCineAssetURL(ctx, config, assetID)
	if err != nil {
		return sparkCineAsset{}, fmt.Errorf("读取官方素材地址失败：%w", err)
	}
	return sparkCineAsset{ID: assetID, URL: link}, nil
}

func sparkCineAssetURL(ctx context.Context, config generation.Config, assetID string) (string, error) {
	if strings.TrimSpace(assetID) == "" {
		return "", errors.New("SparkCine asset_id 为空")
	}
	asset, err := sparkCineJSON(ctx, config, http.MethodGet, "/api/open/v1/assets/"+urlPathEscape(assetID), nil)
	if err != nil {
		return "", err
	}
	link := firstJSONPathURL(asset, "url", "public_url", "download_url", "asset.url", "asset.public_url", "asset.download_url", "file.url", "file.download_url", "data.url", "data.public_url", "data.download_url", "data.asset.url", "data.asset.public_url", "data.asset.download_url", "data.file.url", "data.file.download_url")
	if link == "" {
		return "", errors.New("SparkCine 资产查询未返回可访问 URL")
	}
	return validateSparkCineAssetURL(link)
}

func validateSparkCineAssetURL(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("SparkCine 素材链接必须是无凭据的 HTTPS 地址")
	}
	return parsed.String(), nil
}

func sparkCineJSON(ctx context.Context, config generation.Config, method, path string, body any) (map[string]any, error) {
	target := generation.APIURL(config.BaseURL, path)
	if strings.HasPrefix(strings.TrimSpace(path), "/api/") {
		target = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/") + path
	}
	if _, err := outbound.ValidateOutboundURL(target); err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+config.APIKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	outbound.ApplyOutboundHeaders(request, config.Headers)
	response, err := outbound.OutboundHTTPClient(5 * time.Minute).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d：%s", response.StatusCode, truncateMediaHosting(string(payload), 400))
	}
	var decoded map[string]any
	if len(bytes.TrimSpace(payload)) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("返回内容不是 JSON：%w", err)
	}
	return decoded, nil
}

func firstJSONPathString(root map[string]any, paths ...string) string {
	for _, path := range paths {
		value, ok := lookupJSONPath(root, path)
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
	return ""
}

func firstJSONPathURL(root map[string]any, paths ...string) string {
	for _, path := range paths {
		value, ok := lookupJSONPath(root, path)
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if strings.HasPrefix(strings.ToLower(text), "https://") || strings.HasPrefix(strings.ToLower(text), "http://") {
			return text
		}
	}
	return ""
}

func firstJSONPathMap(root map[string]any, paths ...string) map[string]any {
	for _, path := range paths {
		value, ok := lookupJSONPath(root, path)
		if !ok {
			continue
		}
		typed, _ := value.(map[string]any)
		if typed != nil {
			return typed
		}
	}
	return nil
}

func lookupJSONPath(root map[string]any, dotted string) (any, bool) {
	var current any = root
	for _, part := range strings.Split(dotted, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func urlPathEscape(value string) string {
	return url.PathEscape(value)
}

func truncateMediaHosting(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func redactSparkCineLogText(value string) string {
	return redactSparkCineURLPattern.ReplaceAllString(value, "[URL]")
}

func redactSparkCineError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(redactSparkCineLogText(err.Error()))
}

// redactUploadError strips URLs from an upload error. Hosted links are signed
// and single-use, so they must never reach a log or the browser.
func redactUploadError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(redactSparkCineLogText(err.Error()))
}

var redactSparkCineURLPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

func mediaExtensionForUpload(kind mediahost.MediaKind, contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/mp4":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "video/mp4":
		return ".mp4"
	}
	switch kind {
	case mediahost.MediaKindAudio:
		return ".mp3"
	case mediahost.MediaKindVideo:
		return ".mp4"
	default:
		return ".png"
	}
}

package generation

import (
	"net/url"
	"strings"

	"infinite-canvas/backend/internal/model"
)

func APIURL(baseURL string, path string) string {
	return apiURLWithDefaultPrefix(baseURL, path, "/v1")
}

func ChannelAPIURL(baseURL string, path string) string {
	return APIURL(baseURL, path)
}

// ChannelAPIURLForProtocol 把协议默认版本收敛在传输边界：Gemini 默认 v1beta，
// OpenAI 兼容协议默认 v1；baseURL 或 path 中显式出现的版本始终优先。
func ChannelAPIURLForProtocol(baseURL string, path string, interfaceType model.ChannelInterfaceType) string {
	if interfaceType == model.ChannelInterfaceAgnesVideo && strings.HasPrefix(strings.TrimSpace(path), "/agnesapi") {
		base, err := url.Parse(strings.TrimSpace(baseURL))
		requestPath, pathErr := url.Parse(strings.TrimSpace(path))
		if err == nil && pathErr == nil && base.Scheme != "" && base.Host != "" && strings.HasPrefix(requestPath.Path, "/") {
			base.Path = requestPath.Path
			base.RawPath = requestPath.RawPath
			base.RawQuery = requestPath.RawQuery
			base.Fragment = ""
			return base.String()
		}
	}
	defaultPrefix := "/v1"
	if interfaceType == model.ChannelInterfaceGeminiVeo || interfaceType == model.ChannelInterfaceGeminiImage {
		defaultPrefix = "/v1beta"
	}
	return apiURLWithDefaultPrefix(baseURL, path, defaultPrefix)
}

var channelAPIPrefixes = []string{"/api/plan/v3", "/api/v3", "/api/v1", "/v1beta", "/v1", "/v2", "/v3"}

func apiURLWithDefaultPrefix(baseURL string, path string, defaultPrefix string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	requestPath := strings.TrimSpace(path)
	if requestPath == "" {
		return base
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}

	requestPrefix := requestAPIPathPrefix(requestPath)
	basePrefix := baseAPIPathPrefix(base)
	if requestPrefix != "" {
		if basePrefix == requestPrefix {
			return base + strings.TrimPrefix(requestPath, requestPrefix)
		}
		// base 已经指向服务商自定义的 /api/<segment> 前缀时，path 里的版本段是
		// 该前缀下的子路径，不能再拿它去替换 base 前缀。
		if strings.HasPrefix(basePrefix, "/api/") && !strings.HasPrefix(requestPrefix, "/api/") {
			return base + requestPath
		}
		return strings.TrimSuffix(base, basePrefix) + requestPath
	}
	if basePrefix != "" {
		return base + requestPath
	}
	return base + defaultPrefix + requestPath
}

func requestAPIPathPrefix(value string) string {
	lower := strings.ToLower(value)
	for _, prefix := range channelAPIPrefixes {
		if lower == prefix || strings.HasPrefix(lower, prefix+"/") || strings.HasPrefix(lower, prefix+"?") || strings.HasPrefix(lower, prefix+"#") {
			return prefix
		}
	}
	// 服务商可以把整套 API 挂在自定义前缀下（例如 SparkCine 的 /api/h3）。
	// 这类路径在 path 里已是完整绝对路径，不能再注入默认版本前缀，否则会
	// 变成 /v1/api/h3/... 而打到不存在的路由。
	if strings.HasPrefix(lower, "/api/") {
		segments := strings.SplitN(strings.TrimPrefix(lower, "/api/"), "/", 2)
		if len(segments) > 0 && segments[0] != "" {
			return "/api/" + segments[0]
		}
	}
	return ""
}

func baseAPIPathPrefix(value string) string {
	lower := strings.ToLower(strings.TrimRight(value, "/"))
	for _, prefix := range channelAPIPrefixes {
		if lower == prefix || strings.HasSuffix(lower, prefix) {
			return prefix
		}
	}
	if index := strings.Index(lower, "/api/"); index >= 0 {
		segments := strings.SplitN(lower[index+len("/api/"):], "/", 2)
		if len(segments) > 0 && segments[0] != "" {
			return "/api/" + segments[0]
		}
	}
	return ""
}

func GeminiURL(baseURL string, path string) string {
	return apiURLWithDefaultPrefix(baseURL, path, "/v1beta")
}

func ProviderDownloadURL(baseURL string, rawURL string) string {
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	target, targetErr := url.Parse(strings.TrimSpace(rawURL))
	// A completed task may return a root-relative authenticated media endpoint.
	// Resolve only a single-slash path, never a scheme-relative external host or
	// an arbitrary status string, and leave network validation to the transport.
	if baseErr == nil && targetErr == nil && base.Host != "" && base.User == nil && (base.Scheme == "https" || base.Scheme == "http") && target.Scheme == "" && target.Host == "" && strings.HasPrefix(target.Path, "/") && !strings.HasPrefix(strings.TrimSpace(rawURL), "//") && target.Fragment == "" && !strings.Contains(target.Path, "\\") {
		return base.ResolveReference(target).String()
	}
	if baseErr != nil || targetErr != nil || !strings.EqualFold(base.Scheme, "https") || !strings.EqualFold(target.Scheme, "https") {
		return rawURL
	}
	if isBeefAPIHost(base.Hostname()) && isBeefAPIHost(target.Hostname()) {
		target.Scheme = base.Scheme
		target.Host = base.Host
		return target.String()
	}
	return rawURL
}

func IsBeefAPIHost(host string) bool {
	return isBeefAPIHost(host)
}

func isBeefAPIHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "beefapi.com" || strings.HasSuffix(host, ".beefapi.com")
}

func SameProviderOrigin(baseURL string, rawURL string) bool {
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	target, targetErr := url.Parse(strings.TrimSpace(rawURL))
	if baseErr != nil || targetErr != nil || base.Scheme == "" || base.Host == "" || target.Scheme == "" || target.Host == "" {
		return false
	}
	return strings.EqualFold(base.Scheme, target.Scheme) && strings.EqualFold(base.Host, target.Host)
}

// Package mediahost turns locally stored reference media into a URL that an
// upstream model provider can fetch.
//
// Most providers accept reference media as an inline payload, but several only
// accept a public URL (video models in particular). This package owns that
// decision plus the upload path behind it, so callers ask one question:
// "does this request need hosted media, and if so, what URL do I send?"
package mediahost

import (
	"errors"
	"fmt"
	"strings"
)

// Provider identifies where hosted media is stored.
type Provider string

const (
	// ProviderNone keeps every reference inline. This is the default so an
	// existing workspace behaves exactly as before the feature existed.
	ProviderNone Provider = "none"
	// ProviderBuiltin serves media from this BeefTV instance through the
	// signed public resource route. It requires CANVAS_PUBLIC_BASE_URL.
	ProviderBuiltin Provider = "builtin"
	// ProviderFree uploads to a public free image host.
	ProviderFree Provider = "free"
	// ProviderCustom uploads to a user-supplied multipart endpoint.
	ProviderCustom Provider = "custom"
	// ProviderAliyunOSS, ProviderTencentCOS, ProviderCloudflareR2 and
	// ProviderAWSS3 upload to object storage.
	ProviderAliyunOSS    Provider = "aliyun-oss"
	ProviderTencentCOS   Provider = "tencent-cos"
	ProviderCloudflareR2 Provider = "cloudflare-r2"
	ProviderAWSS3        Provider = "aws-s3"
)

// FreeProvider names a public image host used when Provider is ProviderFree.
type FreeProvider string

const (
	FreeProviderAuto      FreeProvider = "auto"
	FreeProviderLitterbox FreeProvider = "litterbox"
	FreeProviderUguu      FreeProvider = "uguu"
	FreeProviderImgBB     FreeProvider = "imgbb"
)

// MediaKind is the kind of reference media being hosted.
type MediaKind string

const (
	MediaKindImage MediaKind = "image"
	MediaKindVideo MediaKind = "video"
	MediaKindAudio MediaKind = "audio"
)

var (
	ErrDisabled          = errors.New("素材图床未启用")
	ErrKindUnsupported   = errors.New("当前图床不支持这种素材类型")
	ErrSourceUnreadable  = errors.New("读取待上传素材失败")
	ErrPublicBaseMissing = errors.New("内置图床需要配置服务器公开访问地址（CANVAS_PUBLIC_BASE_URL）")
)

// Config is the persisted media-hosting configuration. Secret fields are
// encrypted at rest by the workspace store and never returned to the browser
// in plaintext.
type Config struct {
	Provider Provider `json:"provider"`

	/**
	 * ChannelOverrides lets an individual channel use a different provider, or
	 * opt out of hosting entirely. A channel without an entry follows the
	 * global configuration above.
	 */
	ChannelOverrides map[string]ChannelOverride `json:"channelOverrides,omitempty"`

	FreeProvider FreeProvider `json:"freeProvider,omitempty"`
	FreeExpiry   string       `json:"freeExpiry,omitempty"`
	ImgBBApiKey  string       `json:"imgbbApiKey,omitempty"`

	Endpoint         string `json:"endpoint,omitempty"`
	CustomFileField  string `json:"customFileField,omitempty"`
	CustomURLPath    string `json:"customUrlPath,omitempty"`
	CustomAuthHeader string `json:"customAuthHeader,omitempty"`
	CustomAuthScheme string `json:"customAuthScheme,omitempty"`
	CustomToken      string `json:"customToken,omitempty"`

	Bucket          string `json:"bucket,omitempty"`
	Region          string `json:"region,omitempty"`
	AccountID       string `json:"accountId,omitempty"`
	AccessKeyID     string `json:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	SessionToken    string `json:"sessionToken,omitempty"`
	PublicBaseURL   string `json:"publicBaseUrl,omitempty"`
	PathPrefix      string `json:"pathPrefix,omitempty"`
}

// ChannelOverride is a per-channel media-hosting setting.
type ChannelOverride struct {
	// Mode is "inherit", "off" or "custom".
	Mode string `json:"mode"`
	// Provider, when Mode is "custom", replaces the global provider. The
	// remaining fields are merged onto the global configuration so a channel
	// only has to state what differs.
	Provider         string `json:"provider,omitempty"`
	Endpoint         string `json:"endpoint,omitempty"`
	CustomFileField  string `json:"customFileField,omitempty"`
	CustomURLPath    string `json:"customUrlPath,omitempty"`
	CustomAuthHeader string `json:"customAuthHeader,omitempty"`
	CustomAuthScheme string `json:"customAuthScheme,omitempty"`
}

// ResolveForChannel returns the effective configuration for a channel.
//
// An unset or "inherit" override yields the global configuration. "off"
// disables hosting for that channel. "custom" applies the override on top of
// the global configuration, so secrets and unrelated fields still come from
// the shared settings.
func (c Config) ResolveForChannel(channelID string) Config {
	if len(c.ChannelOverrides) == 0 {
		return Normalize(c)
	}
	override, ok := c.ChannelOverrides[channelID]
	if !ok {
		return Normalize(c)
	}
	switch override.Mode {
	case "off":
		disabled := Normalize(c)
		disabled.Provider = ProviderNone
		return disabled
	case "custom":
		scoped := Normalize(c)
		if override.Provider != "" {
			scoped.Provider = Normalize(Config{Provider: Provider(override.Provider)}).Provider
		}
		if override.Endpoint != "" {
			scoped.Endpoint = override.Endpoint
		}
		if override.CustomFileField != "" {
			scoped.CustomFileField = override.CustomFileField
		}
		if override.CustomURLPath != "" {
			scoped.CustomURLPath = override.CustomURLPath
		}
		if override.CustomAuthHeader != "" {
			scoped.CustomAuthHeader = override.CustomAuthHeader
		}
		if override.CustomAuthScheme != "" {
			scoped.CustomAuthScheme = override.CustomAuthScheme
		}
		// A channel that switches provider must not reuse the global
		// credential, which belongs to a different service.
		if override.Provider != "" && Provider(override.Provider) != Normalize(c).Provider {
			scoped.ImgBBApiKey = ""
			scoped.CustomToken = ""
			scoped.AccessKeyID = ""
			scoped.SecretAccessKey = ""
			scoped.SessionToken = ""
		}
		return Normalize(scoped)
	default:
		return Normalize(c)
	}
}

// DefaultConfig keeps hosting off, so an upgrade never changes how an existing
// channel sends reference media.
func DefaultConfig() Config {
	return Config{
		Provider:         ProviderNone,
		FreeProvider:     FreeProviderAuto,
		FreeExpiry:       "24h",
		CustomFileField:  "file",
		CustomAuthHeader: "Authorization",
		CustomAuthScheme: "Bearer",
		PathPrefix:       "beeftv",
	}
}

// Normalize fills defaults and drops values that are not selectable, so a
// hand-edited or legacy config file cannot put the uploader into an undefined
// state.
func Normalize(config Config) Config {
	fallback := DefaultConfig()

	switch config.Provider {
	case ProviderNone, ProviderBuiltin, ProviderFree, ProviderCustom,
		ProviderAliyunOSS, ProviderTencentCOS, ProviderCloudflareR2, ProviderAWSS3:
	default:
		config.Provider = fallback.Provider
	}

	switch config.FreeProvider {
	case FreeProviderAuto, FreeProviderLitterbox, FreeProviderUguu, FreeProviderImgBB:
	default:
		config.FreeProvider = fallback.FreeProvider
	}

	switch config.FreeExpiry {
	case "1h", "12h", "24h", "72h":
	default:
		config.FreeExpiry = fallback.FreeExpiry
	}

	config.Endpoint = strings.TrimSpace(config.Endpoint)
	config.CustomFileField = strings.TrimSpace(config.CustomFileField)
	if config.CustomFileField == "" {
		config.CustomFileField = fallback.CustomFileField
	}
	config.CustomURLPath = strings.TrimSpace(config.CustomURLPath)
	config.CustomAuthHeader = strings.TrimSpace(config.CustomAuthHeader)
	if config.CustomAuthHeader == "" {
		config.CustomAuthHeader = fallback.CustomAuthHeader
	}
	config.CustomAuthScheme = strings.TrimSpace(config.CustomAuthScheme)

	config.Bucket = strings.TrimSpace(config.Bucket)
	config.Region = strings.TrimSpace(config.Region)
	config.AccountID = strings.TrimSpace(config.AccountID)
	config.PublicBaseURL = strings.TrimRight(strings.TrimSpace(config.PublicBaseURL), "/")
	config.PathPrefix = strings.Trim(strings.TrimSpace(config.PathPrefix), "/")
	if config.PathPrefix == "" {
		config.PathPrefix = fallback.PathPrefix
	}
	return config
}

// Enabled reports whether reference media should be routed through hosting.
func (c Config) Enabled() bool {
	return Normalize(c).Provider != ProviderNone
}

// SupportsKind reports whether the selected provider can carry this media
// kind.
//
// Free public hosts differ: a live probe showed uguu accepting both images and
// audio while litterbox rejected audio with "No file!". Because availability
// shifts over time, free hosting is allowed for image and audio and the upload
// path reports a real error if the chosen host refuses. Video stays excluded
// since no free host here accepts it.
func (c Config) SupportsKind(kind MediaKind) bool {
	normalized := Normalize(c)
	switch normalized.Provider {
	case ProviderNone:
		return false
	case ProviderFree:
		return kind == MediaKindImage || kind == MediaKindAudio
	case ProviderBuiltin, ProviderCustom, ProviderAliyunOSS, ProviderTencentCOS,
		ProviderCloudflareR2, ProviderAWSS3:
		switch kind {
		case MediaKindImage, MediaKindVideo, MediaKindAudio:
			return true
		}
		return false
	default:
		return false
	}
}

// Validate reports a user-actionable problem with the configuration, or nil
// when the selected provider has everything it needs to upload.
func Validate(config Config) error {
	normalized := Normalize(config)
	switch normalized.Provider {
	case ProviderNone:
		return nil
	case ProviderBuiltin:
		// The public base URL is environment configuration, not part of the
		// stored config, so it is verified at upload time.
		return nil
	case ProviderFree:
		if normalized.FreeProvider == FreeProviderImgBB && strings.TrimSpace(normalized.ImgBBApiKey) == "" {
			return errors.New("ImgBB 需要填写 API Key")
		}
		return nil
	case ProviderCustom:
		if normalized.Endpoint == "" {
			return errors.New("自定义图床需要填写上传地址")
		}
		return nil
	case ProviderAliyunOSS, ProviderTencentCOS, ProviderCloudflareR2, ProviderAWSS3:
		if normalized.Bucket == "" {
			return errors.New("对象存储图床需要填写存储桶")
		}
		if normalized.AccessKeyID == "" || normalized.SecretAccessKey == "" {
			return errors.New("对象存储图床需要填写 Access Key")
		}
		if normalized.PublicBaseURL == "" {
			return errors.New("对象存储图床需要填写公开访问地址")
		}
		if (normalized.Provider == ProviderCloudflareR2 || normalized.Provider == ProviderAWSS3) && normalized.Region == "" && normalized.AccountID == "" {
			return errors.New("该对象存储需要填写区域或账号 ID")
		}
		return nil
	default:
		return fmt.Errorf("未知图床类型 %q", normalized.Provider)
	}
}

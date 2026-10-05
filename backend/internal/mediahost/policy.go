package mediahost

import (
	"strings"
)

// ProtocolMediaContract describes how one upstream protocol accepts reference
// media. The values come from the protocol plugin's manifest, which is the
// single source of truth for upstream parameter differences: field naming,
// whether a public URL is required, and whether inline bytes are accepted.
type ProtocolMediaContract struct {
	// RequiresPublicURL mirrors manifest.requiresPublicMediaUrls. When true the
	// upstream only accepts a fetchable URL, so hosting is mandatory.
	RequiresPublicURL bool
	// PrefersURL lists protocols whose request template exposes a URL field.
	// These work with hosting but still accept inline bytes.
	PrefersURL bool
	// AcceptsInlineBytes is true when the protocol can carry base64/data URLs.
	// Protocols that require a public URL set this to false.
	AcceptsInlineBytes bool
}

// urlFieldProtocols mirrors generation.PrefersMediaURLs. It is duplicated
// deliberately: mediahost must answer "can hosting help here?" without
// importing the generation package, which would create a cycle.
var urlFieldProtocols = map[string]bool{
	"chat-completion":                 true,
	"openai-response":                 true,
	"claude-api":                      true,
	"grok-image":                      true,
	"volcengine-ark-image":            true,
	"volcengine-ark-agent-plan-image": true,
	"xai-video":                       true,
	"novita-video":                    true,
	"minimax-video":                   true,
	"newapi":                          true,
	"newapi-channel-1":                true,
	"newapi-channel-2":                true,
	"volcengine-ark-video":            true,
	"volcengine-ark-agent-plan-video": true,
}

// urlOnlyProtocols require a fetchable URL and reject inline media.
var urlOnlyProtocols = map[string]bool{
	"agnes-video":        true,
	"agnes-video-v20":    true,
	"autodl-comfyui":     true,
	"autodl-comfyui-audio": true,
	"dashscope-wan-video":  true,
	"dashscope-wanx-image": true,
	"minimax-video":        true,
	"newapi-channel-1":     true,
	"newapi-channel-2":     true,
	"novita-video":         true,
	"rolldek-wan-video":    true,
	"xai-video":            true,
}

// ProtocolContractFor derives the media contract for a protocol identifier.
// requiresPublicMediaURLs comes from the loaded plugin manifest when available;
// the built-in tables cover protocols that ship without a manifest entry.
func ProtocolContractFor(interfaceType string, requiresPublicMediaURLs bool) ProtocolMediaContract {
	name := strings.TrimSpace(interfaceType)
	requiresURL := requiresPublicMediaURLs || urlOnlyProtocols[name]
	return ProtocolMediaContract{
		RequiresPublicURL:  requiresURL,
		PrefersURL:         requiresURL || urlFieldProtocols[name],
		AcceptsInlineBytes: !requiresURL,
	}
}

// Decision explains why hosting was or was not applied to a request.
type Decision struct {
	// Host reports whether reference media should be uploaded and replaced
	// with a public URL.
	Host bool
	// UsesBuiltin reports that the builtin provider is active. Callers use the
	// signed-URL path instead of uploading a copy.
	UsesBuiltin bool
	// Reason is a short machine-readable explanation, useful in diagnostics.
	Reason string
}

// Decide answers whether reference media for this request should go through
// hosting.
//
// Hosting is applied only when it can help: the user enabled it, the provider
// supports the media kind, and the protocol either demands a URL or exposes a
// URL field. A protocol that only accepts inline bytes (for example Gemini's
// inline_data) is left alone so enabling hosting cannot break it.
func Decide(config Config, contract ProtocolMediaContract, kind MediaKind, hasMedia bool) Decision {
	normalized := Normalize(config)
	if !hasMedia {
		return Decision{Host: false, Reason: "no_reference_media"}
	}
	if normalized.Provider == ProviderNone {
		return Decision{Host: false, Reason: "hosting_disabled"}
	}
	if contract.RequiresPublicURL && !normalized.SupportsKind(kind) {
		// The protocol cannot proceed with inline bytes, so surfacing the
		// mismatch is more useful than silently sending nothing.
		return Decision{Host: false, Reason: "provider_cannot_carry_kind"}
	}
	if !contract.PrefersURL && !contract.RequiresPublicURL {
		return Decision{Host: false, Reason: "protocol_expects_inline"}
	}
	if !normalized.SupportsKind(kind) {
		// Protocol would accept a URL, but this provider cannot host the kind;
		// fall back to inline bytes rather than failing the request.
		return Decision{Host: false, Reason: "provider_cannot_carry_kind_fallback_inline"}
	}
	builtin := normalized.Provider == ProviderBuiltin
	if contract.RequiresPublicURL {
		return Decision{Host: true, UsesBuiltin: builtin, Reason: "protocol_requires_url"}
	}
	return Decision{Host: true, UsesBuiltin: builtin, Reason: "hosting_enabled_for_url_protocol"}
}

// KindForChannelInterface reports which media kind a protocol primarily deals
// with, so callers can ask SupportsKind without inspecting the request.
func KindForChannelInterface(interfaceType string) MediaKind {
	name := strings.TrimSpace(interfaceType)
	switch {
	case strings.Contains(name, "audio"):
		return MediaKindAudio
	case strings.Contains(name, "video"), name == "newapi", strings.HasPrefix(name, "newapi-channel"):
		return MediaKindVideo
	case name == "full-video":
		return MediaKindVideo
	default:
		return MediaKindImage
	}
}

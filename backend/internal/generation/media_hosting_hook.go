package generation

import (
	"context"

	"infinite-canvas/backend/internal/mediahost"
)

// MediaHostingProvider supplies the media-hosting decision without making the
// generation package depend on storage details. It is installed by the
// application wiring; a nil provider keeps upstream behaviour exactly.
type MediaHostingProvider interface {
	// HostingPolicy reports whether reference media for this request should be
	// served from a public URL, and the contract describing the protocol.
	// channelID lets a per-channel override win over the global setting.
	HostingPolicy(ctx context.Context, channelID string, interfaceType string) (mediahost.Decision, error)
}

// MediaHostingUploader is an optional extension implemented by the app layer.
// It lets a protocol-owned ingest path run before the configured fallback
// image host without making generation depend on storage/config details.
type MediaHostingUploader interface {
	UploadForGeneration(ctx context.Context, config Config, kind mediahost.MediaKind, filename, contentType string, body []byte) (string, error)
}

var mediaHostingProvider MediaHostingProvider

// SetMediaHostingProvider installs the media-hosting decision source. The
// application calls this once during startup; tests may leave it unset.
func SetMediaHostingProvider(provider MediaHostingProvider) {
	mediaHostingProvider = provider
}

// MediaHydrationPolicyFor resolves the policy with media hosting applied.
//
// The builtin provider hands the request to the signed-URL path, which does not
// require another copy of the media; every other provider must upload first, so
// it uses the URL-required path. Keeping the choice here means the executor
// calls one function regardless of provider.
func MediaHydrationPolicyForHostedGeneration(ctx context.Context, input Input) MediaHydrationPolicy {
	policy := MediaHydrationPolicyFor(ctx, input)
	if mediaHostingProvider == nil {
		return policy
	}
	decision, err := mediaHostingProvider.HostingPolicy(ctx, input.Config.ChannelID, input.Config.InterfaceType)
	if err != nil || !decision.Host {
		return policy
	}
	if decision.UsesBuiltin {
		policy := MediaHydrationPolicyWithBuiltinHosting(ctx, input)
		config := input.Config
		policy.UploadConfig = &config
		return policy
	}
	policy = MediaHydrationPolicyWithHosting(ctx, input)
	config := input.Config
	policy.UploadConfig = &config
	return policy
}

// MediaHydrationPolicyWithHosting layers the media-hosting decision on top of
// the upstream policy.
//
// It exists as a separate function on purpose: the upstream implementation of
// MediaHydrationPolicyFor stays untouched, so syncing from upstream never
// conflicts with media hosting. When hosting is off, or the provider declines,
// the upstream policy is returned unchanged.
func MediaHydrationPolicyWithHosting(ctx context.Context, input Input) MediaHydrationPolicy {
	policy := MediaHydrationPolicyFor(ctx, input)
	if mediaHostingProvider == nil {
		return policy
	}
	decision, err := mediaHostingProvider.HostingPolicy(ctx, input.Config.ChannelID, input.Config.InterfaceType)
	if err != nil || !decision.Host {
		return policy
	}
	// A masked image edit is a byte-level operation: the upstream policy clears
	// these flags for that case, and hosting must not reintroduce them.
	if input.Mode == "image" && input.Mask != nil {
		return policy
	}
	// The provider only reports Host when the protocol can carry a URL, so
	// promoting PreferURL to RequireURL is safe here.
	policy.RequireURL = true
	policy.PreferURL = true
	policy.CanHydrateLocal = true
	config := input.Config
	policy.UploadConfig = &config
	return policy
}

// MediaHydrationPolicyWithBuiltinHosting is the builtin-provider variant.
//
// The builtin provider does not copy media anywhere: it serves the existing
// local resource through a signed HTTPS URL. That is exactly what PreferHTTPS
// expresses, and unlike RequireURL it stays valid in a local workspace, where
// HydrateOne rejects RequireURL outright.
func MediaHydrationPolicyWithBuiltinHosting(ctx context.Context, input Input) MediaHydrationPolicy {
	policy := MediaHydrationPolicyFor(ctx, input)
	if mediaHostingProvider == nil {
		return policy
	}
	decision, err := mediaHostingProvider.HostingPolicy(ctx, input.Config.ChannelID, input.Config.InterfaceType)
	if err != nil || !decision.Host {
		return policy
	}
	if input.Mode == "image" && input.Mask != nil {
		return policy
	}
	policy.PreferHTTPS = true
	// The signed link replaces inline bytes, so keep the URL preference on
	// without demanding it; a workspace without a public base URL still falls
	// back to inline media instead of failing the request.
	policy.PreferURL = true
	return policy
}

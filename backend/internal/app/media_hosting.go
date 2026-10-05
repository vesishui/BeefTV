package app

import (
	"context"
	"log"
	"sync"

	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/mediahost"
)

// mediaHostingAdapter answers the generation layer's media-hosting question by
// reading the workspace media-hosting configuration.
//
// The configuration is re-read per request rather than cached at startup so a
// settings change takes effect on the next generation without a restart. The
// file is small and the read is cheap; misses fall back to upstream behaviour.
type mediaHostingAdapter struct {
	dataDir string

	mu      sync.Mutex
	store   *mediahost.Store
	uploader *mediahost.Uploader
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
	return &mediaHostingAdapter{dataDir: dataDir, store: store, uploader: uploader}, nil
}

// HostingPolicy implements generation.MediaHostingProvider.
func (a *mediaHostingAdapter) HostingPolicy(ctx context.Context, channelID string, interfaceType string) (mediahost.Decision, error) {
	if a == nil || a.store == nil {
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
	kind := mediahost.KindForChannelInterface(interfaceType)
	return mediahost.Decide(scoped, contract, kind, true), nil
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

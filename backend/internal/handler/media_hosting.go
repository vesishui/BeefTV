package handler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/mediahost"

	"github.com/gin-gonic/gin"
)

// maxTestUploadBytes bounds the probe endpoint so a mis-filled form cannot
// push an arbitrary payload into memory.
const maxTestUploadBytes = 32 * 1024 * 1024

// RegisterMediaHostingRoutes exposes the media-hosting configuration and a
// probe upload. The workspace scope supplies the data directory, so the config
// lives beside the rest of the local workspace state.
func RegisterMediaHostingRoutes(r *gin.RouterGroup) {
	r.GET("/workspace/media-hosting", func(c *gin.Context) {
		scope, err := CurrentWorkspace(c)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		store, err := mediahost.NewStore(scope.DataDir)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		config, err := store.Read()
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		cache, _ := mediahost.NewCache(scope.DataDir)
		ok(c, gin.H{
			"config":      mediahost.Redact(config),
			"cachedCount": cache.Len(),
			"enabled":     config.Enabled(),
		})
	})

	r.PUT("/workspace/media-hosting", func(c *gin.Context) {
		scope, err := CurrentWorkspace(c)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		var body struct {
			Config mediahost.Config `json:"config"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		store, err := mediahost.NewStore(scope.DataDir)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		existing, err := store.Read()
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		// The browser receives redacted secrets, so a returned placeholder must
		// not overwrite the stored key.
		merged := mediahost.MergeRedactedSecrets(body.Config, existing)
		if err := mediahost.Validate(merged); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		if err := store.Write(merged); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		ok(c, gin.H{"config": mediahost.Redact(merged), "enabled": merged.Enabled()})
	})

	// Test upload sends a small synthetic payload so the operator can confirm a
	// provider works without scheduling a real generation.
	r.POST("/workspace/media-hosting/test", func(c *gin.Context) {
		scope, err := CurrentWorkspace(c)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		store, err := mediahost.NewStore(scope.DataDir)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		config, err := store.Read()
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		if !config.Enabled() {
			fail(c, http.StatusBadRequest, errors.New("素材图床未启用"))
			return
		}
		if err := mediahost.Validate(config); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}

		kind := mediahost.MediaKind(strings.TrimSpace(c.DefaultQuery("kind", string(mediahost.MediaKindImage))))
		filename, contentType, payload, err := testPayload(c, kind)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}

		uploader := mediahost.NewUploader()
		cache, _ := mediahost.NewCache(scope.DataDir)
		uploader.Cache = cache
		link, err := uploader.Upload(c.Request.Context(), config, kind, filename, contentType, payload)
		if err != nil {
			fail(c, http.StatusBadGateway, err)
			return
		}
		ok(c, gin.H{"url": link, "kind": kind, "provider": config.Provider, "bytes": len(payload)})
	})
}

// testPayload builds a tiny valid file for the requested kind. Using real
// bytes (not a placeholder string) means the provider's own validation runs.
func testPayload(c *gin.Context, kind mediahost.MediaKind) (string, string, []byte, error) {
	if fileHeader, err := c.FormFile("file"); err == nil && fileHeader != nil {
		return payloadFromUpload(fileHeader)
	}
	switch kind {
	case mediahost.MediaKindImage:
		return "beeftv-probe.png", "image/png", probePNG(), nil
	case mediahost.MediaKindAudio:
		return "beeftv-probe.wav", "audio/wav", probeWAV(), nil
	case mediahost.MediaKindVideo:
		return "beeftv-probe.mp4", "video/mp4", probeMP4(), nil
	default:
		return "", "", nil, errors.New("不支持的素材类型")
	}
}

func payloadFromUpload(header *multipart.FileHeader) (string, string, []byte, error) {
	if header.Size > maxTestUploadBytes {
		return "", "", nil, errors.New("测试文件过大")
	}
	file, err := header.Open()
	if err != nil {
		return "", "", nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxTestUploadBytes+1))
	if err != nil {
		return "", "", nil, err
	}
	if int64(len(payload)) > maxTestUploadBytes {
		return "", "", nil, errors.New("测试文件过大")
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(payload)
	}
	return header.Filename, contentType, payload, nil
}

// probePNG is a valid 1x1 PNG.
func probePNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D,
		0xB0, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
		0x44, 0xAE, 0x42, 0x60, 0x82,
	}
}

// probeWAV is a 0.1s 8kHz mono sine, small but structurally valid.
func probeWAV() []byte {
	const (
		rate      = 8000
		samples   = rate / 10
		dataBytes = samples * 2
	)
	payload := make([]byte, 0, 44+dataBytes)
	payload = append(payload, []byte("RIFF")...)
	payload = appendLittleEndian(payload, uint32(36+dataBytes))
	payload = append(payload, []byte("WAVEfmt ")...)
	payload = appendLittleEndian(payload, uint32(16))
	payload = appendLittleEndian(payload, uint16(1))
	payload = appendLittleEndian(payload, uint16(1))
	payload = appendLittleEndian(payload, uint32(rate))
	payload = appendLittleEndian(payload, uint32(rate*2))
	payload = appendLittleEndian(payload, uint16(2))
	payload = appendLittleEndian(payload, uint16(16))
	payload = append(payload, []byte("data")...)
	payload = appendLittleEndian(payload, uint32(dataBytes))
	for i := 0; i < samples; i++ {
		payload = appendLittleEndian(payload, int16(0))
	}
	return payload
}

// probeMP4 is a minimal ftyp box, enough for a provider to accept the upload.
func probeMP4() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00,
		'i', 's', 'o', 'm', 'i', 's', 'o', '2',
	}
}

func appendLittleEndian[T ~uint16 | ~uint32 | ~int16](target []byte, value T) []byte {
	switch typed := any(value).(type) {
	case uint16:
		return append(target, byte(typed), byte(typed>>8))
	case int16:
		unsigned := uint16(typed)
		return append(target, byte(unsigned), byte(unsigned>>8))
	case uint32:
		return append(target, byte(typed), byte(typed>>8), byte(typed>>16), byte(typed>>24))
	default:
		return target
	}
}

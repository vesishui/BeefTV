package modelcatalog

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/kernel"
)

type CatalogJSONError struct{ Cause error }

func (e CatalogJSONError) Error() string { return "模型服务返回的不是有效 JSON" }
func (e CatalogJSONError) Unwrap() error { return e.Cause }

type CatalogUpstreamRejectedError struct{}

func (CatalogUpstreamRejectedError) Error() string {
	return "模型服务返回失败，请检查渠道配置"
}

type catalogPayload struct {
	Data   json.RawMessage      `json:"data"`
	Models []catalogPayloadItem `json:"models"`
	Error  *catalogPayloadError `json:"error"`
	Code   *int                 `json:"code"`
}

type catalogPayloadError struct {
	Message string `json:"message"`
}

type catalogPayloadItem struct {
	ID                       string                   `json:"id"`
	Name                     string                   `json:"name"`
	DisplayName              string                   `json:"display_name"`
	Capability               string                   `json:"capability"`
	ModelType                string                   `json:"model_type"`
	Type                     string                   `json:"type"`
	SupportedEndpointTypes   []string                 `json:"supported_endpoint_types"`
	DefaultParameters        catalogPayloadParameters `json:"default_parameters"`
	Options                  catalogPayloadOptions    `json:"options"`
	SupportsImages           *bool                    `json:"supports_images"`
	MinImages                *int                     `json:"min_images"`
	MaxImages                *int                     `json:"max_images"`
	VideoCapabilities        json.RawMessage          `json:"video_capabilities"`
	VideoCapabilitiesVersion string                   `json:"video_capabilities_version"`
	VideoPricing             json.RawMessage          `json:"video_pricing"`
}

type catalogPayloadParameters struct {
	AspectRatio     string `json:"aspect_ratio"`
	DurationSeconds string `json:"duration_seconds"`
	Resolution      string `json:"resolution"`
}

type catalogPayloadOptions struct {
	AspectRatio     []ChannelModelCatalogOption `json:"aspect_ratio"`
	DurationSeconds []ChannelModelCatalogOption `json:"duration_seconds"`
	Resolution      []ChannelModelCatalogOption `json:"resolution"`
}

func NormalizeCatalogAPIFormat(apiFormat string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(apiFormat))
	if normalized == "" {
		normalized = "openai"
	}
	if normalized != "openai" && normalized != "gemini" {
		return "", kernel.BadAuthRequest("接口协议不支持拉取模型")
	}
	return normalized, nil
}

func ValidateCatalogRequest(baseURL, apiKey, apiFormat string) (string, string, string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" {
		return "", "", "", kernel.BadAuthRequest("请填写 Base URL")
	}
	if apiKey == "" {
		return "", "", "", kernel.BadAuthRequest("请填写 API Key")
	}
	normalizedFormat, err := NormalizeCatalogAPIFormat(apiFormat)
	if err != nil {
		return "", "", "", err
	}
	return baseURL, apiKey, normalizedFormat, nil
}

func LoadChannelModelCatalog(ctx context.Context, fetcher CatalogFetcher, baseURL, apiFormat, apiKey string, headers []ChannelHeader, extras CatalogExtraSource) ([]ChannelModelCatalogItem, error) {
	baseURL, apiKey, apiFormat, err := ValidateCatalogRequest(baseURL, apiKey, apiFormat)
	if err != nil {
		return nil, err
	}
	if fetcher == nil {
		return nil, CatalogFetchError{Cause: kernel.BadAuthRequest("模型服务地址无效")}
	}
	data, err := fetcher(ctx, baseURL, apiFormat, apiKey, headers)
	if err != nil {
		return nil, err
	}
	catalog, err := ParseChannelModelCatalog(data, apiFormat)
	if err != nil {
		return nil, err
	}
	catalog = OverlayCatalogVideoCapabilities(catalog)
	if extras != nil {
		catalog = MergeCatalogExtras(catalog, extras(baseURL, apiFormat, headers))
	}
	return catalog, nil
}

func ParseChannelModelCatalog(data []byte, apiFormat string) ([]ChannelModelCatalogItem, error) {
	apiFormat, err := NormalizeCatalogAPIFormat(apiFormat)
	if err != nil {
		return nil, err
	}
	var payload catalogPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, CatalogJSONError{Cause: err}
	}
	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return nil, CatalogUpstreamRejectedError{}
	}
	if payload.Code != nil && *payload.Code != 0 {
		return nil, CatalogUpstreamRejectedError{}
	}
	items := payload.Models
	if apiFormat == "openai" {
		items, err = parseOpenAICatalogData(payload.Data)
		if err != nil {
			return nil, CatalogJSONError{Cause: err}
		}
	}
	if apiFormat == "gemini" {
		items = payload.Models
	}
	seen := make(map[string]bool, len(items))
	catalog := make([]ChannelModelCatalogItem, 0, len(items))
	for _, item := range items {
		name := strings.TrimPrefix(strings.TrimSpace(kernel.FirstNonEmpty(item.ID, item.Name)), "models/")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		modelType := firstNonEmptyCatalog(item.ModelType, item.Capability, item.Type)
		if strings.EqualFold(strings.TrimSpace(modelType), "chat") {
			modelType = "text"
		}
		entry := ChannelModelCatalogItem{
			ID:                     name,
			DisplayName:            strings.TrimSpace(item.DisplayName),
			ModelType:              NormalizeCatalogModelType(modelType),
			SupportedEndpointTypes: NormalizeCatalogEndpointTypes(item.SupportedEndpointTypes),
			DefaultParameters: ChannelModelCatalogDefaultParameters{
				AspectRatio:     strings.TrimSpace(item.DefaultParameters.AspectRatio),
				DurationSeconds: strings.TrimSpace(item.DefaultParameters.DurationSeconds),
				Resolution:      strings.TrimSpace(item.DefaultParameters.Resolution),
			},
			Options: ChannelModelCatalogOptions{
				AspectRatio:     NormalizeCatalogOptions(item.Options.AspectRatio),
				DurationSeconds: NormalizeCatalogOptions(item.Options.DurationSeconds),
				Resolution:      NormalizeCatalogOptions(item.Options.Resolution),
			},
			SupportsImages:    item.SupportsImages,
			MinImages:         item.MinImages,
			MaxImages:         item.MaxImages,
			VideoCapabilities: item.VideoCapabilities,
			VideoPricing:      item.VideoPricing,
		}
		if version := strings.TrimSpace(item.VideoCapabilitiesVersion); version != "" || len(item.VideoCapabilities) > 0 {
			copied := version
			entry.VideoCapabilitiesVersion = &copied
		}
		catalog = append(catalog, entry)
	}
	sort.Slice(catalog, func(left int, right int) bool {
		return catalog[left].ID < catalog[right].ID
	})
	return catalog, nil
}

func firstNonEmptyCatalog(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

// parseOpenAICatalogData accepts both the standard OpenAI array and providers
// that group model names by capability (for example OpenVideo's
// data.video/data.image/data.audio/data.chat response).
func parseOpenAICatalogData(raw json.RawMessage) ([]catalogPayloadItem, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var items []catalogPayloadItem
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, nil
	}

	var buckets map[string]json.RawMessage
	if err := json.Unmarshal(raw, &buckets); err != nil {
		return nil, err
	}
	items := make([]catalogPayloadItem, 0)
	for _, bucket := range []struct {
		name      string
		modelType string
	}{
		{name: "video", modelType: "video"},
		{name: "image", modelType: "image"},
		{name: "audio", modelType: "audio"},
		{name: "chat", modelType: "text"},
		{name: "other"},
	} {
		values, ok := buckets[bucket.name]
		if !ok || strings.TrimSpace(string(values)) == "null" {
			continue
		}
		var names []string
		if err := json.Unmarshal(values, &names); err == nil {
			for _, name := range names {
				items = append(items, catalogPayloadItem{ID: name, ModelType: bucket.modelType})
			}
			continue
		}
		var records []catalogPayloadItem
		if err := json.Unmarshal(values, &records); err != nil {
			return nil, err
		}
		for index := range records {
			if bucket.modelType != "" {
				records[index].ModelType = bucket.modelType
			}
		}
		items = append(items, records...)
	}
	return items, nil
}

func CatalogModelIDs(items []ChannelModelCatalogItem) []string {
	seen := make(map[string]bool, len(items))
	models := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.ID)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		models = append(models, name)
	}
	sort.Strings(models)
	return models
}

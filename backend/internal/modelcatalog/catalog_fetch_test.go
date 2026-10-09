package modelcatalog

import (
	"context"
	"errors"
	"testing"
)

func TestParseChannelModelCatalogOpenAIAndGemini(t *testing.T) {
	openAI := []byte(`{"data":[{"id":"models/b"},{"id":"a","display_name":"A","model_type":"image"}]}`)
	items, err := ParseChannelModelCatalog(openAI, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "a" || items[1].ID != "b" || items[0].DisplayName != "A" || items[0].ModelType != "image" {
		t.Fatalf("openai catalog = %#v", items)
	}
	gemini := []byte(`{"models":[{"name":"models/gemini-pro"}]}`)
	items, err = ParseChannelModelCatalog(gemini, "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "gemini-pro" {
		t.Fatalf("gemini catalog = %#v", items)
	}
}

func TestParseChannelModelCatalogOpenVideoCapabilityBuckets(t *testing.T) {
	openVideo := []byte(`{
		"code": 0,
		"data": {
			"video": ["480PSeedance2.0", "Seedance2.0", "seedancemini", "测试"],
			"image": ["krea2"],
			"audio": ["voice-model"],
			"chat": ["chat-model"],
			"other": ["other-model"]
		},
		"message": "ok"
	}`)
	items, err := ParseChannelModelCatalog(openVideo, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("OpenVideo catalog length = %d, items = %#v", len(items), items)
	}
	byID := make(map[string]ChannelModelCatalogItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	for _, id := range []string{"480PSeedance2.0", "Seedance2.0", "seedancemini", "测试"} {
		if got := byID[id].ModelType; got != "video" {
			t.Fatalf("OpenVideo model %q type = %q, want video", id, got)
		}
	}
	for id, want := range map[string]string{
		"krea2":       "image",
		"voice-model": "audio",
		"chat-model":  "text",
		"other-model": "",
	} {
		if got := byID[id].ModelType; got != want {
			t.Fatalf("OpenVideo bucket %q type = %q, want %q", id, got, want)
		}
	}
}

func TestParseChannelModelCatalogOpenVideoRecordsUseCapabilityField(t *testing.T) {
	openVideo := []byte(`{
		"code": 0,
		"data": {
			"video": [
				{"id": "Seedance2.0", "capability": "video"},
				{"name": "seedancemini", "type": "video"}
			],
			"image": [{"id": "krea2"}],
			"chat": [{"id": "chat-model", "model_type": "chat"}]
		},
		"message": "ok"
	}`)
	items, err := ParseChannelModelCatalog(openVideo, "openai")
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]ChannelModelCatalogItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	for id, want := range map[string]string{
		"Seedance2.0":  "video",
		"seedancemini": "video",
		"krea2":        "image",
		"chat-model":   "text",
	} {
		if got := byID[id].ModelType; got != want {
			t.Fatalf("OpenVideo 记录 %q type = %q, want %q", id, got, want)
		}
	}
}

func TestParseChannelModelCatalogNormalizesChatRecordToText(t *testing.T) {
	items, err := ParseChannelModelCatalog([]byte(`{
		"code": 0,
		"data": {
			"chat": [{"id": "chat-record", "model_type": "chat"}]
		}
	}`), "openai")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "chat-record" || items[0].ModelType != "text" {
		t.Fatalf("chat record = %#v", items)
	}
}

func TestParseChannelModelCatalogRejectsInvalidJSONAndUpstreamError(t *testing.T) {
	if _, err := ParseChannelModelCatalog([]byte("not-json"), "openai"); err == nil {
		t.Fatal("invalid json accepted")
	}
	if _, err := ParseChannelModelCatalog([]byte(`{"error":{"message":"nope"}}`), "openai"); err == nil {
		t.Fatal("upstream error accepted")
	}
	if _, err := ParseChannelModelCatalog([]byte(`{"code":1}`), "openai"); err == nil {
		t.Fatal("nonzero code accepted")
	}
}

func TestValidateCatalogRequest(t *testing.T) {
	if _, _, _, err := ValidateCatalogRequest("", "k", ""); err == nil {
		t.Fatal("empty url accepted")
	}
	if _, _, _, err := ValidateCatalogRequest("https://example.com/v1", "", ""); err == nil {
		t.Fatal("empty key accepted")
	}
	if _, _, _, err := ValidateCatalogRequest("https://example.com/v1", "k", "claude"); err == nil {
		t.Fatal("unsupported format accepted")
	}
	base, key, format, err := ValidateCatalogRequest(" https://example.com/v1/ ", " k ", "")
	if err != nil || base != "https://example.com/v1" || key != "k" || format != "openai" {
		t.Fatalf("normalized = %q %q %q %v", base, key, format, err)
	}
}

func TestLoadChannelModelCatalogUsesFetcher(t *testing.T) {
	called := false
	items, err := LoadChannelModelCatalog(context.Background(), func(ctx context.Context, baseURL, apiFormat, apiKey string, headers []ChannelHeader) ([]byte, error) {
		called = true
		if baseURL != "https://example.com/v1" || apiFormat != "openai" || apiKey != "k" {
			t.Fatalf("fetcher args = %q %q %q", baseURL, apiFormat, apiKey)
		}
		return []byte(`{"data":[{"id":"m1"}]}`), nil
	}, "https://example.com/v1", "openai", "k", nil, nil)
	if err != nil || !called || len(items) != 1 || items[0].ID != "m1" {
		t.Fatalf("load = %#v %v called=%v", items, err, called)
	}
	_, err = LoadChannelModelCatalog(context.Background(), func(context.Context, string, string, string, []ChannelHeader) ([]byte, error) {
		return nil, CatalogFetchError{StatusCode: 401, Cause: errors.New("no")}
	}, "https://example.com/v1", "openai", "k", nil, nil)
	if err == nil {
		t.Fatal("fetcher error dropped")
	}
}

func TestCatalogModelIDs(t *testing.T) {
	ids := CatalogModelIDs([]ChannelModelCatalogItem{{ID: "b"}, {ID: "a"}, {ID: "a"}, {ID: ""}})
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("ids = %#v", ids)
	}
}

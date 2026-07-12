package protocolcodec

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestEncodeCacheMetadataVersioned(t *testing.T) {
	t.Parallel()
	input := json.RawMessage(`{"resources":[],"ttlMs":99,"cacheScope":"private"}`)
	modern, err := EncodeCacheMetadata(VersionMCP20260728, input, CacheMetadata{TTLMs: 2500, Scope: "public"})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONFields(t, modern, true)
	legacy, err := EncodeCacheMetadata(VersionMCP20251125, input, CacheMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONFields(t, legacy, false)
}

func TestEncodeCacheMetadataConcurrent(t *testing.T) {
	t.Parallel()
	const workers = 32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := EncodeCacheMetadata(VersionMCP20260728, json.RawMessage(`{"contents":[]}`), CacheMetadata{Scope: "private"}); err != nil {
				t.Errorf("EncodeCacheMetadata: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestResourceNotFoundCode(t *testing.T) {
	t.Parallel()
	if got := ResourceNotFoundCode(VersionMCP20260728); got != -32602 {
		t.Fatalf("modern code = %d", got)
	}
	if got := ResourceNotFoundCode(VersionMCP20251125); got != -32002 {
		t.Fatalf("legacy code = %d", got)
	}
}

func TestEncodeStructuredPresence(t *testing.T) {
	t.Parallel()
	base := json.RawMessage(`{"content":[{"type":"text","text":"model"},{"type":"text","text":"{}"}],"structuredContent":{}}`)
	absent, err := EncodeStructuredPresence(base, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(absent) != `{"content":[{"text":"model","type":"text"}]}` {
		t.Fatalf("absent = %s", absent)
	}
	nullValue, err := EncodeStructuredPresence(base, true, true)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(nullValue, &result); err != nil {
		t.Fatal(err)
	}
	if string(result.Structured) != "null" || len(result.Content) != 2 || result.Content[1].Text != "null" {
		t.Fatalf("explicit null = %s", nullValue)
	}
}

func assertJSONFields(t *testing.T, raw []byte, present bool) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ttlMs", "cacheScope"} {
		_, ok := object[field]
		if ok != present {
			t.Fatalf("field %s presence = %v, want %v: %s", field, ok, present, raw)
		}
	}
}

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

func TestEncodeResultTypeVersioned(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		version ProtocolVersion
		input   string
		want    string
		present bool
	}{
		{name: "modern absent", version: VersionMCP20260728, input: `{"value":1}`, want: "complete", present: true},
		{name: "modern complete", version: VersionMCP20260728, input: `{"resultType":"complete"}`, want: "complete", present: true},
		{name: "modern input required", version: VersionMCP20260728, input: `{"resultType":"input_required"}`, want: "input_required", present: true},
		{name: "modern task", version: VersionMCP20260728, input: `{"resultType":"task"}`, want: "task", present: true},
		{name: "legacy absent", version: VersionMCP20251125, input: `{"value":1}`, present: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := EncodeResultType(tc.version, json.RawMessage(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			gotRaw, present := result["resultType"]
			if present != tc.present {
				t.Fatalf("resultType presence = %v, want %v: %s", present, tc.present, raw)
			}
			if present {
				var got string
				if err := json.Unmarshal(gotRaw, &got); err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Fatalf("resultType = %q, want %q: %s", got, tc.want, raw)
				}
			}
		})
	}
}

func TestEncodeResultTypeConcurrent(t *testing.T) {
	t.Parallel()
	const workers = 32
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := EncodeResultType(VersionMCP20260728, json.RawMessage(`{"value":1}`)); err != nil {
				t.Errorf("EncodeResultType: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestEncodeResultTypeRejectsNonObject(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`[]`)} {
		if _, err := EncodeResultType(VersionMCP20260728, raw); err == nil {
			t.Fatalf("EncodeResultType accepted %s", raw)
		}
	}
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

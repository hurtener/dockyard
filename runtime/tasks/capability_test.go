package tasks

import (
	"encoding/json"
	"testing"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// canon canonicalises a JSON value so a golden comparison is key-order
// independent.
func canon(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

// TestGolden_CapabilityJSON pins the exact tasks-capability wire JSON the
// engine advertises (AGENTS.md §11 — golden coverage of the wire surface).
func TestGolden_CapabilityJSON(t *testing.T) {
	t.Parallel()

	t.Run("list off", func(t *testing.T) {
		t.Parallel()
		e := newEngine(t, nil)
		raw, err := e.CapabilityJSON()
		if err != nil {
			t.Fatalf("CapabilityJSON: %v", err)
		}
		const want = `{"cancel":{},"requests":{"tools":{"call":{}}}}`
		if got := canon(t, raw); got != want {
			t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("list on", func(t *testing.T) {
		t.Parallel()
		e := newEngine(t, &Options{AdvertiseList: true, RequestorIdentifiable: true})
		raw, err := e.CapabilityJSON()
		if err != nil {
			t.Fatalf("CapabilityJSON: %v", err)
		}
		const want = `{"cancel":{},"list":{},"requests":{"tools":{"call":{}}}}`
		if got := canon(t, raw); got != want {
			t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
		}
	})
}

// TestCapabilityRoundTrip proves the advertised capability decodes back to the
// engine's configuration through the codec.
func TestCapabilityRoundTrip(t *testing.T) {
	t.Parallel()
	e := newEngine(t, &Options{AdvertiseList: true, RequestorIdentifiable: true})
	raw, err := e.CapabilityJSON()
	if err != nil {
		t.Fatalf("CapabilityJSON: %v", err)
	}
	decoded, ok, err := protocolcodec.CodecFor(protocolcodec.DefaultVersion).
		DecodeTasksServerCapability(raw)
	if err != nil || !ok {
		t.Fatalf("decode capability: ok=%v err=%v", ok, err)
	}
	if !decoded.List || !decoded.Cancel || !decoded.ToolsCall {
		t.Fatalf("capability did not round-trip: %#v", decoded)
	}
}

// TestCapabilityKey documents the binding fact that the Tasks capability is the
// top-level `tasks` key, not a `capabilities.extensions` entry.
func TestCapabilityKey(t *testing.T) {
	t.Parallel()
	if CapabilityKey != "tasks" {
		t.Fatalf("CapabilityKey = %q, want \"tasks\"", CapabilityKey)
	}
	if ExtensionID != "io.modelcontextprotocol/tasks" {
		t.Fatalf("ExtensionID = %q", ExtensionID)
	}
}

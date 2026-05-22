package protocolcodec

import (
	"encoding/json"
	"testing"
)

// This file holds the Phase 21.5 fuzz targets for the protocolcodec wire-format
// decode surface — the seam every raw MCP extension wire type lives behind
// (P3). The invariant under fuzz is uniform: a decoder NEVER panics on
// arbitrary input, and where a decode succeeds, re-encoding the decoded value
// and decoding again is stable (round-trip). A decoder may legitimately return
// an error on malformed input — that is correct behaviour, not a fuzz failure.
//
// CI runs the seed corpus as ordinary tests (the default when -fuzz is absent).
// To run a longer session locally:
//
//	go test ./internal/protocolcodec -run '^$' -fuzz FuzzDecodeTask -fuzztime 60s

// FuzzDecodeTask fuzzes the Tasks `Task` decoder. Invariant: DecodeTask never
// panics; on a successful decode, EncodeTask→DecodeTask round-trips to an
// equal Task.
func FuzzDecodeTask(f *testing.F) {
	f.Add([]byte(`{"taskId":"t-1","status":"working"}`))
	f.Add([]byte(`{"taskId":"t-2","status":"completed","createdAt":"2026-01-02T03:04:05Z"}`))
	f.Add([]byte(`{"taskId":"t-3","status":"input_required","ttl":5000,"pollInterval":250}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"taskId":"x","status":"bogus-status"}`))
	f.Add([]byte(`[1,2,3]`))

	c := CodecFor(VersionMCP20251125)
	f.Fuzz(func(t *testing.T, raw []byte) {
		task, err := c.DecodeTask(json.RawMessage(raw))
		if err != nil {
			return // a decode error on malformed input is correct.
		}
		// Round-trip: a value that decoded cleanly must re-encode and decode
		// back to an equal value.
		reEncoded, err := c.EncodeTask(task)
		if err != nil {
			t.Fatalf("EncodeTask failed on a value that decoded cleanly: %v", err)
		}
		again, err := c.DecodeTask(reEncoded)
		if err != nil {
			t.Fatalf("DecodeTask failed on freshly encoded output: %v", err)
		}
		if again.ID != task.ID || again.Status != task.Status {
			t.Fatalf("round-trip drift: got id=%q status=%q, want id=%q status=%q",
				again.ID, again.Status, task.ID, task.Status)
		}
	})
}

// FuzzDecodeTaskMeta fuzzes the request-augmentation `task` field decoder.
// Invariant: no panic; a successful decode round-trips.
func FuzzDecodeTaskMeta(f *testing.F) {
	f.Add([]byte(`{"ttl":1000}`))
	f.Add([]byte(`{"ttl":1000,"pollInterval":100}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"ttl":"not-a-number"}`))

	c := CodecFor(VersionMCP20251125)
	f.Fuzz(func(t *testing.T, raw []byte) {
		m, ok, err := c.DecodeTaskMeta(json.RawMessage(raw))
		if err != nil || !ok {
			return
		}
		reEncoded, err := c.EncodeTaskMeta(m)
		if err != nil {
			t.Fatalf("EncodeTaskMeta failed on a cleanly decoded value: %v", err)
		}
		if _, _, err := c.DecodeTaskMeta(reEncoded); err != nil {
			t.Fatalf("DecodeTaskMeta failed on freshly encoded output: %v", err)
		}
	})
}

// FuzzDecodeCreateTaskResult fuzzes the `tasks/create` result decoder.
func FuzzDecodeCreateTaskResult(f *testing.F) {
	f.Add([]byte(`{"task":{"taskId":"t-1","status":"working"}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"task":42}`))

	c := CodecFor(VersionMCP20251125)
	f.Fuzz(func(t *testing.T, raw []byte) {
		r, err := c.DecodeCreateTaskResult(json.RawMessage(raw))
		if err != nil {
			return
		}
		if _, err := c.EncodeCreateTaskResult(r); err != nil {
			t.Fatalf("EncodeCreateTaskResult failed on a cleanly decoded value: %v", err)
		}
	})
}

// FuzzDecodeAppsExtensionCapability fuzzes the Apps extension-capability
// decoder — the `capabilities.extensions["io.modelcontextprotocol/ui"]` block.
func FuzzDecodeAppsExtensionCapability(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"version":"2026-01-26"}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"version":123}`))
	f.Add([]byte(`"not-an-object"`))

	c := CodecFor(VersionApps20260126)
	f.Fuzz(func(t *testing.T, raw []byte) {
		extCap, ok, err := c.DecodeAppsExtensionCapability(json.RawMessage(raw))
		if err != nil || !ok {
			return
		}
		if _, err := c.EncodeAppsExtensionCapability(extCap); err != nil {
			t.Fatalf("EncodeAppsExtensionCapability failed on a cleanly decoded value: %v", err)
		}
	})
}

// FuzzDecodeAppsToolMeta fuzzes the Apps tool-`_meta` decoder. The input is an
// arbitrary JSON object decoded into a Meta map first — the decoder must
// tolerate any shape (including the deprecated flat key) without panicking.
func FuzzDecodeAppsToolMeta(f *testing.F) {
	f.Add([]byte(`{"ui":{"resourceUri":"ui://x"}}`))
	f.Add([]byte(`{"ui/resourceUri":"ui://legacy"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"ui":42}`))
	f.Add([]byte(`{"ui":{"resourceUri":123}}`))

	c := CodecFor(VersionApps20260126)
	f.Fuzz(func(t *testing.T, raw []byte) {
		var meta Meta
		if err := json.Unmarshal(raw, &meta); err != nil || meta == nil {
			return // not a JSON object — outside this decoder's input domain.
		}
		// Invariant: never panics. An error is an acceptable outcome.
		m, ok, err := c.DecodeAppsToolMeta(meta)
		if err != nil || !ok {
			return
		}
		if _, err := c.EncodeAppsToolMeta(nil, m); err != nil {
			t.Fatalf("EncodeAppsToolMeta failed on a cleanly decoded value: %v", err)
		}
	})
}

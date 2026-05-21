package tasks

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// canonJSON canonicalises a JSON value so a golden comparison is independent of
// key order.
func canonJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(out)
}

// TestGolden_DurableTaskRow pins the exact on-disk JSON shape of a durable
// TaskStore row. The row format is owned by the durable driver and versioned
// independently (schemaVersion); a change to the shape must be a visible golden
// diff, never a silent format drift (CLAUDE.md §11).
func TestGolden_DurableTaskRow(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	ttl := int64(60000)
	poll := int64(1000)
	rec := TaskRecord{
		ID:            "task_0123456789abcdef0123456789abcdef",
		Status:        protocolcodec.TaskWorking,
		StatusMessage: "50% — generating",
		CreatedAt:     created,
		UpdatedAt:     created,
		RequestedTTL:  &ttl,
		TTL:           &ttl,
		ExpiresAt:     created.Add(time.Minute),
		PollInterval:  &poll,
		Method:        "tools/call",
		ToolName:      "generate_report",
		AuthContext:   "alice",
		Result:        TaskResult{Payload: json.RawMessage(`{"isError":false}`)},
	}
	got, err := json.Marshal(rowFromRecord(rec))
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	want := `{
		"schemaVersion": 1,
		"id": "task_0123456789abcdef0123456789abcdef",
		"status": "working",
		"statusMessage": "50% — generating",
		"createdAt": "2026-05-21T12:00:00Z",
		"updatedAt": "2026-05-21T12:00:00Z",
		"requestedTtl": 60000,
		"ttl": 60000,
		"expiresAt": "2026-05-21T12:01:00Z",
		"pollInterval": 1000,
		"method": "tools/call",
		"toolName": "generate_report",
		"authContext": "alice",
		"resultPayload": {"isError":false}
	}`
	if canonJSON(t, got) != canonJSON(t, []byte(want)) {
		t.Errorf("durable task row golden mismatch:\n got: %s\nwant: %s",
			canonJSON(t, got), canonJSON(t, []byte(want)))
	}
}

// TestDurableTaskRow_RoundTrips proves a record survives an encode→decode cycle
// through the durable row format unchanged for every field Phase 14 added.
func TestDurableTaskRow_RoundTrips(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	ttl := int64(120000)
	rec := TaskRecord{
		ID:           "task_feedfacefeedfacefeedfacefeedface",
		Status:       protocolcodec.TaskInputRequired,
		CreatedAt:    created,
		UpdatedAt:    created.Add(time.Second),
		RequestedTTL: &ttl,
		TTL:          &ttl,
		ExpiresAt:    created.Add(2 * time.Minute),
		Method:       "tools/call",
		AuthContext:  "bob",
		Result:       TaskResult{Err: "boom"},
	}
	raw, err := json.Marshal(rowFromRecord(rec))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var row taskRow
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := recordFromRow(row)
	if err != nil {
		t.Fatalf("recordFromRow: %v", err)
	}
	if got.ID != rec.ID || got.Status != rec.Status || got.AuthContext != rec.AuthContext {
		t.Fatalf("identity fields did not round-trip: %#v", got)
	}
	if !got.CreatedAt.Equal(rec.CreatedAt) || !got.UpdatedAt.Equal(rec.UpdatedAt) {
		t.Fatalf("timestamps did not round-trip: %v / %v", got.CreatedAt, got.UpdatedAt)
	}
	if !got.ExpiresAt.Equal(rec.ExpiresAt) {
		t.Fatalf("ExpiresAt did not round-trip: %v", got.ExpiresAt)
	}
	if got.TTL == nil || *got.TTL != *rec.TTL {
		t.Fatalf("TTL did not round-trip: %v", got.TTL)
	}
	if got.Result.Err != "boom" {
		t.Fatalf("result error did not round-trip: %q", got.Result.Err)
	}
}

// TestDurableTaskRow_RejectsFutureSchemaVersion proves a row written by a newer
// binary is a hard error, never a silent misread.
func TestDurableTaskRow_RejectsFutureSchemaVersion(t *testing.T) {
	t.Parallel()
	future := taskRow{
		SchemaVersion: taskStoreSchemaVersion + 1,
		ID:            "task_x",
		Status:        protocolcodec.TaskWorking,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := recordFromRow(future); err == nil {
		t.Fatal("recordFromRow must reject a row from a newer schema version")
	}
}

// TestDurableTaskRow_OmitsEmptyOptionalFields proves an unlimited-retention
// task with no result writes a compact row — no expiresAt, no ttl, no result
// keys when they carry no information.
func TestDurableTaskRow_OmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()
	rec := TaskRecord{
		ID:        "task_minimal",
		Status:    protocolcodec.TaskWorking,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Method:    "tools/call",
	}
	raw, err := json.Marshal(rowFromRecord(rec))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, omitted := range []string{`"expiresAt"`, `"ttl"`, `"resultPayload"`, `"resultErr"`} {
		if bytes.Contains(raw, []byte(omitted)) {
			t.Errorf("a minimal task row should omit %s: %s", omitted, raw)
		}
	}
}

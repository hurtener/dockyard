package protocolcodec

import (
	"encoding/json"
	"testing"
	"time"
)

// Golden tests pin the exact wire JSON the codec emits for fixed inputs, so a
// spec bump that changes a shape is a visible diff (AGENTS.md §11). The
// expected strings are hand-derived from the vendored specs in
// docs/specifications/ — they ARE the spec-compliance assertion.

func canon(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Re-marshal through a generic map so key order is canonical (sorted),
	// making the golden comparison order-independent.
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(out)
}

func TestGolden_AppsToolMeta(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	meta, err := c.EncodeAppsToolMeta(nil, AppsToolMeta{
		ResourceURI: "ui://weather-server/dashboard-template",
		Visibility:  []string{VisibilityApp},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"ui":{"resourceUri":"ui://weather-server/dashboard-template","visibility":["app"]}}`
	if got := canon(t, meta); got != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestGolden_AppsResourceMeta(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	meta, err := c.EncodeAppsResourceMeta(nil, AppsResourceMeta{
		CSP: AppsCSP{
			ConnectDomains:  []string{"https://api.openweathermap.org"},
			ResourceDomains: []string{"https://cdn.jsdelivr.net"},
		},
		PrefersBorder: ptr(true),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"ui":{"csp":{"connectDomains":["https://api.openweathermap.org"],` +
		`"resourceDomains":["https://cdn.jsdelivr.net"]},"prefersBorder":true}}`
	if got := canon(t, meta); got != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestGolden_AppsExtensionCapability(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	raw, err := c.EncodeAppsExtensionCapability(AppsExtensionCapability{
		MIMETypes: []string{MIMETypeApp},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"mimeTypes":["text/html;profile=mcp-app"]}`
	if string(raw) != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", raw, want)
	}
}

func TestGolden_Task(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	raw, err := c.EncodeTask(Task{
		ID:            "task-7f3a",
		Status:        TaskWorking,
		StatusMessage: "collecting rows",
		CreatedAt:     time.Date(2026, 5, 11, 23, 4, 32, 0, time.UTC),
		LastUpdatedAt: time.Date(2026, 5, 11, 23, 4, 32, 0, time.UTC),
		TTL:           ptr(int64(60000)),
		PollInterval:  ptr(int64(1000)),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"taskId":"task-7f3a","status":"working","statusMessage":"collecting rows",` +
		`"createdAt":"2026-05-11T23:04:32Z","lastUpdatedAt":"2026-05-11T23:04:32Z",` +
		`"ttl":60000,"pollInterval":1000}`
	if string(raw) != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", raw, want)
	}
}

func TestGolden_TaskUnlimitedTTL(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	raw, err := c.EncodeTask(Task{
		ID:            "task-0",
		Status:        TaskCompleted,
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastUpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TTL:           nil,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"taskId":"task-0","status":"completed",` +
		`"createdAt":"2026-01-01T00:00:00Z","lastUpdatedAt":"2026-01-01T00:00:00Z","ttl":null}`
	if string(raw) != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", raw, want)
	}
}

func TestGolden_RelatedTaskMeta(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	meta, err := c.EncodeRelatedTaskMeta(nil, "task-7f3a")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"io.modelcontextprotocol/related-task":{"taskId":"task-7f3a"}}`
	if got := canon(t, meta); got != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestGolden_TasksServerCapability(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	raw, err := c.EncodeTasksServerCapability(TasksServerCapability{
		List: true, Cancel: true, ToolsCall: true,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"list":{},"cancel":{},"requests":{"tools":{"call":{}}}}`
	if string(raw) != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", raw, want)
	}
}

func TestGolden_CreateTaskResultMeta(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	meta, err := c.EncodeCreateTaskResultMeta(nil, CreateTaskResultMeta{
		ModelImmediateResponse: "Generating your report…",
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"io.modelcontextprotocol/model-immediate-response":"Generating your report…"}`
	if got := canon(t, meta); got != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestGolden_CreateTaskResult(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	created := time.Date(2025, 11, 25, 10, 30, 0, 0, time.UTC)
	updated := time.Date(2025, 11, 25, 10, 40, 0, 0, time.UTC)
	raw, err := c.EncodeCreateTaskResult(CreateTaskResult{
		Task: Task{
			ID:            "786512e2-9e0d-44bd-8f29-789f320fe840",
			Status:        TaskWorking,
			StatusMessage: "The operation is now in progress.",
			CreatedAt:     created,
			LastUpdatedAt: updated,
			TTL:           ptr(int64(60000)),
			PollInterval:  ptr(int64(5000)),
		},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"task":{"createdAt":"2025-11-25T10:30:00Z",` +
		`"lastUpdatedAt":"2025-11-25T10:40:00Z","pollInterval":5000,` +
		`"status":"working","statusMessage":"The operation is now in progress.",` +
		`"taskId":"786512e2-9e0d-44bd-8f29-789f320fe840","ttl":60000}}`
	if got := canon(t, raw); got != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestGolden_GetTaskResult(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	created := time.Date(2025, 11, 25, 10, 30, 0, 0, time.UTC)
	updated := time.Date(2025, 11, 25, 10, 40, 0, 0, time.UTC)
	raw, err := c.EncodeGetTaskResult(Task{
		ID:            "786512e2-9e0d-44bd-8f29-789f320fe840",
		Status:        TaskWorking,
		CreatedAt:     created,
		LastUpdatedAt: updated,
		TTL:           ptr(int64(30000)),
		PollInterval:  ptr(int64(5000)),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Flat Result & Task — taskId at the top level, no nested `task`.
	const want = `{"createdAt":"2025-11-25T10:30:00Z","lastUpdatedAt":"2025-11-25T10:40:00Z",` +
		`"pollInterval":5000,"status":"working",` +
		`"taskId":"786512e2-9e0d-44bd-8f29-789f320fe840","ttl":30000}`
	if got := canon(t, raw); got != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestGolden_ListTasksResult(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	created := time.Date(2025, 11, 25, 9, 15, 0, 0, time.UTC)
	updated := time.Date(2025, 11, 25, 10, 40, 0, 0, time.UTC)
	raw, err := c.EncodeListTasksResult(ListTasksResult{
		Tasks: []Task{{
			ID:            "abc123-def456-ghi789",
			Status:        TaskCompleted,
			CreatedAt:     created,
			LastUpdatedAt: updated,
			TTL:           ptr(int64(60000)),
		}},
		NextCursor: "next-page-cursor",
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `{"nextCursor":"next-page-cursor","tasks":[{"createdAt":"2025-11-25T09:15:00Z",` +
		`"lastUpdatedAt":"2025-11-25T10:40:00Z","status":"completed",` +
		`"taskId":"abc123-def456-ghi789","ttl":60000}]}`
	if got := canon(t, raw); got != want {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

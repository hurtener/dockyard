package protocolcodec

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

// --- Apps tool meta ----------------------------------------------------------

func TestAppsToolMeta_RoundTrip(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	cases := []struct {
		name string
		in   AppsToolMeta
	}{
		{"resourceURI only", AppsToolMeta{ResourceURI: "ui://weather/dashboard"}},
		{"visibility app-only", AppsToolMeta{
			ResourceURI: "ui://weather/dashboard",
			Visibility:  []string{VisibilityApp},
		}},
		{"visibility both", AppsToolMeta{
			ResourceURI: "ui://weather/dashboard",
			Visibility:  []string{VisibilityModel, VisibilityApp},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta, err := c.EncodeAppsToolMeta(nil, tc.in)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, ok, err := c.DecodeAppsToolMeta(meta)
			if err != nil || !ok {
				t.Fatalf("decode: ok=%v err=%v", ok, err)
			}
			if got.ResourceURI != tc.in.ResourceURI {
				t.Errorf("ResourceURI: got %q want %q", got.ResourceURI, tc.in.ResourceURI)
			}
			if strings.Join(got.Visibility, ",") != strings.Join(tc.in.Visibility, ",") {
				t.Errorf("Visibility: got %v want %v", got.Visibility, tc.in.Visibility)
			}
		})
	}
}

func TestAppsToolMeta_RoundTripThroughJSON(t *testing.T) {
	// Encode -> JSON bytes -> back into a Meta map -> decode. This proves the
	// shape survives an actual wire trip, not just a same-process map.
	c := CodecFor(VersionApps20260126)
	in := AppsToolMeta{ResourceURI: "ui://x/y", Visibility: []string{VisibilityApp}}
	meta, err := c.EncodeAppsToolMeta(nil, in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rt Meta
	if err := json.Unmarshal(b, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok, err := c.DecodeAppsToolMeta(rt)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if got.ResourceURI != in.ResourceURI {
		t.Errorf("ResourceURI: got %q want %q", got.ResourceURI, in.ResourceURI)
	}
}

func TestAppsToolMeta_EmitsNestedFormOnly(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	meta, err := c.EncodeAppsToolMeta(nil, AppsToolMeta{ResourceURI: "ui://a/b"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, bad := meta[metaKeyUIResourceURIFlat]; bad {
		t.Fatal("encoder emitted the deprecated flat ui/resourceUri key")
	}
	if _, ok := meta[metaKeyUI]; !ok {
		t.Fatal("encoder did not emit the nested ui key")
	}
	b, _ := json.Marshal(meta)
	if strings.Contains(string(b), "ui/resourceUri") {
		t.Fatalf("deprecated flat key leaked into wire JSON: %s", b)
	}
}

func TestAppsToolMeta_OptInEmitsBothForms(t *testing.T) {
	// With the opt-in (D-177) the encoder writes BOTH the nested
	// _meta.ui.resourceUri and the deprecated flat key, and the flat value
	// equals the nested resourceUri. The default-mode tests above prove the
	// flat key stays absent without the opt-in.
	c := CodecFor(VersionApps20260126)
	const uri = "ui://a/b"
	meta, err := c.EncodeAppsToolMeta(nil, AppsToolMeta{
		ResourceURI:           uri,
		Visibility:            []string{VisibilityApp},
		EmitLegacyResourceURI: true,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	flat, ok := meta[metaKeyUIResourceURIFlat].(string)
	if !ok {
		t.Fatalf("opt-in did not emit the flat key: %#v", meta)
	}
	if flat != uri {
		t.Errorf("flat key = %q, want it equal to the nested resourceUri %q", flat, uri)
	}
	ui, ok := meta[metaKeyUI].(appsUIToolWire)
	if !ok || ui.ResourceURI != uri {
		t.Fatalf("nested ui form missing or wrong: %#v", meta[metaKeyUI])
	}
	// Round-trip JSON: both keys are present on the wire and the flat value
	// matches.
	b, _ := json.Marshal(meta)
	var rt map[string]any
	if err := json.Unmarshal(b, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rt[metaKeyUIResourceURIFlat] != uri {
		t.Errorf("wire JSON flat key = %v, want %q (JSON: %s)", rt[metaKeyUIResourceURIFlat], uri, b)
	}
	uiObj, _ := rt[metaKeyUI].(map[string]any)
	if uiObj["resourceUri"] != uri {
		t.Errorf("wire JSON nested resourceUri = %v, want %q", uiObj["resourceUri"], uri)
	}
	// The opt-in is still tolerated on read in both directions: decoding the
	// both-keys output yields the nested resourceUri and never surfaces the
	// control flag.
	got, ok, err := c.DecodeAppsToolMeta(meta)
	if err != nil || !ok {
		t.Fatalf("decode both-keys: ok=%v err=%v", ok, err)
	}
	if got.ResourceURI != uri || got.EmitLegacyResourceURI {
		t.Errorf("decode = %+v, want ResourceURI=%q and EmitLegacyResourceURI=false", got, uri)
	}
}

func TestAppsToolMeta_OptInWithoutResourceURIEmitsNothing(t *testing.T) {
	// The opt-in alone, with no ResourceURI, has nothing to emit — neither the
	// nested nor the flat key — so _meta.ui is omitted entirely.
	c := CodecFor(VersionApps20260126)
	meta, err := c.EncodeAppsToolMeta(nil, AppsToolMeta{EmitLegacyResourceURI: true})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, bad := meta[metaKeyUIResourceURIFlat]; bad {
		t.Error("opt-in with no ResourceURI emitted a flat key")
	}
	if _, bad := meta[metaKeyUI]; bad {
		t.Error("opt-in with no ResourceURI emitted a nested ui key")
	}
}

func TestAppsToolMeta_ToleratesDeprecatedFlatForm(t *testing.T) {
	// A peer that still speaks the deprecated _meta["ui/resourceUri"] form
	// must be understood on read (RFC §16 item 3; brief 01 §2.3).
	c := CodecFor(VersionApps20260126)
	wire := Meta{metaKeyUIResourceURIFlat: "ui://legacy/widget"}
	got, ok, err := c.DecodeAppsToolMeta(wire)
	if err != nil || !ok {
		t.Fatalf("decode flat form: ok=%v err=%v", ok, err)
	}
	if got.ResourceURI != "ui://legacy/widget" {
		t.Errorf("ResourceURI: got %q", got.ResourceURI)
	}
}

func TestAppsToolMeta_NeverReEmitsDeprecatedForm(t *testing.T) {
	// Decode a deprecated-form _meta, re-encode it, and confirm the output is
	// the modern nested form with no flat key — the "tolerate on read, never
	// emit" guarantee end to end.
	c := CodecFor(VersionApps20260126)
	in := Meta{
		metaKeyUIResourceURIFlat: "ui://legacy/widget",
		"unrelated":              "kept",
	}
	decoded, ok, err := c.DecodeAppsToolMeta(in)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	out, err := c.EncodeAppsToolMeta(in, decoded)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, bad := out[metaKeyUIResourceURIFlat]; bad {
		t.Fatal("re-encode kept the deprecated flat key")
	}
	if out["unrelated"] != "kept" {
		t.Error("re-encode dropped an unrelated _meta key")
	}
	ui, ok := out[metaKeyUI].(appsUIToolWire)
	if !ok || ui.ResourceURI != "ui://legacy/widget" {
		t.Errorf("nested ui form not produced: %#v", out[metaKeyUI])
	}
}

func TestAppsToolMeta_NestedFormWinsOverFlat(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	in := Meta{
		metaKeyUI:                appsUIToolWire{ResourceURI: "ui://modern/x"},
		metaKeyUIResourceURIFlat: "ui://stale/y",
	}
	got, ok, err := c.DecodeAppsToolMeta(in)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if got.ResourceURI != "ui://modern/x" {
		t.Errorf("nested form should win: got %q", got.ResourceURI)
	}
}

func TestAppsToolMeta_NoUIMeta(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	for _, in := range []Meta{nil, {}, {"other": 1}} {
		_, ok, err := c.DecodeAppsToolMeta(in)
		if err != nil {
			t.Fatalf("decode %v: %v", in, err)
		}
		if ok {
			t.Errorf("decode %v: expected ok=false", in)
		}
	}
}

func TestAppsToolMeta_Malformed(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	if _, _, err := c.DecodeAppsToolMeta(Meta{metaKeyUI: make(chan int)}); err == nil {
		t.Fatal("expected error on un-marshalable ui value")
	} else if !errors.Is(err, ErrMalformedMeta) {
		t.Errorf("want ErrMalformedMeta, got %v", err)
	}
	_, _, err := c.DecodeAppsToolMeta(Meta{metaKeyUIResourceURIFlat: 123})
	if !errors.Is(err, ErrMalformedMeta) {
		t.Errorf("non-string flat key: want ErrMalformedMeta, got %v", err)
	}
}

func TestAppsToolMeta_BasePreservedNotMutated(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	base := Meta{"keepme": "v"}
	out, err := c.EncodeAppsToolMeta(base, AppsToolMeta{ResourceURI: "ui://a/b"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out["keepme"] != "v" {
		t.Error("base key dropped")
	}
	if _, mutated := base[metaKeyUI]; mutated {
		t.Error("encoder mutated the caller's base map")
	}
}

// --- Apps resource meta ------------------------------------------------------

func TestAppsResourceMeta_RoundTrip(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	in := AppsResourceMeta{
		CSP: AppsCSP{
			ConnectDomains:  []string{"https://api.weather.com"},
			ResourceDomains: []string{"https://cdn.jsdelivr.net"},
			FrameDomains:    []string{"https://www.youtube.com"},
			BaseURIDomains:  []string{"https://cdn.example.com"},
		},
		Permissions:   AppsPermissions{Camera: true, ClipboardWrite: true},
		Domain:        "a904794854a047f6.claudemcpcontent.com",
		PrefersBorder: ptr(true),
	}
	meta, err := c.EncodeAppsResourceMeta(nil, in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b, _ := json.Marshal(meta)
	var rt Meta
	if err := json.Unmarshal(b, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok, err := c.DecodeAppsResourceMeta(rt)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if got.Domain != in.Domain {
		t.Errorf("Domain: got %q want %q", got.Domain, in.Domain)
	}
	if got.PrefersBorder == nil || *got.PrefersBorder != true {
		t.Errorf("PrefersBorder: got %v", got.PrefersBorder)
	}
	if !got.Permissions.Camera || !got.Permissions.ClipboardWrite ||
		got.Permissions.Microphone || got.Permissions.Geolocation {
		t.Errorf("Permissions round-trip wrong: %#v", got.Permissions)
	}
	if got.CSP.ConnectDomains[0] != "https://api.weather.com" ||
		got.CSP.FrameDomains[0] != "https://www.youtube.com" {
		t.Errorf("CSP round-trip wrong: %#v", got.CSP)
	}
}

func TestAppsResourceMeta_PrefersBorderFalseIsPreserved(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	meta, err := c.EncodeAppsResourceMeta(nil, AppsResourceMeta{PrefersBorder: ptr(false)})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b, _ := json.Marshal(meta)
	if !strings.Contains(string(b), `"prefersBorder":false`) {
		t.Fatalf("explicit false prefersBorder not emitted: %s", b)
	}
	var rt Meta
	_ = json.Unmarshal(b, &rt)
	got, ok, err := c.DecodeAppsResourceMeta(rt)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if got.PrefersBorder == nil || *got.PrefersBorder != false {
		t.Errorf("PrefersBorder false lost: %v", got.PrefersBorder)
	}
}

func TestAppsResourceMeta_Zero(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	meta, err := c.EncodeAppsResourceMeta(Meta{"x": 1}, AppsResourceMeta{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, bad := meta[metaKeyUI]; bad {
		t.Error("zero resource meta should not emit a ui key")
	}
	_, ok, err := c.DecodeAppsResourceMeta(meta)
	if err != nil || ok {
		t.Errorf("decode zero: ok=%v err=%v", ok, err)
	}
}

// --- Apps extension capability ----------------------------------------------

func TestAppsExtensionCapability_RoundTrip(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	raw, err := c.EncodeAppsExtensionCapability(AppsExtensionCapability{MIMETypes: []string{MIMETypeApp}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok, err := c.DecodeAppsExtensionCapability(raw)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if len(got.MIMETypes) != 1 || got.MIMETypes[0] != MIMETypeApp {
		t.Errorf("MIMETypes: got %v", got.MIMETypes)
	}
}

func TestAppsExtensionCapability_DefaultsMIMEType(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	raw, err := c.EncodeAppsExtensionCapability(AppsExtensionCapability{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(raw), MIMETypeApp) {
		t.Fatalf("missing required mimeTypes default: %s", raw)
	}
}

func TestAppsExtensionCapability_EmptyInput(t *testing.T) {
	c := CodecFor(VersionApps20260126)
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null"), {}} {
		_, ok, err := c.DecodeAppsExtensionCapability(raw)
		if err != nil || ok {
			t.Errorf("decode %q: ok=%v err=%v", raw, ok, err)
		}
	}
}

// --- Tasks: Task object ------------------------------------------------------

func TestTask_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	in := Task{
		ID:            "task-abc",
		Status:        TaskWorking,
		StatusMessage: "collecting rows",
		CreatedAt:     time.Date(2026, 5, 11, 23, 4, 32, 0, time.UTC),
		LastUpdatedAt: time.Date(2026, 5, 11, 23, 5, 0, 0, time.UTC),
		TTL:           ptr(int64(60000)),
		PollInterval:  ptr(int64(1000)),
	}
	raw, err := c.EncodeTask(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := c.DecodeTask(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != in.ID || got.Status != in.Status || got.StatusMessage != in.StatusMessage {
		t.Errorf("scalar mismatch: %#v", got)
	}
	if !got.CreatedAt.Equal(in.CreatedAt) || !got.LastUpdatedAt.Equal(in.LastUpdatedAt) {
		t.Errorf("timestamps: got %v / %v", got.CreatedAt, got.LastUpdatedAt)
	}
	if got.TTL == nil || *got.TTL != 60000 {
		t.Errorf("TTL: got %v", got.TTL)
	}
	if got.PollInterval == nil || *got.PollInterval != 1000 {
		t.Errorf("PollInterval: got %v", got.PollInterval)
	}
}

func TestTask_NullTTLIsUnlimited(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	raw, err := c.EncodeTask(Task{
		ID: "t", Status: TaskCompleted,
		CreatedAt: time.Unix(0, 0).UTC(), LastUpdatedAt: time.Unix(0, 0).UTC(),
		TTL: nil,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The schema requires `ttl` to be present, with null meaning unlimited.
	if !strings.Contains(string(raw), `"ttl":null`) {
		t.Fatalf("nil TTL must marshal to explicit null, got: %s", raw)
	}
	got, err := c.DecodeTask(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TTL != nil {
		t.Errorf("null ttl should decode to nil, got %v", *got.TTL)
	}
}

func TestTask_OmitsEmptyOptionalFields(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	raw, _ := c.EncodeTask(Task{
		ID: "t", Status: TaskWorking,
		CreatedAt: time.Unix(0, 0).UTC(), LastUpdatedAt: time.Unix(0, 0).UTC(),
	})
	s := string(raw)
	if strings.Contains(s, "statusMessage") || strings.Contains(s, "pollInterval") {
		t.Fatalf("empty optional fields should be omitted: %s", s)
	}
}

func TestTask_DecodeRejectsBadStatus(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	_, err := c.DecodeTask(json.RawMessage(
		`{"taskId":"t","status":"bogus","createdAt":"2026-01-01T00:00:00Z","lastUpdatedAt":"2026-01-01T00:00:00Z","ttl":null}`))
	if !errors.Is(err, ErrMalformedMeta) {
		t.Errorf("want ErrMalformedMeta for bad status, got %v", err)
	}
}

func TestTask_DecodeRejectsBadTimestamp(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	_, err := c.DecodeTask(json.RawMessage(
		`{"taskId":"t","status":"working","createdAt":"not-a-time","lastUpdatedAt":"2026-01-01T00:00:00Z","ttl":null}`))
	if !errors.Is(err, ErrMalformedMeta) {
		t.Errorf("want ErrMalformedMeta for bad timestamp, got %v", err)
	}
}

// --- Tasks: TaskMeta (request augmentation) ----------------------------------

func TestTaskMeta_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	raw, err := c.EncodeTaskMeta(TaskMeta{TTL: ptr(int64(30000))})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok, err := c.DecodeTaskMeta(raw)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if got.TTL == nil || *got.TTL != 30000 {
		t.Errorf("TTL: got %v", got.TTL)
	}
}

func TestTaskMeta_EmptyInput(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null")} {
		_, ok, err := c.DecodeTaskMeta(raw)
		if err != nil || ok {
			t.Errorf("decode %q: ok=%v err=%v", raw, ok, err)
		}
	}
}

// --- Tasks: related-task _meta key ------------------------------------------

func TestRelatedTaskMeta_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	meta, err := c.EncodeRelatedTaskMeta(Meta{"keep": 1}, "task-xyz")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if meta["keep"] != 1 {
		t.Error("base key dropped")
	}
	b, _ := json.Marshal(meta)
	if !strings.Contains(string(b), metaKeyRelatedTask) {
		t.Fatalf("related-task key not in wire JSON: %s", b)
	}
	var rt Meta
	_ = json.Unmarshal(b, &rt)
	id, ok, err := c.DecodeRelatedTaskMeta(rt)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if id != "task-xyz" {
		t.Errorf("taskID: got %q", id)
	}
}

func TestRelatedTaskMeta_Absent(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	_, ok, err := c.DecodeRelatedTaskMeta(Meta{"other": 1})
	if err != nil || ok {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

func TestRelatedTaskMeta_Malformed(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	_, _, err := c.DecodeRelatedTaskMeta(Meta{metaKeyRelatedTask: "not-an-object"})
	if !errors.Is(err, ErrMalformedMeta) {
		t.Errorf("want ErrMalformedMeta, got %v", err)
	}
}

// --- Tasks: CreateTaskResult _meta -------------------------------------------

func TestCreateTaskResultMeta_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	meta, err := c.EncodeCreateTaskResultMeta(nil,
		CreateTaskResultMeta{ModelImmediateResponse: "Working on your report…"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok, err := c.DecodeCreateTaskResultMeta(meta)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if got.ModelImmediateResponse != "Working on your report…" {
		t.Errorf("got %q", got.ModelImmediateResponse)
	}
}

func TestCreateTaskResultMeta_ZeroIsNoOp(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	base := Meta{"x": 1}
	out, err := c.EncodeCreateTaskResultMeta(base, CreateTaskResultMeta{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, bad := out[metaKeyModelImmediateResponse]; bad {
		t.Error("zero CreateTaskResultMeta emitted a key")
	}
	_, ok, _ := c.DecodeCreateTaskResultMeta(out)
	if ok {
		t.Error("decode of no-key meta should be ok=false")
	}
}

// TestCreateTaskResultMeta_NilBaseZeroValueReturnsEmptyMeta pins the aligned
// behaviour: with a nil base and a zero value, EncodeCreateTaskResultMeta
// returns a non-nil empty Meta, exactly like the sibling Encode*Meta funcs —
// not a nil Meta. Predictable, non-nil return shapes keep callers simple.
func TestCreateTaskResultMeta_NilBaseZeroValueReturnsEmptyMeta(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	out, err := c.EncodeCreateTaskResultMeta(nil, CreateTaskResultMeta{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out == nil {
		t.Fatal("nil base + zero value returned a nil Meta — should be a non-nil empty Meta")
	}
	if len(out) != 0 {
		t.Errorf("zero value emitted keys: %v", out)
	}
}

func TestCreateTaskResultMeta_Malformed(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	_, _, err := c.DecodeCreateTaskResultMeta(Meta{metaKeyModelImmediateResponse: 42})
	if !errors.Is(err, ErrMalformedMeta) {
		t.Errorf("want ErrMalformedMeta, got %v", err)
	}
}

// --- Tasks: server capability ------------------------------------------------

func TestTasksServerCapability_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	cases := []TasksServerCapability{
		{},
		{List: true},
		{Cancel: true, ToolsCall: true},
		{List: true, Cancel: true, ToolsCall: true},
	}
	for _, in := range cases {
		raw, err := c.EncodeTasksServerCapability(in)
		if err != nil {
			t.Fatalf("encode %#v: %v", in, err)
		}
		got, ok, err := c.DecodeTasksServerCapability(raw)
		if err != nil || !ok {
			t.Fatalf("decode %#v: ok=%v err=%v", in, ok, err)
		}
		if got != in {
			t.Errorf("round-trip: got %#v want %#v", got, in)
		}
	}
}

func TestTasksServerCapability_ToolsCallShape(t *testing.T) {
	// tasks.requests.tools.call must be an empty object on the wire.
	c := CodecFor(VersionMCP20251125)
	raw, _ := c.EncodeTasksServerCapability(TasksServerCapability{ToolsCall: true})
	if !strings.Contains(string(raw), `"requests":{"tools":{"call":{}}}`) {
		t.Fatalf("unexpected requests shape: %s", raw)
	}
}

func TestTasksServerCapability_EmptyInput(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	_, ok, err := c.DecodeTasksServerCapability(json.RawMessage("null"))
	if err != nil || ok {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

// --- Tasks: method envelopes -------------------------------------------------

func TestCreateTaskResult_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	created := time.Date(2026, 5, 21, 10, 30, 0, 0, time.UTC)
	in := CreateTaskResult{
		Task: Task{
			ID:            "task-abc",
			Status:        TaskWorking,
			StatusMessage: "in progress",
			CreatedAt:     created,
			LastUpdatedAt: created,
			TTL:           ptr(int64(60000)),
			PollInterval:  ptr(int64(5000)),
		},
		Meta: Meta{"io.modelcontextprotocol/model-immediate-response": "working on it"},
	}
	raw, err := c.EncodeCreateTaskResult(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := c.DecodeCreateTaskResult(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Task.ID != in.Task.ID || got.Task.Status != in.Task.Status {
		t.Errorf("task mismatch: %#v", got.Task)
	}
	if got.Meta["io.modelcontextprotocol/model-immediate-response"] != "working on it" {
		t.Errorf("meta lost: %#v", got.Meta)
	}
}

func TestCreateTaskResult_DecodeRejectsBadStatus(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	_, err := c.DecodeCreateTaskResult(json.RawMessage(
		`{"task":{"taskId":"x","status":"bogus","createdAt":"2026-05-21T10:30:00Z","lastUpdatedAt":"2026-05-21T10:30:00Z","ttl":null}}`))
	if !errors.Is(err, ErrMalformedMeta) {
		t.Fatalf("want ErrMalformedMeta, got %v", err)
	}
}

func TestTaskIDParams_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	raw, err := c.EncodeTaskIDParams(TaskID{ID: "task-42"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(raw) != `{"taskId":"task-42"}` {
		t.Fatalf("unexpected wire: %s", raw)
	}
	got, err := c.DecodeTaskIDParams(raw)
	if err != nil || got.ID != "task-42" {
		t.Fatalf("decode: %v %#v", err, got)
	}
}

func TestTaskIDParams_DecodeRejectsEmpty(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	_, err := c.DecodeTaskIDParams(json.RawMessage(`{"taskId":""}`))
	if !errors.Is(err, ErrMalformedMeta) {
		t.Fatalf("want ErrMalformedMeta for empty taskId, got %v", err)
	}
}

func TestGetTaskResult_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	created := time.Date(2026, 5, 21, 9, 15, 0, 0, time.UTC)
	in := Task{
		ID:            "task-flat",
		Status:        TaskCompleted,
		CreatedAt:     created,
		LastUpdatedAt: created,
		TTL:           nil,
	}
	raw, err := c.EncodeGetTaskResult(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The GetTaskResult shape is flat: taskId at the top level, no `task` key.
	if !strings.Contains(string(raw), `"taskId":"task-flat"`) || strings.Contains(string(raw), `"task":`) {
		t.Fatalf("GetTaskResult must be the flat Task shape: %s", raw)
	}
	got, err := c.DecodeGetTaskResult(raw)
	if err != nil || got.ID != "task-flat" || got.Status != TaskCompleted {
		t.Fatalf("decode: %v %#v", err, got)
	}
}

func TestListTasks_RoundTrip(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	p, err := c.EncodeListTasksParams(ListTasksParams{Cursor: "page-2"})
	if err != nil {
		t.Fatalf("encode params: %v", err)
	}
	gotP, err := c.DecodeListTasksParams(p)
	if err != nil || gotP.Cursor != "page-2" {
		t.Fatalf("decode params: %v %#v", err, gotP)
	}
	created := time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC)
	res := ListTasksResult{
		Tasks: []Task{
			{ID: "t1", Status: TaskWorking, CreatedAt: created, LastUpdatedAt: created},
			{ID: "t2", Status: TaskCompleted, CreatedAt: created, LastUpdatedAt: created},
		},
		NextCursor: "page-3",
	}
	raw, err := c.EncodeListTasksResult(res)
	if err != nil {
		t.Fatalf("encode result: %v", err)
	}
	gotR, err := c.DecodeListTasksResult(raw)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(gotR.Tasks) != 2 || gotR.NextCursor != "page-3" {
		t.Fatalf("result mismatch: %#v", gotR)
	}
}

func TestListTasks_EmptyPageIsArrayNotNull(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	raw, err := c.EncodeListTasksResult(ListTasksResult{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// `tasks` is a required field; an empty page is [], never null.
	if !strings.Contains(string(raw), `"tasks":[]`) {
		t.Fatalf("empty tasks page must encode as []: %s", raw)
	}
}

func TestListTasksParams_EmptyInput(t *testing.T) {
	c := CodecFor(VersionMCP20251125)
	got, err := c.DecodeListTasksParams(nil)
	if err != nil || got.Cursor != "" {
		t.Fatalf("nil params: %v %#v", err, got)
	}
}

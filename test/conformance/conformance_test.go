package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// fixturePath returns the absolute path of a fixture file inside this
// package's fixtures/ directory.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(wd, "fixtures", name)
}

// loadFixture reads a JSON fixture, strips the `_cite` annotation key, and
// returns the canonical wire bytes (no whitespace, sorted keys) used for
// round-trip comparison. The `_cite` key is a convention this package adds
// to every fixture so the source-of-truth citation is co-located with the
// fixture; it is NOT part of the wire shape and is removed before
// comparison.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	stripCite(generic)
	canonical, err := canonicalJSON(generic)
	if err != nil {
		t.Fatalf("canonicalize fixture %s: %v", name, err)
	}
	return canonical
}

// stripCite removes the `_cite` annotation key from any nested map so the
// canonical-byte comparison is against the wire shape only.
func stripCite(v any) {
	switch x := v.(type) {
	case map[string]any:
		delete(x, "_cite")
		for _, child := range x {
			stripCite(child)
		}
	case []any:
		for _, child := range x {
			stripCite(child)
		}
	}
}

// canonicalJSON re-marshals v through a generic any so map keys are sorted
// (encoding/json guarantees this on map[string]any), producing a
// deterministic byte string for comparison.
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Trim the trailing newline json.Encoder appends.
	out := bytes.TrimRight(buf.Bytes(), "\n")
	return out, nil
}

// roundTripJSON decodes raw into a generic value, strips _cite, and returns
// canonical bytes — the comparison shape after a codec round-trip.
func roundTripJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("re-decode round-tripped bytes: %v\n  bytes: %s", err, raw)
	}
	stripCite(v)
	out, err := canonicalJSON(v)
	if err != nil {
		t.Fatalf("canonicalize round-tripped bytes: %v", err)
	}
	return out
}

// TestConformance_AppsToolMeta_NestedRoundTrip asserts the codec round-
// trips the canonical Apps tool `_meta.ui` shape against the vendored Apps
// spec snapshot. Decode → re-encode → byte-compare.
//
// Source: docs/specifications/mcp-apps-2026-01-26.mdx, section "Linking a
// Tool to a UI Resource".
func TestConformance_AppsToolMeta_NestedRoundTrip(t *testing.T) {
	t.Parallel()
	want := loadFixture(t, "apps_tool_meta_nested.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)

	// Decode the fixture into the codec's domain type.
	var meta protocolcodec.Meta
	if err := json.Unmarshal(want, &meta); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	dec, ok, err := c.DecodeAppsToolMeta(meta)
	if err != nil {
		t.Fatalf("DecodeAppsToolMeta: %v", err)
	}
	if !ok {
		t.Fatalf("DecodeAppsToolMeta: !ok on fixture")
	}

	// Re-encode.
	out, err := c.EncodeAppsToolMeta(nil, dec)
	if err != nil {
		t.Fatalf("EncodeAppsToolMeta: %v", err)
	}
	// Re-canonicalize for byte comparison.
	outRaw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal re-encoded: %v", err)
	}
	got := roundTripJSON(t, outRaw)
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance round-trip mismatch:\n  want: %s\n   got: %s", want, got)
	}
}

// TestConformance_AppsToolMeta_DeprecatedFlatToleration asserts the codec
// TOLERATES the deprecated flat `_meta["ui/resourceUri"]` form on READ
// (preserves the link), and NEVER EMITS it (the re-encoded shape is the
// canonical nested form). This is the binding RFC §16 forward-
// compatibility rule.
//
// Source: docs/specifications/mcp-apps-2026-01-26.mdx — the deprecated
// flat key is mentioned alongside the nested canonical form; the codec
// is documented to tolerate-on-read and never-emit.
func TestConformance_AppsToolMeta_DeprecatedFlatToleration(t *testing.T) {
	t.Parallel()
	flat := loadFixture(t, "apps_tool_meta_flat_deprecated.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)

	var meta protocolcodec.Meta
	if err := json.Unmarshal(flat, &meta); err != nil {
		t.Fatalf("unmarshal flat fixture: %v", err)
	}
	dec, ok, err := c.DecodeAppsToolMeta(meta)
	if err != nil {
		t.Fatalf("DecodeAppsToolMeta(flat): %v", err)
	}
	if !ok {
		t.Fatalf("DecodeAppsToolMeta(flat): expected ok=true (toleration)")
	}
	if dec.ResourceURI != "ui://legacy-server/dashboard" {
		t.Fatalf("flat toleration: lost resourceUri, got %q", dec.ResourceURI)
	}

	// Re-encode and assert the FLAT key is GONE — only the nested form is
	// emitted.
	out, err := c.EncodeAppsToolMeta(nil, dec)
	if err != nil {
		t.Fatalf("EncodeAppsToolMeta after flat decode: %v", err)
	}
	if _, present := out["ui/resourceUri"]; present {
		t.Fatalf("encoder emitted the DEPRECATED flat key — RFC §16 violation")
	}
	if _, present := out["ui"]; !present {
		t.Fatalf("encoder did not emit the canonical nested `ui` key")
	}
}

// TestConformance_AppsResourceMeta_CSP asserts the codec round-trips the
// CSP + prefersBorder block (the deny-by-default sandbox configuration).
//
// Source: docs/specifications/mcp-apps-2026-01-26.mdx, section "_meta.ui
// fields on a Resource".
func TestConformance_AppsResourceMeta_CSP(t *testing.T) {
	t.Parallel()
	want := loadFixture(t, "apps_resource_meta_csp.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)

	var meta protocolcodec.Meta
	if err := json.Unmarshal(want, &meta); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	dec, ok, err := c.DecodeAppsResourceMeta(meta)
	if err != nil {
		t.Fatalf("DecodeAppsResourceMeta: %v", err)
	}
	if !ok {
		t.Fatalf("DecodeAppsResourceMeta: !ok on fixture")
	}
	out, err := c.EncodeAppsResourceMeta(nil, dec)
	if err != nil {
		t.Fatalf("EncodeAppsResourceMeta: %v", err)
	}
	outRaw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal re-encoded: %v", err)
	}
	got := roundTripJSON(t, outRaw)
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance round-trip mismatch:\n  want: %s\n   got: %s", want, got)
	}
}

// TestConformance_AppsExtensionCapability asserts the
// capabilities.extensions["io.modelcontextprotocol/ui"] block round-
// trips byte-stably against the vendored canonical shape.
//
// Source: docs/specifications/mcp-apps-2026-01-26.mdx, section
// "Capability Declaration".
func TestConformance_AppsExtensionCapability(t *testing.T) {
	t.Parallel()
	want := loadFixture(t, "apps_extension_capability.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)
	dec, ok, err := c.DecodeAppsExtensionCapability(want)
	if err != nil {
		t.Fatalf("DecodeAppsExtensionCapability: %v", err)
	}
	if !ok {
		t.Fatalf("DecodeAppsExtensionCapability: !ok")
	}
	raw, err := c.EncodeAppsExtensionCapability(dec)
	if err != nil {
		t.Fatalf("EncodeAppsExtensionCapability: %v", err)
	}
	got := roundTripJSON(t, raw)
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance round-trip mismatch:\n  want: %s\n   got: %s", want, got)
	}
}

// TestConformance_Task_Working asserts the Task object round-trips byte-
// stably for a working task with TTL + pollInterval set.
//
// Source: docs/specifications/mcp-tasks-experimental.mdx, section
// "Task object".
func TestConformance_Task_Working(t *testing.T) {
	t.Parallel()
	want := loadFixture(t, "task_working.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionMCP20251125)
	dec, err := c.DecodeTask(want)
	if err != nil {
		t.Fatalf("DecodeTask: %v", err)
	}
	raw, err := c.EncodeTask(dec)
	if err != nil {
		t.Fatalf("EncodeTask: %v", err)
	}
	got := roundTripJSON(t, raw)
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance round-trip mismatch:\n  want: %s\n   got: %s", want, got)
	}
}

// TestConformance_Task_UnlimitedTTL asserts a completed task with
// ttl=null (the unlimited-retention canonical wire shape) round-trips
// byte-stably — and that the codec emits an explicit JSON null, never
// omitting the field (the spec's "ttl is always present" invariant).
//
// Source: docs/specifications/mcp-tasks-experimental.mdx, section
// "Task object" — "Receivers MUST emit a ttl field on every task
// response; null means unlimited retention".
func TestConformance_Task_UnlimitedTTL(t *testing.T) {
	t.Parallel()
	want := loadFixture(t, "task_completed_unlimited_ttl.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionMCP20251125)
	dec, err := c.DecodeTask(want)
	if err != nil {
		t.Fatalf("DecodeTask: %v", err)
	}
	raw, err := c.EncodeTask(dec)
	if err != nil {
		t.Fatalf("EncodeTask: %v", err)
	}
	got := roundTripJSON(t, raw)
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance round-trip mismatch:\n  want: %s\n   got: %s", want, got)
	}
}

// TestConformance_TasksServerCapability asserts the capabilities.tasks
// block round-trips byte-stably for the full-capability shape (list,
// cancel, tools/call all advertised).
//
// Source: docs/specifications/mcp-tasks-experimental.mdx, section
// "Capability Negotiation".
func TestConformance_TasksServerCapability(t *testing.T) {
	t.Parallel()
	want := loadFixture(t, "tasks_server_capability.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionMCP20251125)
	dec, ok, err := c.DecodeTasksServerCapability(want)
	if err != nil {
		t.Fatalf("DecodeTasksServerCapability: %v", err)
	}
	if !ok {
		t.Fatalf("DecodeTasksServerCapability: !ok")
	}
	raw, err := c.EncodeTasksServerCapability(dec)
	if err != nil {
		t.Fatalf("EncodeTasksServerCapability: %v", err)
	}
	got := roundTripJSON(t, raw)
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance round-trip mismatch:\n  want: %s\n   got: %s", want, got)
	}
}

// TestConformance_ListTasksResult_EmptyArrayNotNull asserts an empty
// ListTasksResult emits `"tasks":[]`, never `"tasks":null` — the spec's
// PaginatedResult-derived shape requires an array. Conformance proves
// the codec does not regress to JSON null on the empty case.
//
// Source: docs/specifications/mcp-tasks-experimental.mdx, section
// "Listing Tasks".
func TestConformance_ListTasksResult_EmptyArrayNotNull(t *testing.T) {
	t.Parallel()
	want := loadFixture(t, "list_tasks_result_empty.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionMCP20251125)
	raw, err := c.EncodeListTasksResult(protocolcodec.ListTasksResult{})
	if err != nil {
		t.Fatalf("EncodeListTasksResult: %v", err)
	}
	got := roundTripJSON(t, raw)
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance round-trip mismatch:\n  want: %s\n   got: %s", want, got)
	}
	// And a decode of the canonical empty form must succeed and round-trip.
	dec, err := c.DecodeListTasksResult(want)
	if err != nil {
		t.Fatalf("DecodeListTasksResult: %v", err)
	}
	if dec.Tasks == nil || len(dec.Tasks) != 0 {
		t.Fatalf("decoded empty ListTasksResult: Tasks=%v, want empty slice", dec.Tasks)
	}
}

// TestConformance_RelatedTaskMeta asserts the
// `io.modelcontextprotocol/related-task` association key round-trips
// byte-stably — the key Dockyard stamps on tool-result envelopes so a
// requestor can correlate the result with its task.
//
// Source: docs/specifications/mcp-tasks-experimental.mdx, section
// "Associating Task-Related Messages".
func TestConformance_RelatedTaskMeta(t *testing.T) {
	t.Parallel()
	want := loadFixture(t, "related_task_meta.json")

	c := protocolcodec.CodecFor(protocolcodec.VersionMCP20251125)

	var meta protocolcodec.Meta
	if err := json.Unmarshal(want, &meta); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	id, ok, err := c.DecodeRelatedTaskMeta(meta)
	if err != nil {
		t.Fatalf("DecodeRelatedTaskMeta: %v", err)
	}
	if !ok {
		t.Fatalf("DecodeRelatedTaskMeta: !ok")
	}
	if id != "task_7f3a" {
		t.Fatalf("DecodeRelatedTaskMeta id = %q, want task_7f3a", id)
	}
	out, err := c.EncodeRelatedTaskMeta(nil, id)
	if err != nil {
		t.Fatalf("EncodeRelatedTaskMeta: %v", err)
	}
	outRaw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal re-encoded: %v", err)
	}
	got := roundTripJSON(t, outRaw)
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance round-trip mismatch:\n  want: %s\n   got: %s", want, got)
	}
}

// TestConformance_VersionedCodecSelection asserts the codec registry is
// keyed on the negotiated protocolVersion, per the RFC §16 forward-
// compatibility rule. An unknown version surfaces a typed error from
// CodecForStrict — never a silent fallback that could mask a spec
// regression in a downstream caller (`dockyard validate`).
//
// Source: docs/specifications/ — the version-keyed selection is the
// CLAUDE.md §10 "Codecs are versioned and keyed on the negotiated
// `protocolVersion`" rule, made concrete here.
func TestConformance_VersionedCodecSelection(t *testing.T) {
	t.Parallel()

	known := []protocolcodec.ProtocolVersion{
		protocolcodec.VersionMCP20251125,
		protocolcodec.VersionApps20260126,
	}

	// Every known version selects a non-nil codec whose Version() reports
	// the same version it was keyed under.
	for _, v := range known {
		c, err := protocolcodec.CodecForStrict(v)
		if err != nil {
			t.Fatalf("CodecForStrict(%q): %v", v, err)
		}
		if c == nil {
			t.Fatalf("CodecForStrict(%q) returned nil", v)
		}
		if c.Version() != v {
			t.Fatalf("CodecForStrict(%q).Version() = %q, want %q", v, c.Version(), v)
		}
	}

	// An unknown version (neither old nor new) surfaces a typed error.
	unknowns := []protocolcodec.ProtocolVersion{
		"1999-01-01",
		"3000-12-31",
		"not-a-version",
		"",
	}
	for _, u := range unknowns {
		_, err := protocolcodec.CodecForStrict(u)
		if err == nil {
			t.Fatalf("CodecForStrict(%q): want ErrUnknownVersion, got nil", u)
		}
		if !errors.Is(err, protocolcodec.ErrUnknownVersion) {
			t.Fatalf("CodecForStrict(%q): err class = %v, want ErrUnknownVersion", u, err)
		}
	}

	// The graceful-degradation CodecFor (non-strict) MUST always return a
	// non-nil codec — RFC §16 item 7 — even for an unknown version.
	for _, u := range unknowns {
		c := protocolcodec.CodecFor(u)
		if c == nil {
			t.Fatalf("CodecFor(%q) returned nil — graceful degradation violated", u)
		}
		// It must report the DefaultVersion (the registry's fallback).
		if c.Version() != protocolcodec.DefaultVersion {
			t.Fatalf("CodecFor(%q).Version() = %q, want DefaultVersion %q",
				u, c.Version(), protocolcodec.DefaultVersion)
		}
	}

	// And the known set MUST include the versions Dockyard's own runtime
	// + apps + tasks layers exercise — no silent removal.
	got := protocolcodec.KnownVersions()
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	wantSet := append([]protocolcodec.ProtocolVersion(nil), known...)
	sort.Slice(wantSet, func(i, j int) bool { return wantSet[i] < wantSet[j] })
	if !reflect.DeepEqual(got, wantSet) {
		t.Fatalf("KnownVersions = %v, want %v — silent registry change detected", got, wantSet)
	}
}

// TestConformance_DefaultVersionIsCurrentStable pins the DefaultVersion
// to the current stable MCP spec version (RFC §16 item 6 — V1 targets
// 2025-11-25). A change to this is a deliberate spec-bump signal.
//
// Source: docs/specifications/, RFC §16 item 6.
func TestConformance_DefaultVersionIsCurrentStable(t *testing.T) {
	t.Parallel()
	if got, want := protocolcodec.DefaultVersion, protocolcodec.VersionMCP20251125; got != want {
		t.Fatalf("DefaultVersion = %q, want %q — RFC §16 item 6", got, want)
	}
}

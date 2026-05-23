package inspector

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestFixturesFromDir_Empty exercises the graceful-empty paths: an unset dir,
// a non-existent fixtures/ subtree, and a fixtures/ that is a file rather
// than a directory all yield an empty slice and a nil error (the inspector
// falls back to the schema-derived synthetic fixtures).
func TestFixturesFromDir_Empty(t *testing.T) {
	t.Parallel()
	t.Run("empty dir", func(t *testing.T) {
		t.Parallel()
		got, err := FixturesFromDir("")()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %d", len(got))
		}
	})
	t.Run("no fixtures subtree", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		got, err := FixturesFromDir(dir)()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %d", len(got))
		}
	})
	t.Run("fixtures is a file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "fixtures"), []byte("not a dir"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := FixturesFromDir(dir)()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %d", len(got))
		}
	})
}

// TestFixturesFromDir_LoadsHappy proves the loader reads a happy fixture and
// derives a structuredContent payload with the expected `kind` and `state`
// when no `output_override` is present.
func TestFixturesFromDir_LoadsHappy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	toolDir := filepath.Join(dir, "fixtures", "create_chart")
	if err := os.MkdirAll(toolDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{
		"name": "happy",
		"description": "Revenue by month.",
		"state": "ready",
		"input": {
			"type": "bar",
			"title": "Revenue by month",
			"data": {
				"categories": ["Jan", "Feb"],
				"series": [{ "name": "Revenue", "values": [100, 200] }]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(toolDir, "happy.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := FixturesFromDir(dir)()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 fixture, got %d", len(got))
	}
	f := got[0]
	if f.Tool != "create_chart" {
		t.Fatalf("tool: %q", f.Tool)
	}
	if f.Kind != FixtureHappy {
		t.Fatalf("kind: %q", f.Kind)
	}
	if f.State != "ready" {
		t.Fatalf("state: %q", f.State)
	}
	if f.StructuredContent["kind"] != "chart" {
		t.Fatalf("derived kind: %v", f.StructuredContent["kind"])
	}
	if f.StructuredContent["state"] != "ready" {
		t.Fatalf("derived state: %v", f.StructuredContent["state"])
	}
	if f.StructuredContent["theme"] != "auto" {
		t.Fatalf("default theme: %v", f.StructuredContent["theme"])
	}
	if f.StructuredContent["title"] != "Revenue by month" {
		t.Fatalf("title pass-through: %v", f.StructuredContent["title"])
	}
}

// TestFixturesFromDir_OutputOverride proves that when a fixture ships an
// `output_override` block, the loader uses it verbatim (it does NOT
// re-derive from input — the override is a deliberate, hand-curated payload
// for the non-happy states like error/permission).
func TestFixturesFromDir_OutputOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	toolDir := filepath.Join(dir, "fixtures", "create_chart")
	if err := os.MkdirAll(toolDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{
		"name": "error",
		"state": "error",
		"input": { "type": "bar", "data": { "series": [] } },
		"output_override": {
			"kind": "chart",
			"type": "bar",
			"title": "Backend offline",
			"data": { "series": [] },
			"theme": "auto",
			"state": "error",
			"message": "The analytics service is unreachable."
		}
	}`
	if err := os.WriteFile(filepath.Join(toolDir, "error.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := FixturesFromDir(dir)()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 fixture, got %d", len(got))
	}
	want := map[string]any{
		"kind":    "chart",
		"type":    "bar",
		"title":   "Backend offline",
		"data":    map[string]any{"series": []any{}},
		"theme":   "auto",
		"state":   "error",
		"message": "The analytics service is unreachable.",
	}
	if !reflect.DeepEqual(got[0].StructuredContent, want) {
		t.Fatalf("structuredContent mismatch:\n got: %v\nwant: %v", got[0].StructuredContent, want)
	}
}

// TestFixturesFromDir_DeterministicOrder proves the loader returns fixtures
// in a stable (tool ASC, kind in the well-known order) ordering — the
// frontend picker relies on it.
func TestFixturesFromDir_DeterministicOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Two tools, two kinds each — write them in non-natural order to prove
	// the loader sorts, not just iterates filesystem order.
	for _, p := range []string{
		"create_table/large.json",
		"create_chart/empty.json",
		"create_table/happy.json",
		"create_chart/happy.json",
	} {
		full := filepath.Join(dir, "fixtures", p)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(`{"input":{}}`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	got, err := FixturesFromDir(dir)()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantOrder := []struct {
		Tool string
		Kind FixtureKind
	}{
		{"create_chart", FixtureHappy},
		{"create_chart", FixtureEmpty},
		{"create_table", FixtureHappy},
		{"create_table", FixtureLarge},
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("count: got %d want %d", len(got), len(wantOrder))
	}
	for i, w := range wantOrder {
		if got[i].Tool != w.Tool || got[i].Kind != w.Kind {
			t.Fatalf("order[%d]: got (%s, %s) want (%s, %s)",
				i, got[i].Tool, got[i].Kind, w.Tool, w.Kind)
		}
	}
}

// TestFixturesFromDir_MalformedIsTypedError proves a malformed fixture JSON
// surfaces a typed wrapped error so the developer notices, rather than
// silently disappearing.
func TestFixturesFromDir_MalformedIsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	toolDir := filepath.Join(dir, "fixtures", "create_chart")
	if err := os.MkdirAll(toolDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, "happy.json"), []byte(`{ not json }`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := FixturesFromDir(dir)()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("expected wrapped json.SyntaxError, got %v", err)
	}
}

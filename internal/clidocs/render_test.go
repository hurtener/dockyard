package clidocs

import (
	"bytes"
	"strings"
	"testing"
)

// TestRender_ContainsEveryShippedVerb asserts that the rendered Markdown
// names every Dockyard CLI verb. A new verb landing without showing up in
// the docs site's CLI reference is the §19 hygiene drift this renderer
// closes (D-140).
func TestRender_ContainsEveryShippedVerb(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := buf.String()
	wantSubstrings := []string{
		"# CLI reference",
		"## `dockyard`",
		"## `dockyard build`",
		"## `dockyard dev`",
		"## `dockyard generate`",
		"## `dockyard inspect`",
		"## `dockyard install`",
		"## `dockyard new`",
		"## `dockyard run`",
		"## `dockyard test`",
		"## `dockyard validate`",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(got, s) {
			t.Errorf("rendered output missing %q", s)
		}
	}
}

// TestRender_OmitsHelpAndCompletion confirms the renderer skips cobra's
// built-in `help` and `completion` commands — they add noise and have no
// Dockyard-specific behaviour.
func TestRender_OmitsHelpAndCompletion(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := buf.String()
	for _, s := range []string{"## `dockyard help`", "## `dockyard completion`"} {
		if strings.Contains(got, s) {
			t.Errorf("rendered output should not contain %q", s)
		}
	}
}

// TestRender_DeterministicOutput asserts two consecutive renders of the
// same command tree produce identical bytes. The docs build relies on
// this so a no-op rerun produces no diff.
func TestRender_DeterministicOutput(t *testing.T) {
	t.Parallel()
	var a, b bytes.Buffer
	if err := Render(&a); err != nil {
		t.Fatalf("Render a: %v", err)
	}
	if err := Render(&b); err != nil {
		t.Fatalf("Render b: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatalf("Render is not deterministic")
	}
}

// TestRender_EscapesAngleBrackets confirms that the renderer escapes the
// `<` and `>` characters in flag descriptions and defaults so VitePress's
// Vue compiler does not mis-parse them as HTML tag starts.
func TestRender_EscapesAngleBrackets(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := buf.String()
	// `dockyard new` documents `<name>` in its flag help; the escaped form
	// `&lt;name&gt;` is what must appear in the rendered table.
	if !strings.Contains(got, "&lt;") {
		t.Error("rendered output is missing escaped angle brackets — VitePress build would fail")
	}
	// Inversely, a raw `<name>` in a table cell would fail the Vue parser.
	// The Use lines outside tables are kept verbatim — we look for the
	// escape inside a row marker.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "| ") && strings.Contains(line, "<") &&
			!strings.Contains(line, "&lt;") {
			t.Errorf("rendered table cell carries an unescaped `<`: %q", line)
		}
	}
}

// TestRender_OmitsHiddenFlags asserts the renderer filters cobra flags
// marked Hidden — notably `dockyard new --dockyard-path`, which is the
// pre-publish replace seam ([D-080]) and `MarkHidden`-ed from the public
// CLI surface.
func TestRender_OmitsHiddenFlags(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(buf.String(), "--dockyard-path") {
		t.Error("rendered output exposes the hidden --dockyard-path flag")
	}
}

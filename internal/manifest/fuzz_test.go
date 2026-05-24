package manifest

import (
	"bytes"
	"testing"
)

// This file holds the Phase 21.5 fuzz target for the dockyard.app.yaml loader —
// the parse surface every Dockyard project's manifest passes through.
// Invariant: Load never panics on arbitrary bytes, and on a successful parse
// the returned Manifest is non-nil with no malformed value (a parse that
// succeeds must produce a usable Manifest, never nil-with-nil-error).
//
// A parse error on malformed input is correct behaviour — Load is documented
// to fail on bad YAML, an unknown field, or a structural fault. The fuzzer
// only flags a panic or a nil/nil result.
//
// CI runs the seed corpus as ordinary tests. For a longer local session:
//
//	go test ./internal/manifest -run '^$' -fuzz FuzzLoad -fuzztime 60s

// FuzzLoad fuzzes the manifest loader with arbitrary byte input.
func FuzzLoad(f *testing.F) {
	// Seed corpus: a minimal valid manifest, a full one, and several malformed
	// shapes the loader must reject without panicking. Phase 27 extends the
	// corpus with adversarial YAML shapes: deeply-nested maps, oversized
	// fields, escape sequences, anchor abuse, and explicit tags.
	f.Add([]byte("name: s\ntitle: S\nversion: 1.0.0\nruntime:\n  transports: [stdio]\n" +
		"tools:\n  - name: echo\n    description: e\n    input: internal/contracts.In\n    output: internal/contracts.Out\n"))
	f.Add([]byte(""))
	f.Add([]byte("not: yaml: : :"))
	f.Add([]byte("name: x"))
	f.Add([]byte("unknown_top_level_field: true"))
	f.Add([]byte("name: x\nruntime:\n  transports: [bogus-transport]\n"))
	f.Add([]byte("- just\n- a\n- list\n"))
	f.Add([]byte("\x00\x01\x02 binary noise"))
	f.Add([]byte("tools: 12345"))
	// Phase 27 hostile-input additions:
	// Deeply-nested map (a YAML billion-laughs-style attack would balloon
	// memory if the loader weren't bounded — the YAML library bounds it; the
	// fuzzer asserts no panic regardless).
	f.Add([]byte("name: x\nnest: &a {a: &b {b: &c {c: *a}}}\n"))
	// Oversized field — name padded with 256KB of A characters.
	f.Add([]byte("name: " + repeatLocal("A", 262144) + "\n"))
	// Anchor referencing itself (a degenerate alias chain).
	f.Add([]byte("&self {ref: *self}\n"))
	// Explicit tag the schema does not know.
	f.Add([]byte("name: !!binary x\n"))
	// Embedded control characters in a string field — must not corrupt the
	// error path.
	f.Add([]byte("name: \"x\\u0000\\u0001embedded\"\n"))
	// A duplicate top-level key — yaml.v3 KnownFields(true) treats it as an
	// error; the fuzzer asserts the loader stays sane.
	f.Add([]byte("name: a\nname: b\n"))

	f.Fuzz(func(t *testing.T, raw []byte) {
		m, err := Load(bytes.NewReader(raw), "fuzz")
		if err != nil {
			// A parse error must come with a nil Manifest — never a partial
			// value handed back alongside an error.
			if m != nil {
				t.Fatalf("Load returned a non-nil Manifest alongside an error: %v", err)
			}
			return
		}
		// A nil error must come with a usable Manifest.
		if m == nil {
			t.Fatalf("Load returned nil Manifest with nil error")
		}
		// A successfully-parsed manifest has a non-empty name (structural
		// validation guarantees it) — assert the invariant holds.
		if m.Name == "" {
			t.Fatalf("Load succeeded but Manifest.Name is empty")
		}
	})
}

// repeatLocal is the in-test analog of strings.Repeat — keeps the fuzz file
// import-light (gofmt-clean across edits).
func repeatLocal(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

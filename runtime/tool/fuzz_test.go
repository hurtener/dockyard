package tool_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/tool"
)

// This file holds the Phase 21.5 fuzz target for the JSON-RPC tool-argument
// frame-parsing path — the edge-validation seam where a tool call's raw,
// undecoded JSON `arguments` frame is parsed and validated against the tool's
// generated input JSON Schema before the typed handler runs (RFC §6.3).
//
// This is the parse surface that sees attacker-influenced bytes off the wire:
// a malformed `arguments` value must become a typed ArgumentError, never a
// panic. The invariant under fuzz is exactly that — validateArgs never panics
// on arbitrary `arguments` bytes; it either accepts the frame or returns a
// typed error.
//
// CI runs the seed corpus as ordinary tests. For a longer local session:
//
//	go test ./runtime/tool -run '^$' -fuzz FuzzToolArgumentFrame -fuzztime 60s

// FuzzToolArgumentFrame fuzzes the raw tool-argument frame parser.
func FuzzToolArgumentFrame(f *testing.F) {
	// Seed corpus: a valid frame, malformed JSON, wrong-typed values, and
	// frames that violate the schema. None may panic. Phase 27 adds
	// hostile-input shapes: oversized strings, deeply-nested objects, and
	// malicious unicode forms.
	f.Add(`{"period":"2026-Q1"}`)
	f.Add(`{}`)
	f.Add(``)
	f.Add(`null`)
	f.Add(`{"period":42}`)
	f.Add(`{"period":"x","extra":true}`)
	f.Add(`[1,2,3]`)
	f.Add(`{"period":`)
	f.Add(`{"period":"\ud800"}`) // lone surrogate
	f.Add("\x00\x01malformed")
	// Phase 27 hostile-input additions:
	f.Add(`{"period":"` + strings.Repeat("A", 4096) + `"}`)             // oversized string
	f.Add(`{"period":{"nested":{"again":{"deeper":{"deepest":1}}}}}`)   // deep nesting
	f.Add(`{"period":"\\\\\\\\\\\\"}`)                                  // escape spam
	f.Add("{\"period\":\"\u202e\u2066rtl-override\"}")                  // bidi override (escape form per gosec G116)
	f.Add(`{"period":"x","period":"y"}`)                                // duplicate keys
	f.Add("{\"\x00period\":\"x\"}")                                     // NUL in key
	f.Add(strings.Repeat(`{"period":"x"},`, 256) + `{"period":"last"}`) // batch-shaped

	tr := tool.NewHandlerRuntimeForTest(f)
	f.Fuzz(func(_ *testing.T, rawArgs string) {
		// Invariant: no panic on any input. Both an accept and a typed
		// ArgumentError are valid outcomes — only a panic is a failure.
		_ = tr.ValidateForTest(context.Background(), rawArgs)
	})
}

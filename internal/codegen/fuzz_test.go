package codegen

import "testing"

// This file holds the Phase 21.5 fuzz targets for the codegen text-parsing
// surface. The contract-first pipeline takes Go source as input in two places:
// EnumsFromSource scans a contract file's const blocks for named-constant
// types, and TypeScriptForSource parses a contract file's type declarations.
// Both run Go's parser on caller-supplied text — the invariant under fuzz is
// that neither panics on arbitrary input (a parse error is correct), and a
// successful TypeScriptForSource produces non-empty output.
//
// CI runs the seed corpus as ordinary tests. For a longer local session:
//
//	go test ./internal/codegen -run '^$' -fuzz FuzzTypeScriptForSource -fuzztime 60s

// FuzzEnumsFromSource fuzzes the enum-scanner with arbitrary Go-ish source.
// Invariant: no panic; a parse error on malformed source is acceptable.
func FuzzEnumsFromSource(f *testing.F) {
	f.Add("package contracts\n\ntype Status string\n\nconst (\n\tActive Status = \"active\"\n)\n")
	f.Add("package p\n")
	f.Add("")
	f.Add("not go source at all")
	f.Add("package p\nconst X = 1 + ")
	f.Add("package p\ntype T int\nconst (\n\tA T = iota\n\tB\n)\n")
	// Phase 27 hostile-input additions — the parser is fed adversarial Go-ish
	// source that exercises the recover guard's regression set (D-094):
	f.Add("package p\nconst ( \n\tA = \nB \n) \n")
	f.Add("package p\nconst (\n\tA = `unterminated raw")
	f.Add("\x00\x01package p\nconst (A = 1)")
	f.Add("package p\nconst ( A , B = 1 , 2 )") // grouped enum on one line
	f.Add("package p\nconst ( A int = iota; B; C \"x\" )")

	f.Fuzz(func(_ *testing.T, src string) {
		opts, err := EnumsFromSource(src)
		if err != nil {
			return // a parse error on malformed source is correct.
		}
		// A clean parse must return a usable (possibly empty) option slice,
		// never nil-with-nil-error sliced into a panic later.
		_ = opts
	})
}

// FuzzTypeScriptForSource fuzzes the Go→TypeScript source parser. Invariant:
// no panic; a successful generation yields non-empty TypeScript.
func FuzzTypeScriptForSource(f *testing.F) {
	f.Add("package contracts\n\ntype In struct {\n\tName string `json:\"name\"`\n}\n")
	f.Add("package p\n\ntype T struct {\n\tN int `json:\"n\"`\n\tOpt *string `json:\"opt,omitempty\"`\n}\n")
	f.Add("package p\n")
	f.Add("")
	f.Add("garbage input }{")
	f.Add("package p\ntype A struct{ B B }\ntype B struct{ A A }\n")
	// Phase 27 hostile-input additions — adversarial source the tygo
	// recover guard must contain (D-094):
	f.Add("package p\ntype T struct {\n\tF map[string]map[string]map[string]map[string]int\n}\n")
	f.Add("package p\ntype Generic[T any] struct{ V T }\n") // type parameters
	f.Add("package p\ntype T struct {\n\tF func(a, b, c, d, e, f int) (int, error)\n}\n")
	f.Add("package p\ntype T struct {\n\tF interface{ M() error }\n}\n")
	f.Add("package p\ntype T struct {\n\tF [1024][1024]int\n}\n")

	f.Fuzz(func(t *testing.T, src string) {
		out, err := TypeScriptForSource(src)
		if err != nil {
			return // a parse / generation error on malformed source is correct.
		}
		if len(out) == 0 {
			t.Fatalf("TypeScriptForSource returned empty output with nil error for input %q", src)
		}
	})
}

// Package codegen turns Go contract structs into downstream artifacts — the
// spine of Dockyard's contract-first property (P1, RFC §6.1).
//
// A tool's input and output are typed Go structs; they are the single source of
// truth. JSON Schema and TypeScript types are generated from them, never
// hand-authored (AGENTS.md §6). The package implements the Design A pipeline
// (RFC §6.2, brief 06 §3.1):
//
//	                       ┌─ codegen.SchemaFor       ─► JSON Schema  (Phase 04)
//	contract struct ───────┤
//	                       └─ codegen.TypeScriptFor*  ─► TypeScript   (Phase 05)
//
// Both generators read Go directly; there is no Node dependency and the two
// halves never share an intermediate format, so a bug in one cannot silently
// desync the other.
//
// # JSON Schema (Phase 04)
//
// SchemaFor infers a schema; Marshal serializes it deterministically (sorted
// keys) so regeneration is byte-stable. The schema engine is
// github.com/google/jsonschema-go — deliberately the same engine the official
// MCP SDK uses internally (brief 06 §2.3). Picking any other library would
// create a divergent schema dialect; Dockyard standardizes on this one.
//
// # TypeScript (Phase 05)
//
// TypeScriptForSource and TypeScriptForDir convert Go contract source into
// deterministic TypeScript via github.com/gzuidhof/tygo, an AST-based pure-Go
// generator that preserves doc comments, enums and constants (brief 06 §2.4).
// The output carries a "Code generated ... DO NOT EDIT." header and is pinned by
// golden tests.
//
// # Drift cross-check (Phase 05)
//
// Because schema and TypeScript are generated independently, CrossCheck
// cross-verifies that the two artifacts for one contract describe the same
// property set with consistent optionality, and CheckStale verifies that
// generated output on disk still matches a fresh regeneration of its Go source.
// Both hard-fail (RFC §6.2, brief 06 R1) — they are the seam Phase 18's
// `dockyard validate` command calls. Stale generated output is a build blocker,
// never a warning.
package codegen

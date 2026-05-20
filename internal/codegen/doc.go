// Package codegen turns Go contract structs into downstream artifacts — the
// spine of Dockyard's contract-first property (P1, RFC §6.1).
//
// A tool's input and output are typed Go structs; they are the single source of
// truth. JSON Schema and TypeScript types are generated from them, never
// hand-authored (AGENTS.md §6). Phase 04 ships the JSON Schema half of the
// Design A pipeline (RFC §6.2, brief 06 §3.1):
//
//	                       ┌─ codegen.SchemaFor ─► JSON Schema  (this package)
//	contract struct ───────┤
//	                       └─ tygo  ─────────────► TypeScript   (Phase 05)
//
// Both generators read Go directly; there is no Node dependency and the two
// halves never share an intermediate format, so a bug in one cannot silently
// desync the other — Phase 05's `dockyard validate` cross-checks them.
//
// The schema engine is github.com/google/jsonschema-go — deliberately the same
// engine the official MCP SDK uses internally (brief 06 §2.3). Picking any other
// library would create a divergent schema dialect; Dockyard standardizes on this
// one.
//
// SchemaFor infers a schema; Marshal serializes it deterministically (sorted
// keys) so regeneration is byte-stable and golden tests catch any drift.
package codegen

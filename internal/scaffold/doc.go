// Package scaffold implements `dockyard new` — the no-template project
// scaffold (RFC §9.1, §10).
//
// The no-template path is the first-class one (RFC §10): `dockyard new <name>`
// with no --template produces a blank but working MCP server — a manifest, one
// example contract-first tool, the generated contract artifacts, a runnable
// main, and a test. Templates (analytical-card, approval-flow, inspector) are
// optional product-pattern showcases layered on later (Wave 9); this package
// owns only the blank scaffold.
//
// Contract-first by construction (P1, RFC §6.1). The example tool's input and
// output are typed Go structs in the scaffolded project's internal/contracts
// package. Their JSON Schema and TypeScript artifacts are GENERATED here, by
// internal/codegen, from those structs — never hand-written. The scaffold
// emits the generated files carrying the `Code generated … DO NOT EDIT.`
// header so a developer who runs `dockyard generate` (Phase 18) gets identical
// output: the scaffold is just the first generate.
//
// Determinism. Generate writes byte-deterministic output for a fixed Options —
// the same project name always yields the same tree. That is what makes the
// golden test in scaffold_golden_test.go meaningful: an accidental change to a
// scaffolded file fails CI as a visible diff.
package scaffold

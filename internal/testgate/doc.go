// Package testgate is the engine behind `dockyard test` — the contract +
// compliance gate (RFC §9.1, §9.4).
//
// Run executes, against one Dockyard project, every test category Dockyard's
// quality bar is built on:
//
//   - go-test          — the project's own Go unit tests (`go test ./...`).
//   - contract         — the contract-first assertions: the generated JSON
//     Schema and TypeScript still match the Go contract structs (P1).
//   - golden           — the project's fixture / golden snapshots are current.
//   - spec-compliance  — the project's Apps/Tasks constructs conform to the
//     vendored MCP specs (CLAUDE.md §11 — vendored specs, never a live host).
//   - capability       — the project degrades gracefully across host
//     capability sets (RFC §7.5; no hardcoded host matrix, CLAUDE.md §6).
//
// A category is either gating — a regression exits the process non-zero — or
// informational. Run composes the existing seams (internal/validate.Run,
// internal/generate, internal/codegen, runtime/apps) rather than reimplementing
// them: the gate is defined once.
//
// The cobra `dockyard test` command (internal/cli) is a thin wrapper over Run —
// the orchestration is a testable package, not logic buried in a RunE closure.
package testgate

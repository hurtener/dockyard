// Package validate is the engine behind `dockyard validate` — the quality-gate
// verb (RFC §9.4).
//
// It runs the project-level quality checks the RFC §9.4 taxonomy defines and
// reports them as a structured Report of Diagnostics, each carrying a Severity.
// A Blocker diagnostic is a build blocker — `dockyard validate` exits non-zero
// when the Report carries any. A Warning is reported but does not change the
// exit code.
//
// # The checks
//
// Run performs, in order:
//
//   - manifest — dockyard.app.yaml is schema-valid and structurally well-formed
//     (delegated to internal/manifest, which checks required fields, enums,
//     uniqueness, tool↔UI cross-references and Tasks-limit coherence);
//   - schema — every generated <tool>_<side>.schema.json is present and is a
//     valid, resolvable JSON Schema;
//   - tool↔UI mapping — every tool that declares a ui:// resource maps to a
//     real apps[] entry and vice versa, and each App's .svelte entry file
//     exists on disk;
//   - MIME — each App resource carries the single MVP Apps MIME type
//     (text/html;profile=mcp-app) and a well-formed ui:// URI;
//   - spec compliance — Apps/Tasks manifest constructs are checked against the
//     vendored MCP specs in docs/specifications/, never a live host
//     (CLAUDE.md §11);
//   - UI states — the four-state page rule (CLAUDE.md §20): when a quality.*
//     gate is opted in, the App's .svelte source is checked for the required
//     loading / empty / error / permission state markers;
//   - stale codegen — the generated schema and TypeScript on disk are compared
//     byte-for-byte against a fresh regeneration (internal/codegen.CheckStale).
//     Stale generated output is a build blocker (RFC §6.2, P1).
//   - cross-codegen — the committed schema and the committed TypeScript on
//     disk are cross-checked with internal/codegen.CrossCheck so a hand-edited
//     `contracts.ts` that disagrees with the schema is a build blocker (RFC §6.2,
//     P1). `validate` deliberately checks what is checked in, not a fresh
//     regeneration; this gates a desync that, at the validate-standalone or
//     `dockyard test` entrypoint, has not been rewritten by a regeneration
//     step. (`dockyard build` runs a regeneration BEFORE invoking the validate
//     gate, so at build time a hand-edited drift is repaired rather than
//     reported — see D-113. Either way the build artifact upholds P1.)
//
// # Reusable seam
//
// Run is exported so `dockyard build` (Phase 20) and `dockyard test` (Phase 21)
// invoke the same gate the `dockyard validate` verb wraps — the validation
// logic lives here, not in the CLI command. Run builds fresh state per call and
// holds no shared mutable state.
package validate

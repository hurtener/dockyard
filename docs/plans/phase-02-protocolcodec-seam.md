# Phase 02 — `protocolcodec` seam + vendored specs

## Summary

This phase delivers `internal/protocolcodec`, Dockyard's single isolation seam
for every MCP extension wire format, and vendors the two MCP extension specs
Dockyard consumes into `docs/specifications/`. The package provides versioned
codecs keyed on the negotiated `protocolVersion` and typed `_meta` accessors for
the MCP Apps and MCP Tasks extensions; the deprecated flat
`_meta["ui/resourceUri"]` form is tolerated on read and never emitted. A
mechanical test enforces that no package other than `internal/protocolcodec`
imports or constructs raw extension wire types.

## RFC anchor

- RFC §5.4 — the `protocolcodec` isolation seam (settled).
- RFC §16 — forward-compatibility strategy (one seam, vendored snapshots,
  versioned codecs, code-generated/regenerate-and-diff Tasks wire layer).

Supporting context: RFC §7 (Apps `_meta` shapes), RFC §8 (Tasks wire layer).

## Briefs informing this phase

- brief 01 — MCP Apps extension.
- brief 02 — MCP Tasks extension.
- brief 03 — go-sdk audit.

## Brief findings incorporated

- **brief 01 §2.3** — Tools link to UI resources via the nested `_meta.ui`
  object (`resourceUri`, `visibility`); a *deprecated flat* form
  `_meta["ui/resourceUri"]` also exists. "Dockyard should emit only the nested
  form but tolerate reading the flat form." The codec's `DecodeAppsToolMeta`
  reads the nested form, falls back to the flat form, and `EncodeAppsToolMeta`
  emits only the nested form and actively strips the flat key.
- **brief 01 §2.2** — `_meta.ui.csp` / `_meta.ui.domain` are read from the
  `resources/read` response, not only the static resource declaration.
  `AppsResourceMeta` is therefore one domain type covering both the resource
  declaration and the read-response content; the seam is the single choke point.
- **brief 02 §2.3 + §4 risk 2** — The authoritative Tasks surface is the
  `experimental-ext-tasks` `schema/draft/schema.ts`, which is experimental; the
  `/overview` page lags it (documents a non-existent `tasks/update`). This phase
  vendors `schema.ts` (not the overview page) and isolates every Tasks wire type
  behind the seam so a spec revision is regenerate-and-diff.
- **brief 02 §2.3** — `Task.ttl` is `number | null` and the schema requires the
  field to be *present*; `null` means unlimited. `taskWire.TTL` is `*int64`
  **without** `omitempty`, so a nil TTL marshals to explicit JSON `null`.
- **brief 03 R7** — `_meta` is untyped (`map[string]any`), offering no
  compile-time safety; Dockyard should wrap `_meta` access in typed helpers so
  extension-metadata bugs surface in Dockyard's own validation. The codec
  exposes typed domain accessors and returns a wrapped `ErrMalformedMeta` on a
  shape mismatch instead of panicking or dropping data.

## Findings I'm departing from (if any)

- **brief 02 §3 sketch — "field names should be code-generated from the upstream
  schema."** RFC §8.2 / §16 item 4 likewise describe the Tasks wire layer as
  "code-generated from the vendored schema." Phase 02 vendors the schema and
  hand-writes the Go wire types for the small `_meta`-borne and capability
  surface this phase needs, rather than standing up a TypeScript-schema → Go
  code generator now. The generator is a contracts-pipeline concern (Wave 2,
  Phase 04, RFC §6.2) and the V1 Tasks runtime lands in Wave 5 (Phase 16+); the
  full method-envelope wire layer is generated then. The hand-written types in
  this phase are pinned to the vendored snapshot, carry a `regenerate-and-diff`
  note, and are guarded by golden tests, so the forward-compatibility property
  is preserved. Filed as **D-024**.

## Goals

- A single internal package, `internal/protocolcodec`, that owns every MCP
  extension wire format Dockyard consumes.
- Versioned `Codec` selection keyed on the negotiated `protocolVersion`, with a
  graceful default and a strict variant for tooling.
- Typed, round-trippable `_meta` accessors and capability encoders for the MCP
  Apps and MCP Tasks extensions.
- The two MCP extension specs vendored into `docs/specifications/`, pinned by
  upstream commit SHA + date.
- A mechanical guard (a test) that the seam is the only importer/constructor of
  raw extension wire types.

## Non-goals

- The MCP Apps runtime layer (`runtime/apps/`) — Wave 4.
- The MCP Tasks runtime, `tasks/*` method routing, and the `TaskStore` —
  Wave 5 / Phase 03.
- A TypeScript-schema → Go code generator for the Tasks wire layer — deferred
  to the contracts pipeline (Phase 04) and the Tasks runtime (Wave 5); see
  "Findings I'm departing from" and D-024.
- The `postMessage`/iframe bridge dialect — that is client-side (the bridge
  shell library and the inspector), not a `protocolcodec` concern.
- Host-profile derivation of `_meta.ui.domain` — RFC §7.5 / D-012; the seam
  carries the field verbatim.

## Acceptance criteria

- [x] `internal/protocolcodec` exists and is the sole importer of raw MCP
      extension wire types.
- [x] Codecs round-trip the MCP Apps `_meta` shapes (`_meta.ui` on tools and on
      resources) and the MCP Tasks `_meta` shapes (`related-task`,
      `model-immediate-response`) plus the `Task` object and the Apps/Tasks
      capability blocks.
- [x] The deprecated flat `_meta["ui/resourceUri"]` form is tolerated on read
      and **never** emitted by an encoder (proven by a test that decodes the
      flat form, re-encodes, and asserts the flat key is absent).
- [x] No package other than `internal/protocolcodec` imports or hand-writes raw
      extension wire types — enforced by `TestNoRawWireTypeImportsOutsideSeam`
      and `TestNoExtensionMetaKeysOutsideSeam`.
- [x] The MCP Apps spec (revision 2026-01-26, SEP-1865) and the MCP Tasks
      experimental schema (SEP-1686/2663) are vendored into
      `docs/specifications/`, each with a header naming the upstream URL +
      pinned commit SHA + date.
- [x] Codecs are versioned and selected on `protocolVersion`; an unknown
      version degrades to the default codec (`CodecFor`) or errors
      (`CodecForStrict`).

## Files added or changed

```text
internal/protocolcodec/
  doc.go                      # package overview — why the seam exists (P3)
  meta.go                     # raw Meta type, extension ids, _meta key constants
  version.go                  # ProtocolVersion, codec registry, CodecFor[Strict]
  codec.go                    # Codec interface + v1Codec implementation
  apps.go                     # MCP Apps domain + wire types
  tasks.go                    # MCP Tasks domain + wire types, status helpers
  codec_test.go               # round-trip / tolerance / malformed-input tests
  version_test.go             # codec-registry tests
  tasks_test.go               # TaskStatus / ToolTaskSupport helper tests
  golden_test.go              # fixed-input -> fixed wire-JSON golden tests
  concurrency_test.go         # concurrent-reuse test (-race)
  boundary_test.go            # P3 enforcement: no raw wire types outside the seam
docs/specifications/
  mcp-apps-2026-01-26.mdx              # vendored Apps spec, pinned by SHA
  mcp-tasks-experimental.schema.ts     # vendored Tasks schema (source of truth)
  mcp-tasks-experimental.mdx           # vendored Tasks prose spec, pinned by SHA
docs/plans/phase-02-protocolcodec-seam.md   # this file
scripts/smoke/phase-02.sh                   # smoke checks
docs/decisions.md                          # D-022, D-023, D-024
docs/glossary.md                           # Codec, versioned codec, vendored spec
```

No new top-level directory (`internal/` and `docs/specifications/` already
exist per AGENTS.md §3); AGENTS.md §3 unchanged.

## Public API surface

`internal/protocolcodec` is an internal package; the surface below is consumed
by later Dockyard phases (Apps runtime, Tasks runtime, `dockyard validate`),
never by code outside the module.

```go
// Version selection.
type ProtocolVersion string
func CodecFor(version ProtocolVersion) Codec               // never nil; default fallback
func CodecForStrict(version ProtocolVersion) (Codec, error) // errors on unknown
func KnownVersions() []ProtocolVersion

// The seam.
type Codec interface {
    Version() ProtocolVersion
    // MCP Apps
    EncodeAppsToolMeta(base Meta, m AppsToolMeta) (Meta, error)
    DecodeAppsToolMeta(meta Meta) (AppsToolMeta, bool, error)
    EncodeAppsResourceMeta(base Meta, m AppsResourceMeta) (Meta, error)
    DecodeAppsResourceMeta(meta Meta) (AppsResourceMeta, bool, error)
    EncodeAppsExtensionCapability(c AppsExtensionCapability) (json.RawMessage, error)
    DecodeAppsExtensionCapability(raw json.RawMessage) (AppsExtensionCapability, bool, error)
    // MCP Tasks
    EncodeTask(t Task) (json.RawMessage, error)
    DecodeTask(raw json.RawMessage) (Task, error)
    EncodeTaskMeta(m TaskMeta) (json.RawMessage, error)
    DecodeTaskMeta(raw json.RawMessage) (TaskMeta, bool, error)
    EncodeRelatedTaskMeta(base Meta, taskID string) (Meta, error)
    DecodeRelatedTaskMeta(meta Meta) (string, bool, error)
    EncodeCreateTaskResultMeta(base Meta, m CreateTaskResultMeta) (Meta, error)
    DecodeCreateTaskResultMeta(meta Meta) (CreateTaskResultMeta, bool, error)
    EncodeTasksServerCapability(c TasksServerCapability) (json.RawMessage, error)
    DecodeTasksServerCapability(raw json.RawMessage) (TasksServerCapability, bool, error)
}

// Domain types: Meta, AppsToolMeta, AppsResourceMeta, AppsCSP, AppsPermissions,
// AppsExtensionCapability, Task, TaskStatus, ToolTaskSupport, TaskMeta,
// CreateTaskResultMeta, TasksServerCapability.
// Errors: ErrUnknownVersion, ErrMalformedMeta.
```

## Test plan

- **Unit:** round-trip every Apps and Tasks shape (encode → decode, and encode →
  JSON bytes → decode); deprecated-flat-form tolerance and the never-re-emit
  guarantee; nested-form-wins-over-flat; malformed-input → wrapped
  `ErrMalformedMeta`; base-map not mutated by encoders; null-TTL ⇒ explicit
  JSON `null`; `TaskStatus` validity / terminality / transition rules;
  `ToolTaskSupport` validity; codec-registry lookup and fallback.
- **Integration:** N/A — Phase 02's only dependency is Phase 00 (repo
  scaffolding), not another shipped subsystem, and this phase *opens* the
  `protocolcodec` seam rather than consuming one. The seam is consumed by the
  Apps runtime (Wave 4) and Tasks runtime (Wave 5); those phases ship the
  cross-subsystem integration tests against it (AGENTS.md §17).
- **Concurrency / golden:** `TestCodecConcurrentReuse` and
  `TestCodecForConcurrentLookup` exercise a shared `Codec` and the registry from
  64/32 goroutines under `-race` (the reusable-artifact rule, AGENTS.md §14).
  `golden_test.go` pins fixed-input → fixed wire-JSON for every encoder; the
  expected strings are derived from the vendored specs and are the
  spec-compliance assertion (AGENTS.md §11 — spec compliance is tested against
  the vendored specs, not live hosts).
- **Boundary:** `TestNoRawWireTypeImportsOutsideSeam` and
  `TestNoExtensionMetaKeysOutsideSeam` walk the whole module and fail if any
  non-seam package imports a raw extension wire module or hand-writes an
  extension `_meta` key literal; `TestSeamGuardActuallyScans` guards the guard.

## Smoke script additions

`scripts/smoke/phase-02.sh` asserts:

- `internal/protocolcodec` exists and builds.
- Both vendored specs exist under `docs/specifications/` and carry a pinned
  commit SHA in their header.
- The deprecated flat key `ui/resourceUri` appears in Go sources only inside
  `internal/protocolcodec` (the seam-isolation invariant, checked statically).
- `go test` for `internal/protocolcodec` passes (when a Go toolchain is
  available; skips otherwise).

## Coverage target

- `internal/protocolcodec` — **85%** (the phase's stated target; the package is
  a forward-compatibility-critical seam, so it is held to the conformance-tested
  bar rather than the 80% new-package default). Achieved: 93%.

## Dependencies

- Phase 00 — repository scaffolding (Makefile, smoke harness, docs skeleton).

## Risks / open questions

- **Experimental Tasks schema (brief 02 §4 risk 2; RFC §18 Q-4).** The Tasks
  schema is explicitly experimental and may change. Mitigation: the schema is
  vendored and pinned by SHA, every Tasks wire type lives behind the seam, and
  golden tests make a shape change a visible diff. Resolved by D-009/D-010 and
  re-affirmed here.
- **Hand-written vs generated Tasks wire layer (RFC §16 item 4).** Phase 02
  hand-writes the small wire surface it needs rather than building a generator;
  see "Findings I'm departing from" and D-024. The generator is Wave 2 / Wave 5
  scope. Risk: divergence between hand-written types and a future generated
  layer — mitigated by the pinned snapshot, the `regenerate-and-diff` note, and
  golden tests.
- **`protocolVersion` → codec mapping (RFC §18 Q-4; brief 01 §6 Q-4).** V1
  registers one `v1Codec` for both the core spec version and the Apps spec
  revision because their wire shapes are stable across those versions. The
  registry is the mechanism for divergence later; no open question blocks this
  phase.

## Glossary additions

- **Codec** — a `protocolcodec` encoder/decoder pair for one negotiated
  `protocolVersion`.
- **Versioned codec** — the forward-compatibility mechanism: codecs keyed on
  `protocolVersion` so a spec bump adds a sibling codec instead of editing one.
- **Vendored spec** — an external MCP spec mirrored into `docs/specifications/`,
  pinned by upstream commit SHA + date.

(All three added to `docs/glossary.md` in this PR.)

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target (93% ≥ 85%)
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17) —
      N/A, see Test plan; the seam is *opened* here, consumed (with integration
      tests) by Waves 4–5
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`

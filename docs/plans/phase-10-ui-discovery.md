# Phase 10 — ui-discovery

## Summary

Phase 10 adds convention-based UI auto-discovery and the embed pipeline to the
MCP Apps layer. A `.svelte` file under `web/src/apps/` is discovered by
convention, registered as a `ui://` resource composing Phase 09's
`apps.Register`, and the resulting tool↔UI wiring is written into
`dockyard.app.yaml` so the architecture stays inspectable. The built Svelte
bundle is embedded into the Go binary via `//go:embed all:dist`, and the same
`embed.FS` backs the `ui://` MCP resource handler; the build fails cleanly when
the `dist/` embed target is absent.

## RFC anchor

- RFC §7.6 — UI-resource auto-discovery (`.svelte` under `web/src/apps/` →
  `ui://` resource; the wiring written into `dockyard.app.yaml` — "convenience
  without hiding the architecture").
- RFC §7.1 — what Dockyard registers (the `ui://` registration surface this
  phase composes).
- RFC §14 — packaging & deployment (the `//go:embed all:dist` UI bundle; the
  same `embed.FS` backs the `ui://` resource handler).

## Briefs informing this phase

- brief 06 — Go-2026 no-CGo stack & toolchain.
- brief 04 — mcp-use DX teardown.

## Brief findings incorporated

- **brief 06 §2.2** — "a `web/embed.go` file with `//go:embed all:dist`
  populating an `embed.FS` … the same `embed.FS` backs both the inspector's
  HTTP preview and the MCP resource handler." Phase 10 ships
  `runtime/apps/embed.go` carrying an `embed.FS`-backed `Bundle` type that the
  `ui://` resource handler reads from — one `embed.FS`, no second copy.
- **brief 06 §2.2** — "`//go:embed` cannot reach outside its own module
  directory and ignores files starting with `_`/`.` unless `all:` is prefixed."
  Phase 10's `Bundle.Validate` fails cleanly when the embedded `dist/` tree is
  empty (the build-time analogue: a missing `dist/` makes `go build` fail at the
  embed directive — Phase 10 surfaces the runtime equivalent as a typed error).
- **brief 04 §2.4 / §2.7** — mcp-use's "drop a `.tsx` in `resources/`, get a
  registered widget — no manual registration" is the convenience bar. Phase 10
  clears it for Svelte: `.svelte` under `web/src/apps/` is discovered with no
  manual registration call.
- **brief 04 §6 (sharp edge)** — "Widget auto-discovery is a double-edged
  convention … Dockyard keeps the convenience but must surface the wiring
  explicitly in `dockyard.app.yaml` so it stays inspectable." Phase 10's
  discovery writes every discovered App back into the manifest `apps[]` list;
  the convention never hides the wiring.

## Findings I'm departing from (if any)

- **brief 04 §2.4 — "a tool references a widget by file-stem name".** mcp-use
  binds a widget to a tool implicitly by matching the file stem to the tool
  name. Phase 10 does **not** infer the tool↔UI link from the file stem: it
  discovers the App and writes the `apps[]` entry, but the `tools[].ui`
  reference stays an explicit, developer-authored manifest field. Inferring it
  silently would re-introduce exactly the hidden-architecture problem RFC §7.6
  rejects. Discovery surfaces the App; the developer wires the tool. Filed as
  **D-056**.

## Goals

- Discover `.svelte` files under the `web/src/apps/` convention path and turn
  each into a registrable `ui://` App.
- Register a discovered App as a `ui://` resource by composing Phase 09's
  `apps.Register` / `apps.App` surface — no rewrite of `apps.go`.
- Write the discovered tool↔UI wiring into `dockyard.app.yaml` (`apps[]`
  entries) so it stays inspectable; never overwrite a developer-authored entry.
- Embed the built Svelte bundle via `//go:embed all:dist` and back the `ui://`
  resource handler from that one `embed.FS`.
- Fail cleanly — a typed error, never a panic — when the `dist/` embed target
  is absent or empty.

## Non-goals

- The shared `web/ui/` design system / component inventory (Phase 10a).
- The Svelte bridge shell `postMessage` dialect (Phase 11).
- Host-profile `_meta.ui.domain` derivation (Phase 12).
- The `dockyard build` CLI command that sequences `vite build` → `go build`
  (Phase 20) — Phase 10 ships the embed *seam* the command will drive.
- The inspector's HTTP preview surface (Phase 22) — Phase 10 ships the
  `embed.FS`-backed `Bundle` it will reuse, not the HTTP server.
- Vite HMR / the dev loop (Phase 19).

## Acceptance criteria

- [ ] A `.svelte` file dropped under `web/src/apps/` is discovered by convention
      and produces a registrable `ui://` App (`Discover`).
- [ ] A discovered App registers as a `ui://` resource composing Phase 09's
      `apps.Register` (`RegisterDiscovered`).
- [ ] The discovered wiring lands in `dockyard.app.yaml`: a new `apps[]` entry
      is written for each discovered App, an existing developer-authored entry
      is preserved, and the YAML round-trips through `manifest.Load`.
- [ ] The embedded UI bundle (`embed.FS`, `//go:embed all:dist`) serves over the
      MCP `ui://` resource handler — a `resources/read` returns the bundle HTML.
- [ ] The build fails cleanly when the `dist/` embed target is absent:
      `Bundle.Validate` returns a typed error wrapping `ErrEmptyBundle`, never
      panics.

## Files added or changed

```text
runtime/apps/
  discovery.go            # NEW — convention discovery of web/src/apps/*.svelte
  discovery_test.go       # NEW
  embed.go                # NEW — embed.FS-backed Bundle + ui:// resource backing
  embed_test.go           # NEW
  testdata/               # NEW — sample .svelte tree + a sample dist/
internal/manifest/
  wiring.go               # NEW — WriteDiscoveredApps: discovered wiring → apps[]
  wiring_test.go          # NEW
  testdata/
    wiring-base.yaml      # NEW — manifest with one developer-authored app
docs/plans/phase-10-ui-discovery.md   # NEW — this plan
scripts/smoke/phase-10.sh             # NEW
docs/decisions.md                     # D-056, D-057, D-058
docs/glossary.md                      # discovery / embedded bundle terms
runtime/apps/doc.go                   # CHANGED — Phase 10 surface documented
test/integration/phase10_ui_discovery_test.go  # NEW — cross-subsystem test
```

## Public API surface

```go
package apps

// DiscoveredApp is one .svelte file found under the convention path, lifted to
// a registrable ui:// App. ID, URI, and Entry mirror a manifest apps[] entry.
type DiscoveredApp struct {
    ID    string // manifest-local id, derived from the file stem
    URI   string // ui://<manifest-name>/<stem>
    Entry string // path relative to project root (web/src/apps/<stem>.svelte)
}

// Discover walks web/src/apps/ under root and returns every .svelte file as a
// DiscoveredApp. A missing convention directory is not an error — it yields an
// empty slice (a plain MCP server has no UI).
func Discover(root, manifestName string) ([]DiscoveredApp, error)

// RegisterDiscovered registers a discovered App as a ui:// resource on s,
// composing Register. The HTML body is read from bundle.
func RegisterDiscovered(s *server.Server, d DiscoveredApp, bundle Bundle) error

// Bundle is an embed.FS-backed view of the built Svelte UI (//go:embed all:dist).
type Bundle struct { /* unexported embed.FS + sub-root */ }

// NewBundle returns a Bundle rooted at dist within fsys.
func NewBundle(fsys fs.FS, dist string) Bundle

// Validate reports ErrEmptyBundle when the embed target carries no built UI.
func (b Bundle) Validate() error

// HTML returns the App HTML for a discovered entry, read from the bundle.
func (b Bundle) HTML(entry string) ([]byte, error)

// ErrEmptyBundle is returned (wrapped) when the //go:embed dist tree is absent.
var ErrEmptyBundle = errors.New("dockyard/runtime/apps: empty UI bundle")
```

```go
package manifest

// WriteDiscoveredApps merges discovered apps[] entries into the manifest at
// path, preserving every developer-authored entry, and rewrites the YAML.
func WriteDiscoveredApps(path string, discovered []DiscoveredApp) error
```

## Test plan

- **Unit:** `Discover` over a `testdata/` `.svelte` tree (found / nested /
  ignored non-`.svelte`); missing convention dir → empty slice;
  `Bundle.Validate` over a populated and an empty `embed.FS`;
  `Bundle.HTML` reads the right entry; `WriteDiscoveredApps` adds new entries,
  preserves an authored entry, and the result round-trips through `Load`.
- **Integration:** `test/integration/phase10_ui_discovery_test.go` — discover a
  `.svelte` tree, register each App over a real `server.Server` +
  `InMemoryTransport`, issue a `resources/read`, assert the embedded bundle HTML
  comes back with `_meta.ui` (the Phase 09 seam) attached; assert a manifest
  written by `WriteDiscoveredApps` loads and validates.
- **Concurrency / golden:** `Bundle` is read-only after construction — a
  concurrent-`HTML`/`Validate` test under `-race`. Golden: the YAML
  `WriteDiscoveredApps` emits is asserted byte-for-byte against a golden file.

## Smoke script additions

- `runtime/apps/discovery.go` and `embed.go` exist.
- `runtime/apps` still builds CGo-free (`CGO_ENABLED=0`).
- `Discover` / `RegisterDiscovered` / `Bundle` / `ErrEmptyBundle` surface present.
- `internal/manifest/wiring.go` exposes `WriteDiscoveredApps`.
- the embed pipeline uses `//go:embed` (grep the directive).
- the Phase 10 unit + integration tests pass.

## Coverage target

- `runtime/apps` — 80% (new files; the package target stays Phase 09's bar).
- `internal/manifest` — 80% on the new `wiring.go`.

## Dependencies

- Phase 09 — `runtime/apps` `Register` / `App` / `MIMETypeApp` surface.
- Phase 06 — `internal/manifest` schema + loader (the `apps[]` section).
- Phase 07 — `runtime/server` `AddResource` / `ResourceDef` (via Phase 09).

## Risks / open questions

- **Parallel edit of `runtime/apps` with Phase 12.** Phase 12 also edits
  `runtime/apps` (host profiles). Phase 10 keeps every change in *new* files
  (`discovery.go`, `embed.go`) and composes Phase 09's existing `Register`/`App`
  API — a merge-time conflict is expected only in shared docs
  (`decisions.md`, `glossary.md`); the coordinator reconciles.
- **`//go:embed all:dist` with no `dist/`.** A `//go:embed` directive over a
  missing path fails the Go *build*. Phase 10's repo carries a placeholder
  `testdata` `dist/` so `runtime/apps` itself always builds; the real generated
  project's `dist/` is produced by `vite build` (Phase 20). `Bundle.Validate`
  gives the runtime-side clean failure for an *empty* tree.
- **Manifest rewrite preserving comments.** `gopkg.in/yaml.v3` re-marshalling
  drops comments. `WriteDiscoveredApps` re-marshals the typed `Manifest`; the
  authored fields and structure survive, inline comments do not. Documented in
  D-058; acceptable for V1 (the manifest is machine-authored after `dockyard
  new`).

## Glossary additions

- **UI auto-discovery** — the RFC §7.6 convention by which a `.svelte` file
  under `web/src/apps/` becomes a `ui://` resource without a manual
  registration call, with the wiring written back into `dockyard.app.yaml`.
- **Embedded UI bundle** — the built Svelte `dist/` tree compiled into the Go
  binary via `//go:embed all:dist`; one `embed.FS` backs the `ui://` MCP
  resource handler (RFC §14).

## Pre-merge checklist

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check in this PR
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`

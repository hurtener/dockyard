# Phase 06 — manifest

## Summary

Phase 06 delivers the `dockyard.app.yaml` manifest: the typed Go schema, the YAML
loader, and structural validation. The manifest is Dockyard's control plane — the
single artifact `validate`, `generate`, `dev`, `test`, `build`, and `install` (all
Wave 7) read. This phase ships the schema + loader + validation only; it leaves a
clean typed `Manifest` API for those later CLI commands to consume.

## RFC anchor

- RFC §4.2 — the manifest `dockyard.app.yaml` (primary).
- RFC §4.1 — an App is a server (the manifest models tools + optional `apps`).
- RFC §6.1 — contract-first: the manifest's tool `input`/`output` are Go type refs.
- RFC §7.2 — display modes the `apps[].display_modes` subset draws from.
- RFC §7.4 — CSP / single-file bundles the `apps[].csp` and `runtime.ui` express.
- RFC §8.4 — `task_support` (`forbidden | optional | required`).
- RFC §9.4 — the `quality` knobs `dockyard validate` enforces.

## Briefs informing this phase

- brief 04 — mcp-use DX teardown.
- brief 01 — MCP Apps extension.

## Brief findings incorporated

- Brief 04 §"What mcp-use gets wrong" R7: "No manifest / control plane — config is
  scattered across `package.json`, `tsconfig.json`, and code. Nothing equivalent to
  `dockyard.app.yaml` to drive validate/generate/build uniformly." Phase 06 makes
  the manifest the single, inspectable control plane that drives every later
  command, exactly the gap the teardown identifies.
- Brief 04 §"The Dockyard answer": "`dockyard.app.yaml` manifest as the single
  control plane driving validate/generate/build/install." The loader's typed
  `Manifest` struct is that control plane's in-memory form.
- Brief 04 Q-7: staleness detection for generated contracts must let `validate` /
  `build` hard-fail on stale output. Phase 06 does not implement staleness checks,
  but the manifest carries the tool `input`/`output` Go type references those
  checks need; Phase 06's `ResolveContracts` seam returns the resolved schemas so a
  later command can diff them against generated files.
- Brief 01 §2.2: CSP and domain are read from the `resources/read` response, not
  only the static resource declaration. The manifest's `apps[].csp` block is the
  authored source those response-time values are threaded from; Phase 06 validates
  its shape (`connect`/`resource` domain lists) so a malformed CSP fails in
  Dockyard's own validation rather than inside a host.
- Brief 01 §2.5 / §2.7: Apps run under a deny-by-default CSP and graceful
  degradation is mandatory. The manifest models CSP and `display_modes` as opt-in
  subsets; an empty `csp` block is the secure default (single-file bundle, zero
  external origins) and is valid.

## Findings I'm departing from (if any)

None. Phase 06 implements RFC §4.2 as written and adopts the brief findings above.

## Goals

- A typed Go `Manifest` struct mirroring RFC §4.2: `name` / `title` / `version`,
  `runtime` (`transports`, `ui`), `tools` (`name`, `description`, `input`,
  `output`, `ui`, `task_support`), `apps` (`id`, `uri`, `entry`, `display_modes`,
  `csp`, `visibility`), and `quality`.
- A loader that parses `dockyard.app.yaml` into the typed struct.
- Structural validation that fails an invalid manifest with **source-located**
  errors (`file:line` where the YAML node position is available).
- The tool `input`/`output` Go type references are resolvable: a pluggable
  resolver seam turns a `pkg.Type` reference into a JSON Schema via
  `internal/codegen.SchemaForType`.
- An example manifest under `examples/` the loader round-trips cleanly.

## Non-goals

- The CLI commands that consume the manifest (`validate`, `generate`, `dev`,
  `build`, `install`) — Wave 7 (phases 17–21).
- Actually parsing Go source to discover contract types — the resolver is a seam;
  Phase 06 ships a registry-backed resolver for tests and leaves the
  source-scanning resolver to `dockyard generate` (Phase 18).
- Enforcing the `quality` gates — Phase 06 parses and structurally validates the
  `quality` block; enforcement is `dockyard validate` (Phase 18, RFC §9.4).
- Schema generation, TypeScript codegen, drift cross-check — phases 04 / 05.
- Writing or scaffolding manifests — `dockyard new` (Phase 17).

## Acceptance criteria

- [x] A valid `dockyard.app.yaml` loads into a typed `Manifest` Go struct with
      every RFC §4.2 field populated.
- [x] An invalid manifest fails with a source-located error naming the file and,
      where the YAML node carries a position, the line.
- [x] The Go type references in `tools[].input` / `tools[].output` resolve through
      the resolver seam to JSON Schemas.
- [x] Structural validation rejects: missing `name`/`title`/`version`, malformed
      `version`, unknown `transport`, unknown `task_support`, unknown
      `display_mode`, a `ui:` referencing no `apps[]` entry, duplicate tool/app
      names, malformed `ui://` URI, unknown `visibility`.
- [x] The example manifest under `examples/` round-trips: load → validate → no
      error.
- [x] `scripts/smoke/phase-06.sh` reports `OK ≥ 6`, `FAIL = 0`.

## Files added or changed

- `internal/manifest/doc.go` — package overview.
- `internal/manifest/manifest.go` — the typed `Manifest` struct + enums.
- `internal/manifest/load.go` — the YAML loader (`Load`, `LoadFile`).
- `internal/manifest/validate.go` — structural validation + source-located errors.
- `internal/manifest/resolve.go` — the contract-resolver seam + a registry-backed
  resolver wrapping `internal/codegen.SchemaForType`.
- `internal/manifest/load_test.go` — loader / round-trip tests.
- `internal/manifest/validate_test.go` — table + golden structural-validation tests.
- `internal/manifest/resolve_test.go` — resolver-seam tests.
- `internal/manifest/testdata/*.yaml` — valid + invalid fixtures.
- `examples/customer-health/dockyard.app.yaml` — the round-tripped example.
- `docs/plans/phase-06-manifest.md` — this file.
- `scripts/smoke/phase-06.sh` — the smoke script.
- `docs/decisions.md` — D-035, D-036, D-037.
- `docs/glossary.md` — `Manifest`, `Contract reference`, `Quality gate`.
- `go.mod` / `go.sum` — `gopkg.in/yaml.v3` (a direct dependency).

## Public API surface

```go
package manifest

// Manifest is the typed form of dockyard.app.yaml (RFC §4.2).
type Manifest struct {
    Name    string
    Title   string
    Version string
    Runtime Runtime
    Tools   []Tool
    Apps    []App
    Quality Quality
}

func Load(r io.Reader, source string) (*Manifest, error)
func LoadFile(path string) (*Manifest, error)

// Validate runs structural validation; errors are source-located.
func (m *Manifest) Validate() error

// Tool / App lookups for consumers (Wave 7).
func (m *Manifest) Tool(name string) (*Tool, bool)
func (m *Manifest) App(id string) (*App, bool)

// ContractResolver turns a tools[].input/output Go type reference into a schema.
type ContractResolver interface {
    Resolve(ref string) (*jsonschema.Schema, error)
}

// ResolveContracts resolves every tool's input/output through the resolver.
func (m *Manifest) ResolveContracts(r ContractResolver) (map[string]ToolContracts, error)
```

## Test plan

- **Unit:** the loader (well-formed YAML → struct; malformed YAML → wrapped
  error); every validation rule (table-driven, one row per rejection); enum
  parsing (`transport`, `task_support`, `display_mode`, `visibility`); the
  `ui://` URI check; the contract-reference parser (`pkg/path.Type`).
- **Integration:** `ResolveContracts` against a real `internal/codegen`-backed
  resolver — Phase 06's `Deps` name Phase 04, so this closes the manifest↔codegen
  seam with the real resolver, not a mock (AGENTS.md §17). Lives in-package
  (`resolve_test.go`) because `internal/manifest` *is* that wiring boundary.
- **Concurrency / golden:** golden tests pin the rendered text of each
  source-located validation error (`testdata/*.yaml` → fixed error string), so a
  regression in error wording or position surfaces as a diff. `Manifest` is an
  immutable value after `Load`; a concurrent-read test confirms `Tool`/`App`/
  `ResolveContracts` are safe under concurrent use.

## Smoke script additions

- `internal/manifest` package exists and builds CGo-free.
- The example manifest exists and is non-empty.
- `gopkg.in/yaml.v3` is a direct dependency in `go.mod`.
- The manifest package tests pass.
- The example manifest loads and validates without error.
- An invalid fixture fails validation with a source-located error.

## Coverage target

- `internal/manifest` — 80% (new package, AGENTS.md §11 default).

## Dependencies

- Phase 04 — `internal/codegen` (`SchemaForType` is the resolver's engine).

## Risks / open questions

- The contract resolver must, in Wave 7, scan Go source to find the named type;
  Phase 06 ships a registry-backed resolver instead. Risk: the seam shape does not
  fit the source-scanning resolver. Mitigated by keeping the seam minimal — a
  single `Resolve(ref string) (*jsonschema.Schema, error)` method — so any backend
  (reflection registry, source scan, build-info) satisfies it.
- YAML node-position reporting depends on `gopkg.in/yaml.v3` exposing line numbers
  on decode errors and on `yaml.Node`. Mitigated: where a position is unavailable
  the error still names the `source`, degrading to `file` rather than `file:line`.
- Parallel Phase 05 also touches `go.mod` / `docs/decisions.md` / `docs/glossary.md`;
  a merge conflict there is expected and resolved by the coordinator.

## Glossary additions

- **Manifest** — `dockyard.app.yaml`, Dockyard's control plane.
- **Contract reference** — the `pkg/path.Type` string in `tools[].input`/`output`.
- **Quality gate** — a `quality.*` manifest knob `dockyard validate` enforces.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`

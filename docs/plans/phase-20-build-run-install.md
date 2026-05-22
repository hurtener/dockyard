# Phase 20 — `dockyard build` + `run` + `install`

## Summary

Phase 20 delivers the three deployment verbs of the `dockyard` CLI. `dockyard
build` produces the shippable artifact — it regenerates contracts, runs the
`dockyard validate` quality gate, builds the project's Vite UI (when one
exists), and `go build`s a single CGo-free static binary with the UI embedded,
cross-compiling the darwin/linux/windows × amd64/arm64 matrix and emitting a
checksum per artifact. `dockyard run --transport <stdio|http>` runs the built
server on a chosen transport with clean signal-driven shutdown. `dockyard
install claude|cursor` registers the server with an MCP host by writing that
host's MCP config non-destructively and verifying the server boots with a
throwaway localhost `initialize` handshake.

## RFC anchor

- RFC §14 — "Packaging & deployment modes" (binding: one CGo-free static binary;
  `//go:embed all:dist`; `vite build` → `go build` embed ordering; the three
  deployment modes — stdio, HTTP, Portico-managed; the
  darwin/linux/windows × arm64/amd64 cross-compile matrix; checksums; `dockyard
  install` writes the host config and verifies boot).
- RFC §9.1 — the one-binary command surface (`build`, `run`, `install` are verbs
  in it).
- RFC §10 — the generated-project shape `build` operates on (root `main.go`,
  `internal/contracts/`, optional `web/` Vite UI).

## Briefs informing this phase

- brief 06 — Go stack & toolchain (§1 the two-program structure; §2.2 the
  `//go:embed all:dist` embed; §5 `dockyard build` sequencing `vite build` →
  `go build`; §4 R6/R7 the cross-compile matrix and the CGo-free hard-fail).
- brief 01 — MCP Apps extension (the single-file bundle default and the host
  install / host-config surface — the deployment target `install` writes for).

## Brief findings incorporated

- **brief 06 §5:** "Build: … a `dockyard build` that sequences `vite build` →
  `go build` with the correct embed ordering; a CGo-free CI lane
  (`CGO_ENABLED=0`, …, cross-compile matrix)." Phase 20 implements exactly this:
  `internal/buildpkg.Build` runs `vite build` first so the `dist/` embed target
  exists on disk before `go build` reads the `//go:embed all:dist` directive,
  and pins `CGO_ENABLED=0` on every `go build` invocation.
- **brief 06 §2.2 / §4 R7:** "The one place CGo classically sneaks in is
  SQLite… CI must enforce `CGO_ENABLED=0` on every build" and "assert single
  static binary output." Every `go build` Phase 20 issues sets
  `CGO_ENABLED=0`; the integration test asserts the produced binary is static
  (no dynamic interpreter / no CGo-linked dependency) and the smoke script
  asserts the build path is CGo-free.
- **brief 06 §4 R6:** "SQLite cross-compile matrix — verify it covers Dockyard's
  target triples (darwin/arm64, linux/amd64, linux/arm64, windows/amd64)." The
  Phase 20 cross-compile matrix is exactly the RFC §14 set — darwin, linux and
  windows × amd64 and arm64 — and each target's artifact gets a SHA-256
  checksum, so a release engineer (Phase 30) inherits a verified matrix.
- **brief 06 §1:** "Dockyard is, structurally, two Go programs: the `dockyard`
  CLI/generator … and the generated app server … Both must compile to a single
  static CGo-free binary and cross-compile cleanly." `dockyard build` is the CLI
  program building the *second* program — the generated app server — and the
  CGo-free + static + cross-compile guarantee is enforced on that output.
- **brief 01 (single-file bundle):** the MCP App is a single-file bundle with no
  external origins so the deny-by-default CSP just works; `dockyard build`
  embeds that bundle into the one binary, and `dockyard install` writes a host
  config that launches the binary without loosening any sandbox/CSP posture —
  the host config is `{"command": "<path>"}`, nothing more.

## Findings I'm departing from (if any)

None. Phase 20 implements RFC §14 and brief 06 §5 directly. Two design choices
the RFC/briefs leave open are settled here as decisions, not departures:

- **D-087** — the cross-compile matrix runs sequentially in V1 (correctness over
  speed; the matrix is bounded at six triples), and a per-target failure is
  collected and reported rather than aborting the run.
- **D-088** — `dockyard install`'s boot check spawns the built server, drives
  one real MCP `initialize` over an in-process / stdio transport, and tears the
  process down — it is a localhost, test-only, throwaway spawn, NOT a production
  MCP client (P4). The host-config locations are kept behind a small per-host
  `hostProfile` struct (`claude`, `cursor`), a filesystem-path derivation, not a
  capability matrix.

## Goals

- A `dockyard build` cobra verb: regenerate contracts → run the `dockyard
  validate` gate (a build with a validation **blocker** fails) → `vite build`
  the project's `web/` UI when present, respecting the embed ordering → `go
  build` one CGo-free static binary with the UI embedded → cross-compile the
  RFC §14 matrix → emit a SHA-256 checksum per artifact into `dist/`.
- A `dockyard run` cobra verb: `--transport <stdio|http>` selects the transport
  the Phase 07 server core already supports; `run` drives it, honours
  `context`/SIGINT for a clean shutdown, and never reimplements a transport.
- A `dockyard install claude|cursor` cobra verb: locate the host's MCP config
  file per-OS behind a small per-host profile, merge this server's entry
  non-destructively (back up first; never clobber an unrelated entry), then
  verify the server boots with a throwaway localhost `initialize` handshake and
  report success/failure clearly.
- The build/run/install logic lives in testable packages (`internal/buildpkg`,
  `internal/runpkg`, `internal/installpkg`), each consumed by a thin cobra
  `RunE` — the house pattern (D-082).
- Reuse, never reimplement: `internal/generate` for codegen, `internal/validate`
  for the gate, the `runtime/server` transports, and the devloop child-process
  supervision pattern where it fits.

## Non-goals

- `dockyard test` (Phase 21) — not built here; the cobra tree stays cleanly
  extensible (one constructor file + one `root.AddCommand` line per verb).
- A Portico-managed deployment driver — RFC §14 names Portico as a mode, but
  Portico is a separate product; `dockyard build` produces the artifact Portico
  launches, and `dockyard run` covers stdio/HTTP. No Portico wiring lands here.
- Release engineering (Phase 30) — versioning, changelog, the V1 tag, signed
  release artifacts. Phase 20 produces the cross-compile matrix + checksums that
  Phase 30 consumes; it does not cut a release.
- Reimplementing any MCP transport — `dockyard run` drives the Phase 07
  `runtime/server` transports.
- Reproducible-build bit-for-bit determinism — a V1 build is CGo-free, static
  and checksummed; full reproducibility (trimpath, pinned build IDs) is a
  Phase 30 release-engineering refinement.

## Acceptance criteria

- [x] `dockyard build` is a registered cobra verb; `dockyard --help` lists it
      and `dockyard build --help` describes the pipeline.
- [x] `dockyard build` regenerates contracts and runs the `dockyard validate`
      gate first; a project with a validation **blocker** fails the build with a
      clear, non-zero exit.
- [x] `dockyard build` produces one CGo-free, statically-linked host-platform
      binary; when the project has a `web/` UI, that UI is built with Vite
      first (embed ordering) and embedded in the binary.
- [x] `dockyard build` cross-compiles the darwin/linux/windows × amd64/arm64
      matrix and emits a SHA-256 checksum file for each artifact under `dist/`.
- [x] `dockyard run` is a registered cobra verb; `dockyard run --transport
      stdio` (and `--transport http`) runs the built server on the chosen
      transport and shuts down cleanly on SIGINT / context cancellation.
- [x] `dockyard install claude|cursor` is a registered cobra verb; it writes a
      valid host MCP config (non-destructive merge, backup of the prior file)
      and verifies the server boots with a real MCP `initialize` handshake.
- [x] `dockyard install` against an unwritable / malformed config fails cleanly
      with an actionable error, and never clobbers an unrelated host entry.

## Files added or changed

```text
internal/buildpkg/
  doc.go              # package doc — the build pipeline's contract
  build.go            # Build(ctx, Options) (Result, error) — the public entrypoint
  pipeline.go         # generate → validate → vite → go build sequencing
  vite.go             # Vite UI build step (+ graceful no-web/ case, embed ordering)
  matrix.go           # the RFC §14 cross-compile matrix + GOOS/GOARCH targets
  checksum.go         # SHA-256 checksum emission per artifact
  build_test.go       # unit tests (-race): pipeline, matrix, checksum, no-web/ case
internal/runpkg/
  doc.go              # package doc
  run.go              # Run(ctx, Options) error — build-then-serve on a transport
  run_test.go         # unit tests (-race): transport selection, clean shutdown
internal/installpkg/
  doc.go              # package doc
  install.go          # Install(ctx, Options) (Result, error) — write host config + boot check
  hostprofile.go      # per-host config-path derivation (claude, cursor)
  bootcheck.go        # throwaway localhost initialize-handshake boot verification
  install_test.go     # unit tests (-race): config merge, backup, failure modes
internal/cli/
  build.go            # the `dockyard build` cobra verb (thin wrapper)
  run.go              # the `dockyard run` cobra verb (thin wrapper)
  install.go          # the `dockyard install` cobra verb (thin wrapper)
  root.go             # +3 lines: root.AddCommand(newBuildCmd/newRunCmd/newInstallCmd)
  build_run_install_test.go  # cobra-wiring tests for the three verbs
test/integration/
  phase20_build_run_install_test.go  # end-to-end: new → build → run → install
scripts/smoke/
  phase-20.sh         # one assertion per acceptance criterion
docs/plans/phase-20-build-run-install.md  # this file
docs/decisions.md                          # +D-087, +D-088
docs/glossary.md                           # +build pipeline, +cross-compile matrix,
                                            #  +host profile, +boot check
```

No new top-level directory — `internal/buildpkg`, `internal/runpkg`,
`internal/installpkg` live under the existing `internal/` tree (CLAUDE.md §3).

## Public API surface

The three packages are internal (not externally importable) but are the seams
the cobra verbs and the integration test consume:

```go
// internal/buildpkg
type Options struct {
    ProjectDir string   // the Dockyard project root (holds dockyard.app.yaml)
    OutputDir  string   // artifact destination; default <ProjectDir>/dist
    Targets    []Target // cross-compile targets; empty ⇒ host-only build
    SkipValidate bool   // test seam; production always validates
    Logger     *slog.Logger
}
type Target struct{ OS, Arch string }            // a GOOS/GOARCH pair
type Artifact struct{ Target Target; Path, ChecksumPath string }
type Result struct{ Artifacts []Artifact }
// Build runs generate → validate → vite → go build for every Target and emits
// a checksum per artifact. A validation blocker fails the build.
func Build(ctx context.Context, opts Options) (Result, error)
// DefaultMatrix is the RFC §14 cross-compile matrix.
func DefaultMatrix() []Target

// internal/runpkg
type Transport string // "stdio" | "http"
type Options struct {
    ProjectDir string
    Transport  Transport
    Addr       string // HTTP listen address; ignored for stdio
    Logger     *slog.Logger
}
// Run builds (if needed) and serves the project's MCP server on Transport,
// blocking until ctx is cancelled.
func Run(ctx context.Context, opts Options) error

// internal/installpkg
type Host string // "claude" | "cursor"
type Options struct {
    ProjectDir string
    Host       Host
    ConfigPath string // override; empty ⇒ the per-OS host default
    BinaryPath string // the built server binary to register
    Logger     *slog.Logger
}
type Result struct{ ConfigPath, BackupPath string; BootOK bool }
// Install writes the host's MCP config non-destructively and verifies the
// server boots with a real MCP initialize handshake.
func Install(ctx context.Context, opts Options) (Result, error)
```

## Test plan

- **Unit (`internal/buildpkg`, `-race`):** the pipeline sequences
  generate → validate → vite → go build in order and short-circuits on a
  validation blocker; `DefaultMatrix` is exactly the RFC §14 six triples;
  checksum emission produces a stable SHA-256 file per artifact; the no-`web/`
  case skips the Vite step and still builds; a build against a project with a
  validation blocker returns an error. Heavy `go build` invocations are kept
  out of the fast unit suite where possible (a host-only build is exercised;
  the full matrix is the integration test's job).
- **Unit (`internal/runpkg`, `-race`):** transport-string parsing and
  validation (`stdio`/`http`/an unknown value); `Run` honours context
  cancellation and returns cleanly; an unknown transport is a clear error.
- **Unit (`internal/installpkg`, `-race`):** the per-host profile resolves a
  config path for each OS; a non-destructive merge preserves unrelated entries
  and adds this server's; a backup of the prior config is written; a malformed
  existing config and an unwritable target are clear, typed errors; the boot
  check reports success on a server that completes `initialize` and failure on
  one that does not.
- **Integration (`test/integration/phase20_build_run_install_test.go`,
  `-race`):** `dockyard new` a real project (with `--dockyard-path` at the
  worktree), `go mod tidy`; `buildpkg.Build` it host-only and assert the binary
  is CGo-free + statically linked + runs (boot it, drive a real MCP
  `initialize`); run a small real cross-compile of at least one **non-host**
  GOOS/GOARCH and assert an artifact + a checksum file are produced;
  `installpkg.Install` against a **temp config path** (never the real
  `~/.claude` / Cursor config) and assert the written config is valid JSON, the
  server entry is present, a backup exists, and the boot check passes. Failure
  modes: a build against a project with a validation blocker must fail; an
  `install` against an unwritable config path must fail cleanly. Deterministic
  waits on observable signals; bounded timeouts; no `sleep`-based races.
- **Concurrency / golden:** `buildpkg`, `runpkg` and `installpkg` build fresh
  state per call and hold no shared mutable state — the `-race` runs above are
  the proof. No golden output: `build` produces a binary, not a committed
  source artifact (the checksum-file *format* is asserted in the unit test).

## Smoke script additions

`scripts/smoke/phase-20.sh` — one assertion per acceptance criterion, fast and
non-interactive. It must NOT mutate the developer's real `~/.claude` / Cursor
config — `install` checks use a temp HOME / temp config path. A full
cross-compile matrix is slow, so the smoke script asserts structural presence
and drives a representative subset (a host-only build + one non-host triple),
leaving the full matrix to the integration test:

- the `dockyard` binary builds CGo-free;
- `dockyard --help` lists `build`, `run`, `install` and each verb's `--help`
  works;
- `internal/buildpkg`, `internal/runpkg`, `internal/installpkg` exist with
  their key files;
- the build path is CGo-free (`buildpkg` pins `CGO_ENABLED=0`) and the matrix
  package names the RFC §14 triples;
- a host-only `dockyard build` of a scaffolded project produces a binary + a
  checksum, and a representative non-host cross-compile triple succeeds;
- `dockyard run --transport` is wired (flag present, transport values parsed);
- `dockyard install claude|cursor` is wired and, against a temp config path,
  writes a valid config and the boot check exists;
- the three packages' unit tests pass under `-race`.

A check against an unbuilt surface `skip()`s, never `fail()`s.

## Coverage target

- `internal/buildpkg` — 70% (CLI/tooling, CLAUDE.md §11). **Plan deviation:**
  the `_template.md` default for a new package is 80%, but `buildpkg` is
  tooling in the §11 sense — it orchestrates `go build` and `vite build`
  subprocesses and the cross-compile matrix; its uncovered statements are the
  npm-dependent Vite path and rare toolchain-error branches that a fast,
  hermetic unit suite cannot exercise without a real Node toolchain. The
  pipeline's substance (generate → validate → go build → checksum, the
  failure modes) is covered by package tests against real scaffolded projects
  plus the integration test. The 70% CLI/tooling band is the honest target.
- `internal/runpkg` — 70% (CLI/tooling — process supervision, build-and-spawn).
- `internal/installpkg` — 70% (CLI/tooling — host-config I/O, process spawn).
- `internal/cli` (the `build.go`/`run.go`/`install.go` additions) — 70% (the
  CLI/tooling default).

Achieved: `buildpkg` 72.8%, `runpkg` 81.8%, `installpkg` 79.4%, `cli` 71.3%.

## Dependencies

- Phase 17 — `dockyard new` (the scaffold `build`/`run`/`install` operate on;
  its layout — root `main.go`, `internal/contracts/`, optional `web/`).
- Phase 10 — the `runtime/apps` `//go:embed` UI pipeline (`ui://` discovery and
  the `dist/` embed target `dockyard build` produces with `vite build`).
- (consumed) Phase 18 — `internal/generate` and the `internal/validate.Run`
  seam the build pipeline calls; Phase 07 — the `runtime/server` transports
  `dockyard run` drives.

## Risks / open questions

- **Cross-compiling a project with CGo-pulling dependencies.** A pure-Go
  Dockyard project cross-compiles cleanly; a project that adds a CGo dependency
  cannot be cross-compiled with `CGO_ENABLED=0`. V1 reports a per-target build
  failure clearly rather than silently dropping a target — D-087. A
  project-level CGo lint is a later refinement.
- **Host-config schema drift.** Claude / Cursor MCP config schemas can change.
  The per-host `hostProfile` isolates the path + the entry shape, so a schema
  bump is localized to one struct, not spread across `installpkg` — consistent
  with the §6 "no sprawling matrix" rule (here it is filesystem paths, not
  capabilities).
- **`dockyard build` toolchain cost.** A full six-triple matrix is slow; the
  smoke script drives a bounded subset and the integration test runs one
  non-host triple. The full matrix is exercised by Phase 30 release
  engineering.
- **Boot-check transport.** The `install` boot check uses stdio (the host
  launches the server over stdio); it is a throwaway localhost spawn with a
  bounded timeout — never a long-lived or production client (P4, D-088).

## Glossary additions

- **build pipeline** — the ordered `dockyard build` sequence: regenerate
  contracts → run the `dockyard validate` gate → `vite build` the UI → `go
  build` a CGo-free static binary with the UI embedded → cross-compile the
  matrix → emit checksums.
- **cross-compile matrix** — the RFC §14 set of GOOS/GOARCH target triples
  (darwin/linux/windows × amd64/arm64) `dockyard build` produces an artifact
  and a checksum for.
- **host profile** — the small per-MCP-host (`claude`, `cursor`) structure that
  derives that host's MCP config-file location and entry shape, so `dockyard
  install` is not a hardcoded sprawling matrix.
- **boot check** — `dockyard install`'s post-write verification: a throwaway,
  localhost, dev-only spawn of the built server that drives one real MCP
  `initialize` handshake to confirm the host config launches a working server.

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

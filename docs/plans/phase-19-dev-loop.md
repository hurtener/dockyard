# Phase 19 — `dockyard dev` — the fsnotify orchestrator

## Summary

Phase 19 delivers `dockyard dev`: an embedded `fsnotify`-based dev orchestrator
that supervises one process tree from a single command. It watches a scaffolded
project, restarts the Go MCP server on `.go` changes, re-runs codegen in-process
on contract-file changes, and supervises the project's Vite dev server (which
owns Svelte HMR). Ctrl-C tears the whole tree down cleanly — no orphan
processes, no leaked goroutines, no leaked ports.

## RFC anchor

- RFC §9.2 — "The `dev` loop — settled" (binding: embedded `fsnotify`, no
  shell-out to `air`/`wgo`; one process tree; restart Go on `.go`, regenerate on
  contract change, supervise Vite).
- RFC §9.1 — the one-binary command surface (`dockyard dev` is one verb in it).
- RFC §9.3 — the CLI uses `spf13/cobra`.

## Briefs informing this phase

- brief 06 — Go stack & toolchain (§2.6 the fsnotify/`air`/`wgo` decision, §2.7
  Vite integration).
- brief 04 — mcp-use DX teardown (§2.2, §2.8 the one-command dev-loop bar).

## Brief findings incorporated

- **brief 06 §2.6:** "Dockyard should *not shell out to either* — it should
  embed its own watcher using `fsnotify`… `dockyard dev` is a multi-process
  orchestrator… and needs to choreograph them — restart only the Go process on
  `.go` changes, re-run codegen on contract changes, leave Vite HMR to handle
  `.svelte`." Phase 19 implements exactly this: `internal/devloop` embeds
  `fsnotify`, classifies events, and choreographs a Go-server supervisor, an
  in-process codegen step, and a Vite supervisor.
- **brief 06 §2.6:** "Vite already provides hot-reload for the UI; Go has no
  in-process hot reload, so the Go server is *restarted*, not patched." The
  Go-server supervisor terminates the old process and starts a fresh one; the
  Vite supervisor starts Vite once and lets it own `.svelte` HMR — Dockyard
  never reimplements HMR.
- **brief 06 §2.6:** "`wgo`'s 'parallel commands' model is the conceptual
  reference; `fsnotify` + `os/exec` is the implementation." The orchestrator is
  `fsnotify` + `os/exec` with a `context`-scoped supervisor goroutine per child.
- **brief 04 §2.8:** "One-command start to running app… Time-to-first-render is
  minutes." `dockyard dev` is one command, one process; the `log/slog` text
  handler gives readable, immediate dev output — clearing the "DX better than
  mcp-use" bar (brief 04 §1).
- **brief 04 §2.2:** mcp-use's `dev` triggers "automatic server restart and
  widget reload (HMR)" on file change. Dockyard matches this behaviour with no
  external dev tool and no Node-mediated `npx` — one static binary.

## Findings I'm departing from (if any)

None. The phase implements brief 06 §2.6 and RFC §9.2 directly. The one design
choice the briefs leave open — how `dev` behaves for a blank server with no
`web/` UI project — is settled here as D-086 (graceful degradation: supervise
only the Go server), consistent with RFC §4.1 ("a UI resource is additive").

## Goals

- A `dockyard dev` cobra verb wired into the root command tree (one constructor
  file + one `root.AddCommand` line — the house pattern).
- An embedded `fsnotify` file watcher with debounced event classification:
  `.go` source change, contract-source change, everything else ignored.
- A Go-server supervisor: build + run the scaffolded project's MCP server;
  on a `.go` change, cleanly terminate the old process and start the new one.
- Codegen on contract change: call the `internal/generate` API **in-process**
  (not by shelling out to the `dockyard generate` verb) so generated types are
  live before the Go server restarts. Order is regenerate-then-restart.
- A Vite dev-server supervisor for the project's `web/` UI; if no `web/` Vite
  project exists, degrade gracefully — supervise only the Go server, log it,
  never error.
- One process tree with a clean lifecycle: `context` cancellation / Ctrl-C
  tears down the Go server, Vite, and the watcher with no orphan processes, no
  leaked goroutines, no leaked ports.
- A child-process crash is reported through `log/slog` and the loop survives or
  exits cleanly with a clear message — it never panics `dockyard dev` itself.
- `internal/devloop` is a reusable, concurrency-safe, testable package — the
  orchestration is not buried in a cobra `RunE`.

## Non-goals

- `dockyard build` / `run` / `install` (Phase 20) and `dockyard test`
  (Phase 21) — not built here; the cobra tree stays cleanly extensible.
- The inspector (`dockyard inspect`, Phase 22+). RFC §9.2's prose mentions an
  inspector in the `dev` process tree; the inspector surface does not exist yet,
  so Phase 19 supervises the Go server, codegen, and Vite only. Inspector
  attachment is a later phase's one-line addition to the supervised set — see
  Risks.
- Reimplementing Svelte HMR — Vite owns it.
- A file-watch config surface (include/exclude globs in the manifest) — V1
  watches a fixed, sensible set; a config surface is a later refinement.

## Acceptance criteria

- [ ] `dockyard dev` is a registered cobra verb; `dockyard --help` lists it and
      `dockyard dev --help` describes it.
- [ ] Editing a contract source file regenerates the contract types live (the
      `internal/generate` API is invoked in-process on a contract-file event).
- [ ] The Go MCP server is restarted on a `.go` source change (old process
      terminated, new process started — no orphan, no port leak).
- [ ] Svelte HMR works via the supervised Vite dev server (Vite is started and
      supervised for a project with a `web/` UI; Dockyard does not reimplement
      HMR).
- [ ] A project with no `web/` UI degrades gracefully — `dev` supervises only
      the Go server, logs that no UI project was found, and does not error.
- [ ] One `dockyard dev` process, no external dev tool (no `air`/`wgo`/`npx`
      shell-out for the watch loop; `fsnotify` is embedded).
- [ ] `context` cancellation tears down the whole process tree cleanly — proven
      under `-race` with no goroutine leak and no orphan child process.

## Files added or changed

```text
internal/devloop/
  doc.go             # package doc — the orchestrator's contract
  devloop.go         # Orchestrator: Run(ctx, Config) — the public entrypoint
  watcher.go         # fsnotify watcher + debounced event classification
  supervisor.go      # generic context-scoped child-process supervisor
  goserver.go        # the Go MCP server supervisor (build + run + restart)
  vite.go            # the Vite dev-server supervisor (+ graceful no-web/ case)
  devloop_test.go    # unit + concurrency tests (-race)
  watcher_test.go    # debounce + event-classification tests
internal/cli/
  dev.go             # the `dockyard dev` cobra verb (thin wrapper over devloop)
  root.go            # +1 line: root.AddCommand(newDevCmd())
  dev_test.go        # cobra-wiring test for the dev verb
test/integration/
  devloop_integration_test.go  # end-to-end: new → dev → touch → cancel
scripts/smoke/
  phase-19.sh        # one assertion per acceptance criterion
docs/plans/phase-19-dev-loop.md   # this file
docs/decisions.md                  # +D-084..D-086
docs/glossary.md                   # +dev loop, +orchestrator, +supervisor
go.mod / go.sum                    # + github.com/fsnotify/fsnotify
```

## Public API surface

`internal/devloop` is internal (not externally importable) but is the seam the
`dockyard dev` verb and the integration test consume:

```go
// Config configures one dev-orchestrator run.
type Config struct {
    ProjectDir string      // the scaffolded project root (holds dockyard.app.yaml)
    Logger     *slog.Logger
    Debounce   time.Duration // event debounce window; 0 ⇒ a sane default
    // GoServerCommand / ViteCommand are seams: empty ⇒ the real defaults
    // (`go run .` and `npm run dev`); a test injects a controllable command.
}

// Run starts the orchestrator and blocks until ctx is cancelled or a fatal
// error occurs. On return, the whole process tree is torn down. Run is safe to
// call once per Config; it holds no global state.
func Run(ctx context.Context, cfg Config) error
```

## Test plan

- **Unit (`internal/devloop`, `-race`):** debounce coalesces a burst of events
  into one trigger (table-driven over burst sizes/intervals); event
  classification (`.go` vs contract-source vs ignored); the supervisor starts,
  restarts (terminate-then-start, no orphan), and stops a child; the no-`web/`
  case logs and does not error; a child that fails to start is reported and the
  loop survives; concurrent file events while a restart is in flight do not
  deadlock or leak. Clean-shutdown test asserts no goroutine leak
  (`runtime.NumGoroutine` settle, or a leak-check helper).
- **Integration (`test/integration/devloop_integration_test.go`, `-race`):**
  `dockyard new` a real project (with `--dockyard-path` at the worktree),
  `go mod tidy`, start `devloop.Run` against it in-process with a **real**
  `fsnotify` watcher and **real** child processes (the Go child is a small
  controllable stub command so CI stays fast — injected via `Config`, the seam;
  the watcher and codegen path are fully real). Then: (1) touch a `.go` file →
  assert the Go server child is restarted (observe a restart signal, not
  `sleep`); (2) edit a contract source file → assert codegen re-ran and the
  generated output bytes changed; (3) cancel the context → assert the whole
  tree is torn down, no orphan child, no leaked goroutine. Failure mode: a
  child command that exits non-zero on start — assert the loop reports it and
  survives (or exits cleanly), never panics. Deterministic waits on observable
  signals (channels / file mtime), bounded timeouts, no `sleep`-based races.
- **Concurrency / golden:** the `-race` runs above are the concurrency proof.
  No golden output — `dev` produces no committed artifact.

## Smoke script additions

`scripts/smoke/phase-19.sh` — one assertion per acceptance criterion, fast and
non-interactive (no real long-running `dockyard dev`):

- the `dockyard` binary builds CGo-free;
- `dockyard --help` lists `dev` and `dockyard dev --help` works;
- the `internal/devloop` orchestrator package exists with its key files;
- the orchestrator package's tests pass (drives the testable surface — debounce,
  classification, restart, teardown — without starting a real dev session);
- `internal/devloop` references `fsnotify` and not `air`/`wgo` (no external dev
  tool);
- `internal/cli/dev.go` exists and `root.go` registers `newDevCmd`.

A check against unbuilt surface `skip()`s, never `fail()`s.

## Coverage target

- `internal/devloop` — 80% (new non-CLI package).
- `internal/cli` (the `dev.go` addition) — 70% (CLI/tooling default).

## Dependencies

- Phase 17 — `dockyard new` (the scaffold `dev` runs against; its layout —
  root `main.go`, `internal/contracts/`, optional `web/`).
- Phase 18 — `dockyard generate` / the `internal/generate` API `dev` calls
  in-process on a contract change.

## Risks / open questions

- **Process teardown on different OSes.** Killing a child process tree is
  platform-sensitive. V1 targets POSIX (`SIGTERM` then `SIGKILL` on a grace
  timeout, child started in its own process group so the whole group dies).
  Windows is not a V1 dev-loop target — the shipped binary cross-compiles, but
  `dockyard dev` is a developer-machine tool. Flagged, not blocking.
- **Inspector in the dev tree.** RFC §9.2 names the inspector as part of the
  `dev` process tree; the inspector lands in Phase 22+. Phase 19's supervisor
  set is a slice — adding the inspector later is one more supervised entry, not
  a restructure. Noted as a deliberate, non-silent scoping (D-085).
- **Vite command discovery.** `dev` detects a `web/` Vite project by a
  `web/package.json`; the dev command defaults to `npm run dev`. A project
  using a different package manager is a later refinement; the `Config` seam
  already allows overriding the command.

## Glossary additions

- **dev loop** — the `dockyard dev` orchestrated edit-feedback cycle: an
  embedded `fsnotify` watcher choreographing a Go-server restart, in-process
  codegen, and a supervised Vite dev server, as one process tree.
- **orchestrator** (`internal/devloop`) — the reusable, concurrency-safe package
  that implements the dev loop.
- **supervisor** — a `context`-scoped goroutine owning one child process's
  lifecycle (start, restart, clean stop) within the orchestrator.

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

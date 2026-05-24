---
name: package
description: Package a Dockyard server into its shippable artifact with `dockyard build` — one CGo-free static binary embedding the Svelte UI, optional cross-compile matrix, SHA-256 checksums — and install it into an MCP host (Claude, Cursor) with `dockyard install`. Use when shipping a release or wiring a built server into a host.
license: Apache-2.0
metadata:
  framework: dockyard
  surface: cli
  verbs: "build run install"
---

# Package and ship a Dockyard server

Dockyard ships everything you need to go from a project directory to a
single static binary running inside an MCP host:

- **`dockyard build`** — the packaging pipeline. Regenerate → validate →
  Vite-build the UI → `go build` one CGo-free static binary with the UI
  embedded. Optional cross-compile matrix with SHA-256 checksums.
- **`dockyard run`** — build + run on the chosen transport. The
  "I want to see it running now" verb.
- **`dockyard install <host>`** — write the host's MCP config so the
  host launches your built server, then verify it boots with a real MCP
  `initialize` handshake. V1 hosts: `claude`, `cursor`.

## `dockyard build`

```bash
# One-time setup for a project that has a web/ UI:
(cd web && npm install)

dockyard build                        # host platform only
dockyard build --cross-compile        # darwin/linux/windows x amd64/arm64
dockyard build --output dist          # custom output dir (default: <project>/dist)
```

What it runs (in order):

1. **`dockyard generate`** — regenerate every contract artifact from the
   Go contracts. A failing generate fails the build.
2. **`dockyard validate`** — the quality gates (see the `validate`
   skill). A blocker fails the build, so a stale or invalid contract
   never ships.
3. **Vite build** — runs `npm run build` in `web/` (when the project has
   a UI) before `go build`. The build's output (`web/dist/`) is what the
   server's `//go:embed all:web/dist` directive picks up at compile
   time.
4. **`go build`** — produces one CGo-free, statically-linked binary per
   target with the UI embedded. With `--cross-compile`, emits the full
   matrix and a `.sha256` checksum per artifact.

Artifacts land in the output directory (default `dist/`).

## `dockyard run`

```bash
dockyard run                          # stdio (default)
dockyard run --transport http         # streamable-HTTP on 127.0.0.1:8080
dockyard run --transport http --addr 0.0.0.0:9000   # custom address
```

`run` builds the project (same pipeline as `dockyard build`) and runs
the produced server on the selected transport. Ctrl-C tears down the
child cleanly — no orphan process.

The project's `main.go` owns its transport wiring (the scaffold templates
respect `DOCKYARD_TRANSPORT` + `DOCKYARD_HTTP_ADDR`); `dockyard run`
sets them and drives the binary. The CLI never reimplements a transport.

## `dockyard install`

```bash
dockyard install claude
dockyard install cursor
dockyard install claude --binary /path/to/built/server
dockyard install claude --config /custom/path/to/mcp.json
```

What it does:

- **Writes the host's MCP config** so the host launches your Dockyard
  server as a local stdio subprocess. The prior config is backed up to
  a timestamped sidecar — unrelated MCP-server entries are preserved.
- **Verifies the server boots** by spawning it and driving a real MCP
  `initialize` handshake. A boot failure still leaves the config
  written; the CLI prints both the install result and the failure so
  you can triage.

`dockyard install` writes a **host config** — it is **not** an MCP
client (P4, RFC §1). The boot check is a throwaway, localhost, dev-only
spawn.

When the binary path is omitted, the install verb assumes the project
has been built; pass `--binary` to point at a binary at a different
path.

## CGo-free guarantee

The shipped artifact is **always** built with `CGO_ENABLED=0`. The
runtime's lone persistence driver in V1 is `modernc.org/sqlite` (pure
Go, no CGo); the SDK is pure Go; the obs stack is pure Go. A
dependency that would force CGo is rejected (AGENTS.md §13).

The `-race` test detector requires CGo, so test runs use
`CGO_ENABLED=1`. Test binaries are not shipped.

## Reproducible builds

`dockyard build --cross-compile` produces the same byte-identical binary
for the same Go source + manifest + UI input. The SHA-256 checksum
sidecars let a downstream consumer verify integrity. Phase 30 will tag
V1 with a published release matrix; locally, `--cross-compile` is the
same matrix CI publishes.

## Common patterns

- **Local dev** — `dockyard dev` (the dev loop skill). No build, no install.
- **Smoke a built binary** — `dockyard run` once. Ctrl-C when done.
- **Ship to a host** — `dockyard build` then `dockyard install <host>`.
- **CI release matrix** — `dockyard build --cross-compile --output dist`.

## Common pitfalls

- **The validate step failed.** `dockyard build` will not proceed.
  Run `dockyard validate` standalone for the focused report (see the
  `validate` skill).
- **`web/dist/` is empty.** Vite did not run. Confirm `web/package.json`
  exists; if you have no UI, that's fine — the `//go:embed` directive
  emits an empty FS and the server has no App.
- **Install wrote the config but boot-check failed.** Run the binary
  directly with `DOCKYARD_TRANSPORT=stdio` and stdin redirected from
  `/dev/null` — the runtime's error will print to stderr. Fix and
  re-install.
- **Host doesn't see the new server after install.** Some hosts cache
  their config — restart the host application after `dockyard install`.

## What to do next

- Drive the installed server from the host's chat surface — fire one of
  your tools; the App should render inline.
- Or attach the inspector to the installed server while the host is
  running ⇒ `test-with-the-inspector` skill.
- Iterate ⇒ `run-the-dev-loop` skill.

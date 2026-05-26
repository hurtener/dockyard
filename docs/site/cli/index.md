# CLI reference

> Auto-generated from the cobra command tree by `internal/clidocs`. Do not hand-edit — re-run `make docs`.

## `dockyard`

> Build production-grade MCP Servers and MCP Apps

```text
Dockyard is a Go-native framework for building production-grade
MCP Servers and MCP Apps. It ships as one static, CGo-free binary: scaffold a
server, write typed Go tool handlers, and get generated contracts, a local
inspector, quality gates, and one-command packaging.

The command tree covers the full developer workflow: 'new' scaffolds a server,
'generate' regenerates contracts from typed Go structs, 'validate' enforces
the contract-first quality gate, 'dev' runs the live-reload loop, 'build'
produces the shippable binary, 'run' serves it, 'install' registers it with
a host, 'test' runs the contract + compliance gate, and 'inspect' opens the
local debug surface.
```

## `dockyard build`

> Build the project into a CGo-free static binary with the UI embedded

```text
Build a Dockyard project into its shippable artifact (RFC §14).

'dockyard build' runs the packaging pipeline:

  - regenerates the project's contract artifacts from the Go contracts;
  - runs the 'dockyard validate' quality gate — a build blocker fails the
    build, so a stale or invalid contract never ships;
  - builds the project's web/ Svelte UI with Vite (when the project has one),
    before 'go build' so the embedded dist/ tree exists at compile time;
  - 'go build's one CGo-free, statically-linked binary with the UI embedded.

With --cross-compile it builds the full darwin/linux/windows x amd64/arm64
matrix and emits a SHA-256 checksum file per artifact; otherwise it builds the
host platform only. Artifacts are written to the --output directory (default
dist/).
```

| Flag | Description | Default |
| --- | --- | --- |
| `--cross-compile` | build the full darwin/linux/windows x amd64/arm64 matrix with checksums | `false` |
| `--dir` | project directory (default: current directory) | `—` |
| `--output` | artifact output directory (default: &lt;project&gt;/dist) | `—` |

## `dockyard dev`

> Run the project's dev loop — watch, regenerate, restart, inspect

```text
Run Dockyard's embedded dev loop against a project (RFC §9.2).

'dockyard dev' is one process supervising a process tree. It watches the
project with an embedded fsnotify watcher — no external dev tool — and:

  - restarts the Go MCP server on a .go source change;
  - re-runs contract codegen in-process on a change under internal/contracts,
    so the generated types are live before the server restarts;
  - supervises the Vite dev server for the project's web/ UI (Vite owns Svelte
    HMR). A project with no web/ UI degrades gracefully — only the Go server is
    supervised.
  - auto-attaches the local inspector against the supervised server so the
    Tools / Events / RPC / Verdicts / Prompts panels are one click away. The
    inspector URL is printed to stdout once it is reachable.

By default the dev loop pins the supervised Go server to HTTP on
127.0.0.1:8080 so the inspector has a known MCP base URL to attach to.
A developer who already exported DOCKYARD_TRANSPORT / DOCKYARD_HTTP_ADDR
in their shell wins — the dev-loop pins are defaults, not overrides.

Press Ctrl-C to stop: the whole process tree is torn down cleanly.
```

| Flag | Description | Default |
| --- | --- | --- |
| `--debounce` | file-change debounce window (default: 250ms) | `0s` |
| `--dir` | project directory (default: current directory) | `—` |
| `--inspector-addr` | inspector loopback bind (default: 127.0.0.1:0 — OS-assigned port) | `—` |
| `--no-inspector` | do not auto-attach the inspector (for CI / headless dev runs) | `false` |

## `dockyard generate`

> Regenerate JSON Schema and TypeScript from Go contracts

```text
Regenerate a project's contract artifacts from its Go contract structs.

'dockyard generate' runs the Design A codegen pipeline (RFC §6.2): for every
tool in dockyard.app.yaml it generates the input and output JSON Schema and the
TypeScript contract types from the typed Go input/output structs. The Go struct
is the single source of truth — the generated files are never hand-edited.

generate is idempotent: running it twice with no contract change produces a
byte-identical result and reports nothing changed.
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dir` | project directory (default: current directory) | `—` |

## `dockyard inspect`

> Attach the local inspector to a running MCP server

```text
Attach Dockyard's local inspector to a running MCP server (RFC §12).

'dockyard inspect' serves the inspector — Dockyard's local test/debug surface —
on a loopback port and relays the Logbook event stream of the MCP server named
by --url to it. The inspector renders the server's Apps in a sandboxed iframe,
shows the live Logbook stream and the JSON-RPC log, switches fixtures, runs
contract/spec verdicts, and emulates host capability sets.

  --url      the running MCP server's base URL (e.g. http://127.0.0.1:8080);
             the inspector relays its obs stream and reads its ui:// Apps.
  --dir      the Dockyard project directory (default: the current directory);
             sources the contract verdicts and the generated tool contracts.
  --port     the inspector's own loopback port (default: an OS-assigned port).
  --no-open  do not open a browser — for CI and headless use.

The Verdicts panel and the Fixtures switcher are sourced from the project at
--dir: the verdicts re-run 'dockyard validate', the fixtures derive from the
project's generated tool contracts (P1). When --dir names no Dockyard project,
those panels degrade to their honest empty state. The App preview reads the
attached server's ui:// resources via short-lived, operator-initiated MCP
client sessions (D-103, D-144).

The inspector is dev-mode-gated, localhost-only, and operator-initiated only
(D-144): every client-shaped operation is driven by an explicit UI action in
the localhost-bound web frontend, runs in a short-lived per-request session,
and has a documented decision entry. It is never a production MCP client and
never reachable off-localhost. A non-loopback bind is refused before the
listener opens. Press Ctrl-C to stop.
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dir` | project directory — sources verdicts and contracts (default: current directory) | `—` |
| `--no-open` | do not open a browser (for CI / headless use) | `false` |
| `--port` | the inspector's loopback port (default: an OS-assigned port) | `0` |
| `--url` | the running MCP server's base URL (its obs stream is relayed, its Apps read) | `—` |

## `dockyard install`

> Register the built server with an MCP host (Claude, Cursor)

**Usage:** `dockyard install <claude|cursor>`

```text
Register a built Dockyard server with an MCP host (RFC §14).

'dockyard install claude' (or 'cursor') writes the host's MCP config file so
the host launches this Dockyard server as a local stdio subprocess, then
verifies the server boots by spawning it and driving a real MCP initialize
handshake.

The write is non-destructive: the prior config is backed up to a timestamped
sidecar and every unrelated MCP-server entry is preserved. Pass --binary to
point at the built server (default: build the project first with
'dockyard build'); --config overrides the host config-file path.

This writes a HOST config — it is not an MCP client. The boot check is a
throwaway, localhost, dev-only spawn.
```

| Flag | Description | Default |
| --- | --- | --- |
| `--binary` | path to the built server binary (default: &lt;project&gt;/dist/&lt;name&gt;-&lt;os&gt;-&lt;arch&gt;) | `—` |
| `--config` | host MCP config file path (default: the host's per-OS location) | `—` |
| `--dir` | project directory (default: current directory) | `—` |

## `dockyard new`

> Scaffold a new MCP server project

**Usage:** `dockyard new <name>`

```text
Scaffold a new, blank, working MCP server.

'dockyard new <name>' creates a project directory with a manifest, one example
contract-first tool, its generated JSON Schema and TypeScript, a runnable main,
and a contract test. The generated project builds and serves immediately.

The no-template path is first-class — no --template flag is required to get a
working server. Templates (analytics-widgets, approval-flows, inspector) are
optional product-pattern showcases; pass --template <name> to scaffold one.
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dir` | parent directory to create the project under (default: current directory) | `—` |
| `--example-task-support` | example tool's task_support declaration: forbidden (default), optional, or required. optional/required also auto-wires a tasks.Engine in main.go. | `—` |
| `--module` | Go module path for the new project's go.mod (default: example.com/&lt;name&gt;) | `—` |
| `--template` | product-pattern template to scaffold (e.g. analytics-widgets). Omit for the blank no-template scaffold (the first-class path). | `—` |

## `dockyard run`

> Build and run the project's MCP server on a transport

```text
Build and run a Dockyard project's MCP server (RFC §14).

'dockyard run' builds the project — a fresh, validated, CGo-free binary, the
same pipeline 'dockyard build' runs — and then runs the produced server on the
transport selected by --transport:

  - stdio  the local single-user subprocess transport (the default);
  - http   the streamable-HTTP transport, listening on --addr.

The project's server owns its transport wiring; 'dockyard run' drives it and
never reimplements a transport. Press Ctrl-C to stop: the server child is torn
down cleanly with no orphan process.
```

| Flag | Description | Default |
| --- | --- | --- |
| `--addr` | HTTP listen address (default 127.0.0.1:8080; ignored for stdio) | `—` |
| `--dir` | project directory (default: current directory) | `—` |
| `--transport` | transport to serve on: stdio or http | `stdio` |

## `dockyard test`

> Run the contract + compliance gate (go test, contracts, spec, capability)

```text
Run Dockyard's full test gate against a project (RFC §9.4).

'dockyard test' runs, as one command, every test category Dockyard's quality
bar is built on:

  - go-test          the project's own Go unit tests ('go test ./...')
  - contract         the contract-first assertions — the generated JSON Schema
                     and TypeScript still match the Go contract structs (P1)
  - golden           the fixture / golden snapshots are present and coherent
  - spec-compliance  the Apps/Tasks constructs conform to the vendored MCP
                     specs (checked against the vendored specs, never a host)
  - capability       the project degrades gracefully across host capability
                     sets — no crash, no hardcoded host matrix (RFC §7.5)

A regression in any gating category exits non-zero. Warnings are reported but
do not change the exit code.
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dir` | project directory (default: current directory) | `—` |
| `--skip-go-test` | skip the go-test category (the slowest); the other gates still run | `false` |

## `dockyard validate`

> Run the project quality gates (manifest, schemas, mappings, spec)

```text
Run Dockyard's quality gates against a project (RFC §9.4).

'dockyard validate' checks the manifest, the generated JSON Schemas, the
tool↔UI resource mappings, the App MIME types, MCP spec compliance against the
vendored specs, the four-state UI page rule, and stale-codegen drift — a
generated file that no longer matches its Go contract source.

A build-blocker failure exits non-zero. Warnings are reported but do not change
the exit code.
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dir` | project directory (default: current directory) | `—` |

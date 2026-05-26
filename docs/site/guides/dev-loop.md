# Dev loop

`dockyard dev` is one process supervising a process tree
([RFC §9.2](/reference/rfc)):

- Restarts the Go MCP server on a `.go` change.
- Re-runs contract codegen in-process on a change under
  `internal/contracts/`, so the generated types are live **before** the
  server restarts.
- Supervises the Vite dev server for the project's `web/` Svelte UI
  (Vite owns Svelte HMR).
- **Auto-attaches the local inspector** as a third supervised child,
  printing its URL to stdout once it is reachable. Opt out for CI /
  headless dev with `--no-inspector`.
- Tears the whole tree down cleanly on Ctrl-C.

The watcher is **embedded** (`fsnotify`). No external dev tool —
no `air`, no `wgo`, no `tsc --watch`. Install `dockyard`, run
`dockyard dev`.

## Run it

```bash
dockyard dev
```

Sample output:

```text
INFO running codegen
INFO building server
INFO server listening on http://127.0.0.1:8080
INFO starting vite dev server
INFO vite ready on http://127.0.0.1:5173
INFO inspector ready at http://127.0.0.1:54321
```

Open the inspector URL in a browser — `cmd-click` works in most
terminals. The dev loop, the supervised server, the Vite HMR child,
and the inspector are all reachable from one command.

## Flags

| Flag                | What it does                                                              | Default          |
| ------------------- | ------------------------------------------------------------------------- | ---------------- |
| `--dir`             | project directory                                                          | cwd              |
| `--debounce`        | file-change debounce window                                                | 250ms            |
| `--no-inspector`    | skip the supervised inspector child (CI / headless dev)                    | off (auto-attach) |
| `--inspector-addr`  | inspector loopback bind                                                    | `127.0.0.1:0`    |

## What triggers what

| Change                          | Effect                                              |
| ------------------------------- | --------------------------------------------------- |
| `internal/contracts/*.go`       | regenerate → rebuild Go → restart server            |
| any other `*.go`                | rebuild Go → restart server                         |
| `dockyard.app.yaml`             | regenerate → rebuild Go → restart server            |
| `web/src/**`                    | Vite HMR (Svelte hot reload — no server restart)    |
| `web/vite.config.ts`            | Vite reloads the config; HMR continues              |
| `web/dist/`                     | ignored (build artifact)                            |

## How auto-attach picks the server URL

The inspector needs a deterministic MCP base URL to attach to. The
dev loop **pins the supervised Go server to HTTP** on
`127.0.0.1:8080` by default — it exports `DOCKYARD_TRANSPORT=http`
and `DOCKYARD_HTTP_ADDR=127.0.0.1:8080` as defaults on the child
environment. You override either by exporting them yourself before
running `dockyard dev`; your exports win via the later-entry-wins
rule `os/exec` follows.

With `--no-inspector` the dev loop sets nothing, preserving the
scaffolded server's stdio default exactly. Use this for CI runs,
screen-shares, or any context where the inspector would only be
noise.

## With a standalone inspector

For a server that is not under `dockyard dev` (e.g. a deployed dev
build, a remote loopback server you are debugging), use
`dockyard inspect` directly:

```bash
# terminal 1
DOCKYARD_TRANSPORT=http dockyard run

# terminal 2
dockyard inspect --url http://127.0.0.1:8080
```

Same surface, separate process.

## See also

- [`run-the-dev-loop` agent skill](/agent-skills/)
- [Inspector guide](inspector)
- [Decisions: D-161 — dockyard dev auto-attaches the inspector](/reference/decisions)
- [Decisions: D-162 — the supervised inspector is in-process](/reference/decisions)

# Dev loop

`dockyard dev` is one process supervising a process tree
([RFC §9.2](/reference/rfc)):

- Restarts the Go MCP server on a `.go` change.
- Re-runs contract codegen in-process on a change under
  `internal/contracts/`, so the generated types are live **before** the
  server restarts.
- Supervises the Vite dev server for the project's `web/` Svelte UI
  (Vite owns Svelte HMR).
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
INFO server listening on stdio
INFO starting vite dev server
INFO vite ready on http://127.0.0.1:5173
```

## Flags

| Flag         | What it does                                    | Default     |
| ------------ | ----------------------------------------------- | ----------- |
| `--dir`      | project directory                                | cwd         |
| `--debounce` | file-change debounce window                      | 250ms       |

## What triggers what

| Change                          | Effect                                              |
| ------------------------------- | --------------------------------------------------- |
| `internal/contracts/*.go`       | regenerate → rebuild Go → restart server            |
| any other `*.go`                | rebuild Go → restart server                         |
| `dockyard.app.yaml`             | regenerate → rebuild Go → restart server            |
| `web/src/**`                    | Vite HMR (Svelte hot reload — no server restart)    |
| `web/vite.config.ts`            | Vite reloads the config; HMR continues              |
| `web/dist/`                     | ignored (build artifact)                            |

## With the inspector

```bash
# terminal 1
DOCKYARD_TRANSPORT=http dockyard dev

# terminal 2
dockyard inspect --url http://127.0.0.1:8080
```

When the dev loop restarts the server, the inspector reconnects
automatically.

## See also

- [`run-the-dev-loop` agent skill](/agent-skills/)
- [Inspector guide](inspector)
- [Decisions: D-115 — `make build` embeds the production inspector SPA](/reference/decisions)

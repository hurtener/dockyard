---
name: run-the-dev-loop
description: "Use `dockyard dev` — Dockyard's embedded fsnotify dev orchestrator — to iterate on a server: live restart on Go changes, automatic codegen on contract changes, and supervised Vite HMR for the Svelte UI. Use when actively building a tool or App, instead of `go run` + manual `npm run dev` in two terminals."
license: Apache-2.0
metadata:
  framework: dockyard
  surface: cli
  verbs: "dev"
---

# Run a Dockyard server in dev mode

`dockyard dev` is one process supervising a process tree. It watches your
project with an **embedded fsnotify** watcher (no external tool — no `air`,
no `wgo`) and orchestrates:

- Restarting the Go MCP server on a `.go` file change.
- Re-running contract codegen in-process on a change under
  `internal/contracts/` — the generated types are live *before* the
  server restarts.
- Supervising the Vite dev server for the project's `web/` Svelte UI
  (Vite owns Svelte HMR). A project with no `web/` degrades gracefully —
  only the Go server is supervised.

One Ctrl-C tears down the whole tree cleanly: no orphan `vite` process,
no zombie server.

## Run it

From your project directory:

```bash
dockyard dev
```

That's the entire command. Expected output (paraphrased):

```text
INFO running codegen
INFO building server
INFO server listening on stdio
INFO starting vite dev server
INFO vite ready on http://127.0.0.1:5173
```

## Flags

```text
--dir <path>           project directory (default: cwd)
--debounce <duration>  file-change debounce window (default 250ms)
```

`--debounce` is the window the watcher waits before triggering a rebuild;
the default 250 ms catches multi-file editor saves (an IDE rewriting
several files in one save) without thrashing.

## What triggers what

| Change                          | Effect                                              |
| ------------------------------- | --------------------------------------------------- |
| `internal/contracts/*.go`       | regenerate → rebuild Go → restart server            |
| any other `*.go`                | rebuild Go → restart server                         |
| `dockyard.app.yaml`             | regenerate → rebuild Go → restart server            |
| `web/src/**`                    | Vite HMR (Svelte hot reload — no server restart)    |
| `web/vite.config.ts` etc.       | Vite reloads the config; HMR continues              |
| anything in `web/dist/`         | ignored (it is a build artifact)                    |

## Working alongside the inspector

In a second terminal, attach the inspector to the dev server:

```bash
dockyard inspect --url http://127.0.0.1:8080
```

(Set `DOCKYARD_TRANSPORT=http` for the HTTP transport if your server
defaults to stdio.) The inspector renders your App in a sandboxed iframe,
shows the live `obs/v1` stream, and lets you fire tools from the
Operator-Invoke surface. When the dev loop restarts the server, the
inspector reconnects automatically.

## Common pitfalls

- **Forgetting to set `DOCKYARD_TRANSPORT=http` for the inspector**. The
  stdio transport binds to the dev process's pipes; the inspector
  attaches via HTTP. Either set the env var for `dockyard dev` (it is
  picked up by the supervised server) or run a separate `dockyard run
  --transport http` in another terminal.
- **HMR didn't update**. Verify Vite is still running in the dev process
  output. A syntax error in your Svelte file shows in the Vite output;
  fix it and Vite recovers without a restart.
- **Codegen ran but the server didn't restart**. Check the Go build
  output — a Go compile error blocks the restart and is printed inline.

## Why the embedded watcher

mcp-use's hot-reload is also good (brief 04 §2.8.5), but it relies on
the Node toolchain — `tsc --watch` + a JS process supervisor.
Dockyard's `dev` is one CGo-free static binary supervising one Vite
child process; the watcher, the codegen, and the supervisor are all
in-process. That makes the developer's startup cost zero: install
`dockyard`, run `dockyard dev`. No `npm install dev-tool-of-the-month`,
no version-skew rituals.

## What to do next

- Drive the live server from the inspector ⇒
  `test-with-the-inspector` skill.
- Ship the finished build ⇒ `package` skill.
- Run the full quality gate before pushing ⇒ `validate` skill.

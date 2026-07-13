---
name: run-the-dev-loop
description: "Use `dockyard dev` — Dockyard's embedded fsnotify dev orchestrator — to iterate on a server: live restart on Go changes, automatic codegen on contract changes, supervised Vite HMR for the Svelte UI, and the local inspector auto-attached on a known loopback port. Use when actively building a tool or App, instead of `go run` + manual `npm run dev` + a second `dockyard inspect` terminal."
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
- **Auto-attaching the local inspector** as a third supervised child
  alongside the Go server and Vite. The inspector URL is printed to
  stdout once it is reachable — `cmd-click` to open it. Opt out for
  CI / headless dev with `--no-inspector`.

One Ctrl-C tears down the whole tree cleanly: no orphan `vite` process,
no zombie server, no leftover inspector listener.

## Run it

From your project directory:

```bash
dockyard dev
```

That's the entire command. Expected output (paraphrased):

```text
INFO running codegen
INFO building server
INFO server listening on http://127.0.0.1:8080
INFO starting vite dev server
INFO vite ready on http://127.0.0.1:5173
INFO inspector ready at http://127.0.0.1:54321
```

## Flags

```text
--dir <path>            project directory (default: cwd)
--debounce <duration>   file-change debounce window (default 250ms)
--no-inspector          skip the supervised inspector child (CI / headless)
--inspector-addr <addr> inspector loopback bind (default 127.0.0.1:0 — OS-assigned)
```

`--debounce` is the window the watcher waits before triggering a rebuild;
the default 250 ms catches multi-file editor saves (an IDE rewriting
several files in one save) without thrashing.

`--no-inspector` is for runs where the inspector would only be noise:
a CI job, a screen-share, a headless build server. Everything else
about the dev loop is unchanged.

## How the auto-attached inspector knows the server URL

The dev loop **pins the supervised Go server to HTTP** on
`127.0.0.1:8080` by default so the inspector has a deterministic MCP
base URL to attach to. It does this by setting `DOCKYARD_TRANSPORT=http`
and `DOCKYARD_HTTP_ADDR=127.0.0.1:8080` as **defaults** on the child
environment. You override either by exporting them yourself before
running `dockyard dev` — the values you set win.

With `--no-inspector` the dev loop sets nothing, preserving the
scaffolded server's stdio default exactly.

The scaffolded HTTP entrypoint accepts both current stateless MCP requests and
legacy session-based requests on this one URL. No separate port or host-specific
configuration is needed while clients migrate.

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

The inspector is **already running** — `dockyard dev` auto-attaches it
on the loopback port it printed at startup. Open that URL to drive your
server: render Apps in a sandboxed iframe, watch the live Logbook
stream, invoke tools from the operator-invoke form, pull rendered
messages from the Prompts panel. When the dev loop restarts the
server, the inspector reconnects automatically.

If you need the **standalone** inspector path (e.g. against a server
that is not under `dockyard dev`), use `dockyard inspect --url
http://127.0.0.1:8080` in a second terminal — same surface, separate
process.

The inspector has no OAuth flow, bearer-token flag, token forwarding, or
credential store. It cannot attach to an OAuth-protected server without being
challenged. Use Harbor or a purpose-built test client for authenticated calls,
or run an unauthenticated loopback-only configuration during inspector work; see
`docs/site/guides/oauth-protected-resource.md`.

## Common pitfalls

- **Port 8080 is busy on my machine.** The dev loop's HTTP pin defaults
  to `127.0.0.1:8080` (the scaffold default). If that port is taken,
  the supervised Go server's bind fails — export
  `DOCKYARD_HTTP_ADDR=127.0.0.1:9090` (or any free port) before running
  `dockyard dev`. Your export wins over the dev loop's default.
- **Inspector port collides with a running standalone inspector.** The
  auto-attached inspector defaults to an OS-assigned port (port 0), so
  collisions are impossible by construction. If you pinned
  `--inspector-addr 127.0.0.1:54321` against a port someone else is
  using, the inspector child's bind fails and the rest of the dev tree
  stays up — the error message names the port; fix and re-run.
- **I want stdio.** Pass `--no-inspector`: the dev loop stops pinning
  the transport and your scaffolded server keeps its stdio default.
- **HMR didn't update.** Verify Vite is still running in the dev process
  output. A syntax error in your Svelte file shows in the Vite output;
  fix it and Vite recovers without a restart.
- **Codegen ran but the server didn't restart.** Check the Go build
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

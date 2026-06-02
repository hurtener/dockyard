# Packaging + install

Three verbs ship the packaging surface
([RFC §14](/reference/rfc)):

- **`dockyard build`** — regenerate → validate → Vite build → `go build`
  one CGo-free static binary with the UI embedded.
- **`dockyard run`** — build + run on a transport (stdio | http). The
  "I want to see it now" verb.
- **`dockyard install <host>`** — write the host's MCP config so the
  host launches your built server; verify it boots with a real MCP
  `initialize` handshake. Supported hosts: `claude`, `cursor`.

## `dockyard build`

```bash
# One-time setup for a project that has a web/ UI:
(cd web && npm install)

dockyard build                        # host platform only
dockyard build --cross-compile        # darwin/linux/windows × amd64/arm64
dockyard build --output dist          # custom output dir
```

What it runs:

1. `dockyard generate`
2. `dockyard validate`
3. `npm run build` in `web/` (when the project has a UI)
4. `go build` — one CGo-free statically linked binary per target with
   the UI embedded; `.sha256` sidecars in cross-compile mode.

Artifacts land in the output directory (default `dist/`).

## `dockyard run`

```bash
dockyard run                          # stdio
dockyard run --transport http         # 127.0.0.1:8080
dockyard run --transport http --addr 0.0.0.0:9000
```

`run` builds the project (same pipeline as `build`), then runs the
binary on the selected transport. Ctrl-C tears the child down cleanly
— no orphan process.

## `dockyard install`

```bash
dockyard install claude
dockyard install cursor
dockyard install claude --binary /path/to/built/server
dockyard install claude --config /custom/path/to/mcp.json
```

- **Writes the host's MCP config** so the host launches your Dockyard
  server as a local stdio subprocess. The prior config is backed up to
  a timestamped sidecar; unrelated MCP-server entries are preserved.
- **Verifies the server boots** by spawning it and driving a real MCP
  `initialize` handshake.

`dockyard install` writes a host config — it is **not** an MCP client
(P4, [RFC §1](/reference/rfc)).

## CGo-free guarantee

The shipped binary is always built with `CGO_ENABLED=0`. The
runtime's persistence driver is `modernc.org/sqlite` (pure
Go, no CGo); the SDK is pure Go; the obs stack is pure Go. A
dependency that would force CGo is rejected
([AGENTS.md §13](https://github.com/hurtener/dockyard/blob/main/AGENTS.md)).
Test runs use `CGO_ENABLED=1` because the `-race` detector requires
CGo — test binaries are not shipped.

## See also

- [`package` agent skill](/agent-skills/)
- [Validate + test guide](validate)
- [Dev loop guide](dev-loop)

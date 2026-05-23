# Brief 04 — mcp-use DX teardown

**Date:** 2026-05-20
**Sources:** See §7. All primary sources reachable except `npmjs.com/package/create-mcp-use-app` (HTTP 403 to WebFetch — template list recovered via DeepWiki mirror instead, flagged inline).
**Status:** Draft for RFC-001-Dockyard

## 1. Why this brief exists

The braindump sets an explicit bar: Dockyard's DX must be **better than mcp-use's** (Dump 3, Dump 4). mcp-use is the closest competitor — it self-describes as "the framework for MCP with the best DX" and as "the fullstack MCP framework to build MCP Apps for ChatGPT / Claude & MCP Servers for AI Agents." It has solved, in TypeScript, most of the surface Dockyard wants to own: scaffolding, hot reload, a server+widget workflow, and a built-in inspector.

This brief teardown-audits mcp-use's developer experience command-by-command, separates what genuinely works from what is weak or TypeScript-bound, and converts that into a concrete adopt/build/avoid list for Dockyard. It feeds RFC-001's open-questions and phase-planning sections. Scope reminder: Dockyard is **server-side only** (Harbor owns the MCP client); mcp-use's client SDK and `MCPAgent` features are out of scope for comparison except where they color the DX.

## 2. Findings

### 2.1 Package and CLI topology

mcp-use ships as a fan of npm packages, not one binary:

- `mcp-use` — core framework (MCP server, MCP apps, plus a client SDK + `MCPAgent` Dockyard does not need). Python parity exists via `pip install mcp-use` (monolithic).
- `@mcp-use/cli` — "build tool with hot reload and auto-inspector."
- `@mcp-use/inspector` — standalone web previewer/debugger.
- `create-mcp-use-app` — project scaffolder.

Install path is `npx`-mediated: `npx create-mcp-use-app my-mcp-app`, then the project's own `package.json` scripts wrap `mcp-use dev|build|start`. There is no global binary; everything runs through Node + npm resolution.

### 2.2 CLI command surface (verbatim)

- `npx create-mcp-use-app@latest [name]` — scaffold a new project.
- `mcp-use dev` — "start development with auto-reload and inspector." TypeScript compiles in watch mode; server on `http://localhost:3000`; inspector auto-mounts at `/inspector`; MCP endpoint at `/mcp`. File changes trigger automatic server restart and widget reload (HMR).
- `mcp-use build` — "production build"; compiles TypeScript, bundles React widgets into standalone HTML pages with asset hashing.
- `mcp-use start` — run the compiled production server.
- `npx @mcp-use/inspector [--url <url>] [--port <port>] [--no-open]` — standalone inspector; default port 8080, auto-finds next free port, auto-opens browser; `--no-open` for CI; `--url` auto-connects to a running server (`http://|https://|ws://|wss://`).
- `npx @mcp-use/cli login` — auth for their hosted deploy.
- `npx @mcp-use/cli deploy` — push the server to mcp-use's production/cloud.

Notable: there is **no `test` command, no `validate` command, no `install`-into-host command, and no typegen command**. The dev loop is `create → dev → build → deploy`. Quality gating, host-config wiring, and contract testing are simply absent from the toolchain.

### 2.3 Scaffolding and templates

`create-mcp-use-app` offers three templates (recovered via DeepWiki mirror; npm page was 403):

- `starter` — basic MCP server with example tools.
- `mcp-ui` — server with MCP-UI React widgets.
- `apps-sdk` — OpenAI Apps SDK-compatible server for ChatGPT.

These are **transport/protocol-flavored templates, not product-pattern templates**. The braindump explicitly rejects this framing ("App templates, not blank projects… business/use-case oriented, not protocol oriented" — Dump 2). Separately, mcp-use markets a gallery of "Ready-to-use MCP Apps you can deploy in one click or remix" (Chart Builder, Diagram Builder, Slide Deck, Maps Explorer, Recipe Finder, Widget Gallery, File Manager) — these are example apps to clone, not first-class CLI templates.

Generated project structure: `src/index.ts` (server entry), `resources/` (React widget components), `package.json` + `tsconfig.json`, `dist/` (build output). Flat and minimal — no fixtures directory, no tests directory, no manifest, no contracts directory.

### 2.4 Server + UI widget workflow — the strong part

This is mcp-use's best idea. Tools are defined with `server.tool(name, { description, parameters: z.object({...}), execute })`, Zod schemas providing input validation. Widgets are React `.tsx` files dropped into `resources/` and are **auto-discovered — no manual registration**. The framework handles "tool registration, props mapping, hot reloading." A tool references a widget by file-stem name; the widget reads tool-result data via the `useWidget` hook (`const { props } = useWidget<{ productName: string }>()`). `useMcp()` exposes `callTool()` so widgets can invoke server tools directly. `McpUseProvider` wraps every widget with StrictMode, protocol-aware theming, error boundary, optional debug controls, and optional `ResizeObserver` auto-sizing — so widgets work across both the MCP Apps protocol and the ChatGPT Apps SDK without per-widget branching.

The win: a developer drops a component in a folder and gets a working, host-rendered widget with zero protocol ceremony. That convenience-by-convention is the bar Dockyard must clear.

### 2.5 Inspector / testing surface

The inspector is mcp-use's second-strongest feature. It is **auto-included when `server.listen()` runs** (at `/inspector`), available standalone via `@mcp-use/inspector`, and hosted online at `inspector.mcp-use.com`. Tabs: **Tools** (test execution), **Resources** (browse URIs), **Prompts** (template testing), **Chat** (BYOK live chat against the server). It supports reverse-proxy scenarios (`MCP_URL`) and iframe-embedding control (`MCP_INSPECTOR_FRAME_ANCESTORS`, `*` in dev / `'self'` in prod).

The gap: it is an **interactive inspect surface, not a test harness**. There is no fixture system, no golden snapshots, no scripted assertions, no host-compatibility matrix, no permission/empty/error-state simulation. You can poke the server by hand; you cannot encode "this is correct" and have CI enforce it.

### 2.6 Contracts / typegen

This is the weakest link and the clearest opening for Dockyard. mcp-use's "type safety" is **inference-local, not generated**:

- Zod schema → TypeScript inference flows into the `execute` handler. Good within one file.
- The widget side declares its expected shape **by hand**: `useWidget<{ productName: string }>()`. The generic is author-supplied; nothing connects it back to the tool's Zod output schema. If the tool result shape drifts, the widget's generic silently lies.
- There is **no codegen step** producing a shared contract artifact. Server and widget are two type universes bridged only by developer discipline.

So mcp-use has *types* but not *contracts*. The braindump's "killer feature: contract-first MCP Apps" with generated UI types, fixtures, and contract tests (Dump 2) is precisely the hole.

### 2.7 Packaging / install / deploy

`mcp-use build` produces a `dist/` of compiled JS + bundled widget HTML. Running it requires a **Node runtime present on the target**. There is no single-artifact story. Install into local hosts (Claude/Cursor `mcp.json`) is not automated — no `install` command. Production deployment is funneled through `@mcp-use/cli login` + `deploy` into mcp-use's own cloud. This is the mcp-mesh.ai pattern the braindump flags as the cautionary tale (Dump 4): a good OSS DX with a soft push toward a proprietary hosting endpoint.

### 2.8 What mcp-use DX genuinely does well

1. **One-command start to running app** — `create-mcp-use-app` → `mcp-use dev` → live inspector. Time-to-first-render is minutes.
2. **Widget-by-convention** — drop a `.tsx` in `resources/`, get a registered, hot-reloading widget. Zero protocol boilerplate visible to the developer.
3. **Inspector is on by default** — no separate setup; debugging surface is always one URL away. The Chat (BYOK) tab lets you test model-driven tool selection without a real host.
4. **Cross-protocol provider** — `McpUseProvider` abstracts MCP Apps vs ChatGPT Apps SDK differences.
5. **HMR through to widgets** — widget edits reflect without full restart; even works through reverse proxies.

### 2.9 Where it falls short / is TypeScript-bound

1. **Node runtime everywhere** — server is JS; deployment always ships a Node runtime + `node_modules`. No single binary, no embedded assets. Local stdio install means the host must execute `node`.
2. **No real test toolchain** — no `test`/`validate` command, no fixtures, no golden snapshots, no host-compat tests, no quality gates. Quality is unenforced.
3. **Contracts are not generated** — widget types are hand-declared generics; server↔widget drift is undetected (see §2.6).
4. **Templates are protocol-flavored, not product-flavored** — `starter`/`mcp-ui`/`apps-sdk` describe transports, not workflows.
5. **No UI quality-state scaffolding** — no generated loading/empty/error/permission states. `McpUseProvider` gives an error boundary; that is the extent of it.
6. **Deploy funnels to a proprietary cloud** — `@mcp-use/cli deploy` + `login` lock the boring-path deployment to mcp-use's hosting.
7. **No manifest / control plane** — config is scattered across `package.json`, `tsconfig.json`, and code. Nothing equivalent to `dockyard.app.yaml` to drive validate/generate/build uniformly.
8. **React-only widgets** — `resources/` auto-discovery, `useWidget`, `useMcp`, `McpUseProvider` are all React. Svelte/Vue/plain-TS are not first-class.
9. **Multi-package install friction** — `mcp-use` + `@mcp-use/cli` + `@mcp-use/inspector` + `create-mcp-use-app` plus `npx` resolution; version skew across the four is a live failure mode.

<!--
EDITOR'S NOTE (Phase 24, 2026-05-23): the master plan's original template name
`analytical-card` was renamed to `analytics-widgets` when the template
actually shipped (decision D-124). The two `analytical-card` mentions
below are preserved as historical research content per CLAUDE.md §16
(research briefs are *context*, not the design source of truth); the
shipped V1 template name is `analytics-widgets`.
-->

## 3. Go-flavored shapes / API sketches (Dockyard CLI surface, command-by-command)

Dockyard ships **one statically-linked CGo-free binary** (`dockyard`). No `npx`, no package fan-out, no Node on the install target. Proposed command surface — explicitly a superset of mcp-use's, closing each gap from §2.9:

```text
dockyard new <name> --template <t>   # scaffold; -t analytical-card | approval-flow | inspector (V1)
dockyard dev                         # MCP server + Svelte dev server + local host simulator
                                     #   + schema watcher + contract typegen, all one process
dockyard generate                    # regenerate JSON Schema + web/src/generated/contracts.ts
                                     #   from Go contract structs (also runs inside `dev`)
dockyard validate                    # manifest, schemas, tool→UI mappings, MIME, host-compat,
                                     #   stale-typegen, required UI states — exit non-zero on fail
dockyard test                        # go test + contract tests + fixture/golden snapshots
                                     #   + host-compatibility matrix; the gate mcp-use lacks
dockyard build [--transport stdio|http]  # single embedded-asset binary; cross-compile matrix
dockyard install claude|cursor       # write host mcp.json, point at the binary, verify it starts
dockyard run --transport http --port 7331   # run HTTP service mode
dockyard inspect [--url <url>]        # standalone inspector against any running MCP server
```

Counterpart map (mcp-use → Dockyard):

| mcp-use | Dockyard | Delta Dockyard adds |
| --- | --- | --- |
| `create-mcp-use-app` | `dockyard new` | product-pattern templates; generates fixtures + tests + manifest |
| `mcp-use dev` | `dockyard dev` | + typegen watcher, + local host simulator, + fixture switcher |
| `mcp-use build` | `dockyard build` | single binary, embedded UI, cross-compile, no Node on target |
| `mcp-use start` | `dockyard run` | transport-selectable; same artifact for stdio/http |
| `@mcp-use/inspector` | `dockyard inspect` | in-binary, no separate package, fixture-aware |
| `@mcp-use/cli deploy` | *(no proprietary deploy)* | `build` emits portable artifacts; Portico is the optional control plane, OSS |
| *(none)* | `dockyard validate` | quality gate |
| *(none)* | `dockyard test` | contract tests, golden snapshots, host-compat |
| *(none)* | `dockyard generate` | first-class contract typegen |
| *(none)* | `dockyard install` | host-config wiring + boot verification |

Contract-first authoring (the §2.6 hole closed) — developer writes Go structs once; `dockyard generate` derives JSON Schema + Svelte/TS types + fixtures, so the widget cannot declare a shape that drifts from the tool:

```go
type ShowCustomerHealthInput struct {
    CustomerID string `json:"customer_id" jsonschema:"required"`
}
type ShowCustomerHealthOutput struct {
    Summary string         `json:"summary"`
    Score   int            `json:"score"`
    Signals []HealthSignal `json:"signals"`
}

app.Tool("show_customer_health").
    Input[ShowCustomerHealthInput]().
    Output[ShowCustomerHealthOutput]().
    UI("customer_health").
    Handler(handleCustomerHealth)
```

Widget auto-discovery, kept from mcp-use but Svelte-native: `.svelte` files under `web/src/` map to `ui://` resources by convention; the generated `contracts.ts` is the typed bridge — no hand-written `useWidget<T>()` generic.

## 4. Sharp edges & risks

- **Widget auto-discovery is a double-edged convention.** mcp-use's "drop a file, it registers" is loved but obscures the tool↔UI mapping. Dockyard keeps the convenience but must surface the wiring explicitly in `dockyard.app.yaml` so it stays inspectable (braindump: "reduce boilerplate, but not hide the architecture").
- **HMR for Svelte through the host simulator** is non-trivial: Dockyard runs a Go process *and* a Vite/Svelte dev server *and* an iframe simulator. mcp-use gets this from a single Node process. Dockyard must orchestrate two runtimes in `dev` without making it feel like two runtimes — a real engineering cost.
- **Typegen staleness** is a new failure mode Dockyard introduces by being contract-first. Mitigation: `dev` watches and regenerates; `validate`/`build` hard-fail on stale generated files (braindump build-blocker list).
- **Cross-protocol surface** (MCP Apps vs ChatGPT Apps SDK) — mcp-use absorbs this in `McpUseProvider`. Dockyard needs an equivalent Svelte shell or it loses portability that mcp-use already has.
- **Inspector scope creep.** The braindump's "observability-as-a-protocol console" could balloon. mcp-use kept the inspector deliberately small (4 tabs). Recommend Dockyard V1 inspector match that scope (test/debug only) and treat the multi-server observability console as a satellite — see Q-4.
- **No-CGo constraint** rules out some embedded-browser/screenshot approaches for the inspector and Studio screenshots; the inspector must be a pure-Go-served web UI.

## 5. What Dockyard must adopt / build / avoid

**Adopt (match mcp-use — these are table stakes):**

- One-command scaffold → one-command dev loop with a server already running.
- Widget-by-convention auto-discovery (Svelte files → `ui://` resources, no manual registration).
- Inspector on by default in `dev`, plus a standalone `dockyard inspect` mode with `--url`/`--port`/`--no-open` (CI-friendly).
- HMR that reaches the widget, not just the server.
- A cross-protocol widget shell (MCP Apps + ChatGPT Apps SDK) equivalent to `McpUseProvider`.
- A BYOK chat tab in the inspector to test model-driven tool selection without a real host.

**Build (beat mcp-use — Go-leveraged differentiators):**

- **Single embedded-asset binary** — `dockyard build` emits one CGo-free static executable: MCP server + tools + schemas + bundled Svelte UI. No Node on the target. Local stdio install is just `{"command": "/path/to/app"}`.
- **Generated contracts, not hand-declared types** — `dockyard generate` derives JSON Schema + Svelte/TS types + fixtures from Go structs; widget shapes cannot drift. This is the headline win over mcp-use's `useWidget<T>()`.
- **A real test toolchain** — `dockyard test` (contract tests, golden snapshots, host-compat matrix) and `dockyard validate` (manifest/schema/mapping/MIME/UI-state/stale-typegen gates). Quality enforced by the toolchain, not docs.
- **Product-pattern templates** — V1: `analytical-card`, `approval-flow`, `inspector`. Each generates fixtures, tests, manifest, and loading/empty/error/permission states by default.
- **`dockyard.app.yaml` manifest** as the single control plane driving validate/generate/build/install.
- **`dockyard install claude|cursor`** — write host config, point at the binary, verify boot. mcp-use has nothing here.
- **Fixture-driven local host simulator** with a fixture switcher (happy/empty/error/permission/slow/large) — lets UI work proceed before the backend is done.
- **Cross-compile matrix** in `build` (darwin/linux/windows × arm64/amd64) + checksums — trivial in Go, painful in Node.

**Avoid (do not copy mcp-use's mistakes):**

- Do not funnel deployment to a proprietary cloud. `build` emits portable artifacts; Portico is the optional, OSS control plane. (mcp-mesh.ai cautionary tale — Dump 4.)
- Do not ship a multi-package fan-out. One `dockyard` binary; no version-skew matrix.
- Do not make templates protocol-flavored (`starter`/`mcp-ui`/`apps-sdk`). Templates describe workflows.
- Do not lock widgets to React. Svelte is first-class for Dockyard's examples; the bridge contract should not assume a framework.
- Do not ship an inspector with no assertion/fixture model — an inspect-only surface (mcp-use's gap) is half a feature.

## 6. Open questions (Q-N — feed the RFC open-questions section)

- **Q-1.** Is `dockyard generate` a separate command, or only ever an implicit step inside `dev`/`build`? mcp-use has no typegen at all; making it explicit aids CI but adds surface. Recommend: explicit command *and* implicit in `dev`/`build`.
- **Q-2.** Does the V1 inspector include a BYOK Chat tab (model-driven tool selection), or is that deferred? It is mcp-use's most differentiated inspector feature but needs an LLM key path.
- **Q-3.** How does `dockyard dev` orchestrate the Go process + Svelte/Vite dev server + host simulator as one developer-facing experience? Single supervised process tree, or documented two-runtime model?
- **Q-4.** Is the observability-as-a-protocol multi-server console part of Dockyard V1, or a satellite product? mcp-use kept its inspector deliberately small; scope discipline matters here (raised in Dump 4).
- **Q-5.** Cross-protocol shell — does Dockyard commit to supporting the ChatGPT Apps SDK protocol in V1 (as `McpUseProvider` does), or MCP Apps only with Apps-SDK as a fast-follow?
- **Q-6.** Should `dockyard new` support a non-Svelte/no-UI "server-only" template for pure MCP Servers, given the mission covers MCP Servers *and* MCP Apps — and how does that interact with the 3-template V1 commitment?
- **Q-7.** What is the staleness-detection mechanism for generated contracts (content hash in the generated file header, manifest checksum, git-status check)? `validate`/`build` must hard-fail on stale output.
- **Q-8.** Does Dockyard provide a hosted deploy path at all, or stop at portable artifacts + Portico? Avoiding the mcp-use cloud-funnel is settled; whether an OSS-friendly `dockyard publish` exists is not.

## 7. Sources

- https://github.com/mcp-use/mcp-use — main repo README. Reachable.
- https://github.com/mcp-use/mcp-use-ts — TypeScript SDK repo (server SDK, widgets, hooks). Reachable.
- https://docs.mcp-use.com/inspector/cli — inspector CLI reference. Reachable.
- https://docs.mcp-use.com/typescript/server/widget-components/mcpuseprovider — `McpUseProvider` / `useWidget` docs (via 301 from `mcp-use.com`). Reachable.
- https://deepwiki.com/mcp-use/mcp-use/2.3-cli-tools — CLI tools / scaffolding / template list. Reachable; used as the source for the `starter`/`mcp-ui`/`apps-sdk` template list.
- https://www.npmjs.com/package/create-mcp-use-app — **HTTP 403 to WebFetch (unreachable)**; template details substituted from the DeepWiki mirror above.
- https://www.npmjs.com/package/@mcp-use/cli, https://www.npmjs.com/package/mcp-use, https://www.npmjs.com/package/@mcp-use/inspector — package metadata, surfaced via WebSearch result snippets (npm pages themselves 403 to WebFetch).
- WebSearch (May 2026) — corroborating snippets on CLI commands, `useWidget`, widget auto-discovery, and the deploy/cloud landscape.

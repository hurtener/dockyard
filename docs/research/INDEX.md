# Research briefs — subsystem index

Reverse map from "I'm authoring an RFC section or phase about X" → "these are the
briefs to read first." The briefs themselves live alongside this file
(`docs/research/NN-*.md`). They are the research substrate for `RFC-001-Dockyard.md`;
the RFC distills them into settled design. **A phase plan that doesn't cite at least
one informing brief is a drift signal.**

## Briefs at a glance

| Brief | Title | Date | Primary sources |
|------:|-------|------|-----------------|
| 01 | MCP Apps extension | 2026-05-20 | ext-apps spec 2026-01-26 (SEP-1865) |
| 02 | MCP Tasks extension | 2026-05-20 | experimental-ext-tasks (SEP-1686/2663) |
| 03 | Official Go MCP SDK audit | 2026-05-20 | `modelcontextprotocol/go-sdk` v1.6.0 |
| 04 | mcp-use DX teardown | 2026-05-20 | `mcp-use/mcp-use` + docs |
| 05 | Observability & competitive landscape | 2026-05-20 | OTel MCP semconv, mcp-mesh, inspectors |
| 06 | Go-2026 no-CGo stack & toolchain | 2026-05-20 | Go 1.26, codegen + build toolchain |

## Subsystem → briefs reverse index

When authoring an RFC section or phase plan whose subsystem matches the left column,
**read at least the bold briefs in the right column** before drafting.

| Subsystem | Briefs |
|-----------|--------|
| MCP server core / Go SDK integration | **03** |
| MCP Apps extension implementation (server-side) | **01**, 03 |
| MCP Tasks extension implementation (server-side) | **02**, 03 |
| Contract-first codegen pipeline (Go → JSON Schema → TS) | **06**, 04, 01 |
| CLI, scaffolding, templates, dev loop | **04**, 06 |
| Local inspector (test/debug host surface) | **05**, 04, 01 |
| Observability protocol (`obs/v1`) | **05** |
| Build, packaging, embedding, no-CGo toolchain | **06**, 04 |
| Host compatibility checks | **01**, 05 |
| Quality gates (`validate` / `test`) | **04**, 06, 01 |

Bold = primary brief. Non-bold = relevant context.

## Cross-brief synthesis (load-bearing findings)

These emerged across multiple briefs and are binding context for the RFC:

1. **Layer on the official Go SDK; never fork.** Apps and Tasks both ride on the SDK's
   `ServerCapabilities.AddExtension` + first-class `_meta` plumbing. A fork would forfeit
   the v1.x no-breaking-changes guarantee. (Briefs 01, 02, 03.)
2. **The SDK deliberately scoped first-class Apps support OUT** (issue #933, "all
   primitives in place"). That gap *is* Dockyard's reason to exist. (Briefs 03, 01.)
3. **Tasks is experimental with no released SDK API.** V1 builds a `_meta`/extension
   shim behind a versioned, code-generated wire layer — built against the
   `experimental-ext-tasks` schema, NOT the out-of-date `/extensions/tasks/overview`
   page (which documents a `tasks/update` method that does not exist). (Briefs 02, 03.)
4. **Contract-first codegen is the headline differentiator over mcp-use.** The Go
   contract struct is the single source of truth → JSON Schema + TypeScript both
   generated downstream, 100% pure-Go, no Node dependency. mcp-use has types but no
   contracts; server↔UI drift goes silent there. (Briefs 04, 06, 01.)
5. **Forward-compatibility is a hard requirement.** The Apps spec (stable 2026-01-26 +
   a live draft), Tasks (experimental), and the SDK (fast cadence, security defaults
   flip between releases) all move independently. All extension wire code must sit
   behind one internal interface with versioned codecs. (Briefs 01, 02, 03.)
6. **Observability is a headless protocol (`obs/v1`), modeled on Harbor Console.** The
   runtime emits a canonical versioned event stream; the inspector and the post-V1
   multi-server console are pure clients of it. OTel is an optional export adapter,
   never a prerequisite to observe. (Brief 05.)
7. **Avoid the cloud-funnel.** mcp-use funnels deployment to its proprietary cloud;
   mcp-mesh delegates observability to an external Grafana/Tempo/Redis stack. Dockyard
   observability is intrinsic and zero-dependency; `build` emits portable artifacts.
   (Briefs 04, 05.)
8. **The inspector is the one place Dockyard ships client-shaped code** — it must
   implement the *host half* of the `ui/` postMessage bridge to render Apps locally.
   This does not break the server-side-only scope; it is a test-only surface. (Briefs
   01, 04, 05.)

## How briefs interact with the RFC and phase plans

- **Briefs** are authoritative for *context*, not for design — the RFC and phase plans
  are where decisions land.
- **The RFC** distills brief findings into Dockyard's settled design.
- **A phase plan** translates an RFC section into a shippable unit of work.
- If a phase plan's design conflicts with a brief finding, the plan must explicitly
  justify the departure. Silent departure is forbidden.

## Adding a new brief

A new brief lands when a new subsystem is scoped that the briefs above don't cover, or
when an empirical validation produces design-shaping findings. Update this index in the
same change.

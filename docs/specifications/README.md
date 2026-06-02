# Vendored protocol specifications

External MCP specifications Dockyard consumes are mirrored into this directory so
the build is reproducible and the source of truth is searchable from inside the
repo (AGENTS.md §10). Each vendored file carries a header comment naming the
upstream URL and the commit SHA + date at the time of vendoring.

## Inventory

| Spec | Upstream | Vendored by |
|------|----------|-------------|
| MCP Apps extension — **prose** (`io.modelcontextprotocol/ui`, revision 2026-01-26, SEP-1865) | `github.com/modelcontextprotocol/ext-apps` — `specification/2026-01-26/apps.mdx` @ `298e884e` | the Apps-extension phase (RFC §7) |
| MCP Apps extension — **machine-readable schema** (same revision; `web/bridge/src/spec/ext-apps-schema.ts`) | `github.com/modelcontextprotocol/ext-apps` — `src/generated/schema.ts` @ `7d4434e` (2026-06-01) | v1.7 wave A (D-182) — the frontend wire-conformance source |
| MCP Tasks extension (`io.modelcontextprotocol/tasks`, experimental, SEP-1686/2663) | `github.com/modelcontextprotocol/experimental-ext-tasks` — `schema/draft/schema.ts` + `docs/specification/draft/tasks.mdx` | the Tasks-extension phase (RFC §8) |

> **The MCP Apps spec is vendored TWICE** — the prose `.mdx` (the Go runtime
> conforms to it via `internal/protocolcodec`) and the machine-readable
> `ext-apps-schema.ts` (the frontend bridge conforms to it; D-182). They are the
> **same spec revision** (2026-01-26) at **different upstream commits**
> (`298e884e` prose, `7d4434e` schema), content-equivalent as of v1.7 wave A.
> **Reconcile both pins whenever either is bumped** (see the checklist below) so
> the server-emitted wire and the View-validated wire never drift apart.

The actual spec files are vendored by the phase that first consumes them — the
phase plan records the exact pinned commit. Until then, the authoritative content
is summarized in the research briefs (`docs/research/01-mcp-apps-extension.md`,
`docs/research/02-mcp-tasks-extension.md`).

## Bumping a vendored spec

1. Open the target file's upstream URL at the new commit.
2. Replace the local copy verbatim; update the header comment's SHA + date.
3. Regenerate any code generated from it (`internal/protocolcodec`, RFC §10) and
   diff. A spec bump is a deliberate, reviewed change — never silent.
4. **For the MCP Apps spec, bump BOTH copies to the same upstream commit** — the
   prose `apps.mdx` and `web/bridge/src/spec/ext-apps-schema.ts` — and re-run both
   the Go golden tests and the frontend `conformance.test.ts`. Bumping one without
   the other silently diverges the server-emitted wire from the View-validated
   wire (the two are currently `298e884e` / `7d4434e`, content-equivalent).

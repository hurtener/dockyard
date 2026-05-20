# Vendored protocol specifications

External MCP specifications Dockyard consumes are mirrored into this directory so
the build is reproducible and the source of truth is searchable from inside the
repo (AGENTS.md §10). Each vendored file carries a header comment naming the
upstream URL and the commit SHA + date at the time of vendoring.

## Inventory

| Spec | Upstream | Vendored by |
|------|----------|-------------|
| MCP Apps extension (`io.modelcontextprotocol/ui`, revision 2026-01-26, SEP-1865) | `github.com/modelcontextprotocol/ext-apps` — `specification/2026-01-26/apps.mdx` | the Apps-extension phase (RFC §7) |
| MCP Tasks extension (`io.modelcontextprotocol/tasks`, experimental, SEP-1686/2663) | `github.com/modelcontextprotocol/experimental-ext-tasks` — `schema/draft/schema.ts` + `docs/specification/draft/tasks.mdx` | the Tasks-extension phase (RFC §8) |

The actual spec files are vendored by the phase that first consumes them — the
phase plan records the exact pinned commit. Until then, the authoritative content
is summarized in the research briefs (`docs/research/01-mcp-apps-extension.md`,
`docs/research/02-mcp-tasks-extension.md`).

## Bumping a vendored spec

1. Open the target file's upstream URL at the new commit.
2. Replace the local copy verbatim; update the header comment's SHA + date.
3. Regenerate any code generated from it (`internal/protocolcodec`, RFC §10) and
   diff. A spec bump is a deliberate, reviewed change — never silent.

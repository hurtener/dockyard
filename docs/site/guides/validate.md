# Validate + test

Two verbs share the quality-gate surface
([RFC §9.4](/reference/rfc)):

- **`dockyard validate`** — fast, build-blocker check (manifest,
  schemas, tool↔UI mappings, App MIME, spec compliance, four-state
  UI rule, stale-codegen drift).
- **`dockyard test`** — the full contract + compliance + capability
  gate.

Both exit non-zero on a regression; warnings report inline.

## `dockyard validate`

```bash
dockyard validate            # in the project directory
dockyard validate --dir path/to/project
```

The checks:

| Category           | What it catches                                              |
| ------------------ | ------------------------------------------------------------ |
| manifest           | malformed `dockyard.app.yaml`, missing required fields       |
| schemas            | the generated JSON Schema is not valid against draft-2020-12 |
| tool↔UI mappings   | a tool's `ui: <id>` does not match any `apps[].id`           |
| MIME               | an App's resource MIME is not `text/html;profile=mcp-app`    |
| spec compliance    | the Apps/Tasks shapes deviate from the vendored specs        |
| four-state UI rule | a fixture is missing for a required UI state (§20)           |
| stale-codegen      | the generated `*.gen.*` files no longer match the Go source  |
| CrossCheck         | the generated TS would differ from on-disk if regenerated (D-113) |

The schema check accepts only JSON Schema 2020-12. It rejects external `$ref`
and `$dynamicRef` targets and bounds schema bytes, depth, node count, and local
reference work. Input roots must resolve to objects; output roots may resolve to
any JSON value. The check is against Dockyard's pinned release-candidate
snapshot and does not claim conformance to an unaudited final specification.

`dockyard build` runs `validate` first; a blocker fails the build.

## `dockyard test`

```bash
dockyard test
dockyard test --skip-go-test
```

Categories:

| Category           | What it runs                                                  |
| ------------------ | ------------------------------------------------------------- |
| `go-test`          | `go test ./...` in the project                                 |
| `contract`         | generated artifacts match the Go contracts                    |
| `golden`           | fixtures / goldens are coherent                                |
| `spec-compliance`  | offline legacy + modern core conformance against vendored fixtures |
| `capability`       | the project degrades gracefully across host capability sets   |

The spec-compliance result reports both supported base MCP revisions:

```text
core 2025-11-25 [PASS]: initialize fixture conforms
core 2026-07-28 [PASS]: server/discover fixture conforms
```

These fixtures are embedded and run offline; `dockyard test` never contacts a
live host. MCP Apps `2026-01-26` uses the separate iframe `ui/initialize`
dialect. That handshake is not base MCP `server/discover` and is not presented
as a third core revision.

## Reading the report

Both verbs print:

```text
  blocker: tool "create_chart" references app "wigets" — no such app id
  warning: tool "summarise" has no fixture for the "permission" state
  validate: 1 blocker, 1 warning
```

A blocker exits non-zero; warnings don't change the exit code.

## CI shape

```yaml
- run: dockyard validate
- run: dockyard test
```

Both are idempotent and produce deterministic output. Pin them into
your CI workflow.

## See also

- [`validate` agent skill](/agent-skills/)
- [Contracts (Design A)](contracts)
- [Decisions: D-113 — `dockyard validate` runs CrossCheck](/reference/decisions)
- [Decision D-193 — bounded local schema validation](/reference/decisions)

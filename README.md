# Dockyard

> The paved road for production-grade MCP Apps.

Dockyard is a Go-native, web-aware framework for building **production-grade MCP
Servers and MCP Apps** — with a high minimum quality bar enforced by the toolchain,
a contract-first developer experience, and one-command packaging into a single
CGo-free static binary.

It is the third product in a three-part ecosystem:

```text
Portico  — the MCP gateway        (connects and governs tools)
Harbor   — the agent framework    (builds and runs agents; owns the MCP client)
Dockyard — the MCP Apps framework (builds the MCP servers and apps users touch)
```

> Portico connects. Harbor reasons. Dockyard presents.

## Status

**Pre-code — design phase.** The architecture is settled in
[`RFC-001-Dockyard.md`](RFC-001-Dockyard.md); implementation is planned in
`docs/plans/`. Dockyard is built with a doc-driven methodology — research briefs →
RFC → master phase plan → phased implementation — to keep design and code coherent
across a long build.

## What Dockyard does

- **Contract-first.** Typed Go structs are the single source of truth; JSON Schema,
  TypeScript types, and fixtures are generated — server↔UI drift is caught by the
  toolchain.
- **Full MCP protocol compliance.** Server-side implementation of the MCP **Apps**
  and **Tasks** extensions, built on the official Go MCP SDK.
- **Intrinsic observability.** Every server emits a canonical `obs/v1` event stream
  with zero external infrastructure; OpenTelemetry export is an optional adapter.
- **A local inspector.** Render, drive, and debug MCP Apps locally — no real host
  required.
- **One artifact, three modes.** A single CGo-free static binary runs over stdio,
  as an HTTP service, or behind Portico.

## Repository map

| Path | What |
|------|------|
| `RFC-001-Dockyard.md` | The design source of truth. |
| `docs/research/` | Phase-planning research briefs + `INDEX.md`. |
| `docs/plans/` | Master phase plan + per-phase plans. |
| `docs/decisions.md` | Append-only log of settled architectural decisions. |
| `docs/glossary.md` | Dockyard vocabulary. |
| `AGENTS.md` / `CLAUDE.md` | Contributor & agent normatives (binding, verbatim mirror). |

## Contributing

Read [`AGENTS.md`](AGENTS.md) first — it is binding. Build, test, and lint via the
`Makefile` (`make help`); the `make preflight` gate runs in CI and as a pre-commit
hook (`make install-hooks`).

## License

[Apache-2.0](LICENSE).

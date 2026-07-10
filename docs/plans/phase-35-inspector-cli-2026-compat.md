# Phase 35 — Inspector, CLI, and quality-gate compatibility

## Summary

Update Dockyard's test-only client-shaped surfaces, generated projects, CLI boot
checks, published documentation, and quality gates for the dual MCP lifecycle.
The inspector remains local, dev-mode-gated, and operator-initiated; it does not
become a production MCP client or OAuth credential manager.

## RFC anchor

- RFC §9
- RFC §10
- RFC §12
- RFC §19.1

## Briefs informing this phase

- brief 07
- brief 04
- brief 05
- brief 03

## Brief findings incorporated

- **brief 07 §1.1:** client capabilities and metadata are request-scoped in the
  modern lifecycle.
- **brief 04 §4:** DX tooling must make the secure paved road observable rather
  than hide protocol mechanics behind unexplained automation.
- **brief 05 §2:** the inspector is a local test/debug surface, not a production
  client.

## Findings I'm departing from (if any)

None.

## Goals

- Make every Dockyard-owned client-shaped test path negotiate modern discovery or
  deliberately exercise legacy fallback.
- Ensure scaffolds and documentation describe dual lifecycle behavior accurately.
- Add an offline, vendored-spec conformance category covering both revisions.

## Non-goals

- Production OAuth authorization or credential persistence.
- A hosted inspector, console, or host capability matrix.
- New user-facing template patterns beyond compatibility updates.

## Acceptance criteria

- [ ] `dockyard install` boot check succeeds against a modern server/discover
      server and a legacy-compatible server, with no initialize-only assumption.
- [ ] Inspector invoke, prompt, App-resource, and MRTR test paths speak the
      correct protocol revision and remain localhost-only.
- [ ] `dockyard new`, templates, examples, `dockyard run`, and `dockyard dev`
      default to the documented dual HTTP behavior.
- [ ] `dockyard test` reports conformance for both supported protocol versions
      against vendored fixtures without contacting a live host.
- [ ] All affected published docs and skills distinguish base MCP discovery from
      the separate `ui/initialize` iframe dialect.
- [ ] Existing operator flows have loading, empty, error, and ready states after
      any inspector UI change.

## Files added or changed

- `internal/inspector/*`, tests, and dev-loop attachment paths
- `internal/installpkg/*`, `internal/cli/*`, `internal/runpkg/*`
- `internal/scaffold/*`, `templates/*`, `examples/*`
- `internal/testgate/*`, `internal/validate/*`, conformance fixtures
- `docs/site/*`, `skills/*`, template READMEs
- `docs/plans/phase-35-inspector-cli-2026-compat.md`
- `scripts/smoke/phase-35.sh`

## Public API surface

- No new production MCP client API.
- `dockyard test` adds a documented protocol-conformance report category only if
  its result model can express both revisions without breaking existing callers.

## Design gate

- Map every inspector, boot-check, and generated-project client call onto either
  modern discovery or an explicit legacy fallback before implementation.
- The design owner approves the browser/live-verification script and confirms the
  inspector remains test-only, localhost-bound, and credential-free.

## Test plan

- **Unit:** inspector/boot-check protocol-mode selection, CLI report rendering,
  test-gate category dispatch.
- **Integration:** a generated project is built and driven by real modern and
  legacy SDK clients; the inspector performs an MRTR task flow locally.
- **Concurrency / golden:** inspector short-lived invocations under `-race`;
  protocol report fixtures/goldens; frontend `make web` coverage where touched.

## Smoke script additions

- Assert a scaffold dual-lifecycle integration test, boot-check test, inspector
  modern-call test, and documentation/skill references exist.

## Coverage target

- `internal/inspector`: 80% new-package band.
- `internal/cli`, `internal/testgate`, `internal/scaffold`: 70% CLI/tooling band.

## Dependencies

- 31 — SDK foundation.
- 32 — dual-lifecycle HTTP.
- 33 — MRTR and extension migration.
- 34 — modern contract/server semantics.
- 19, 20, 21, 22, 23 — existing dev/CLI/inspector surfaces.

## Risks / open questions

- The Go SDK's automatic client negotiation must be tested before raw inspector
  requests are retained; raw requests must carry all modern metadata and headers.
- Documentation must not call base MCP discovery `ui/initialize`, which remains
  a distinct Apps iframe dialect.

## Glossary additions

- N/A — compatibility vocabulary is established in earlier phases.

## Pre-merge checklist

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `npx markdownlint-cli2 "**/*.md" "!**/node_modules"` passes
- [ ] `make docs` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check in this PR
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`

# Phase 27 — Security pass + spec-compliance conformance

## Summary

The V1-readiness phase. A cross-cutting security audit against `AGENTS.md` §7
(hostile input on every wire-decoding surface, the HTTPSecurity posture under
load + adversarial traffic, the Tasks security model end-to-end, the inspector's
localhost-bound and operator-initiated boundaries) and a real MCP
spec-compliance conformance suite built against the vendored spec snapshots
(`docs/specifications/`). Each defect surfaced during the audit is fixed in the
same PR. The folded-in CI hygiene fix surfaces `go test` output on a
`make coverage` failure so a flake is diagnosable from the CI log.

## RFC anchor

- RFC §15 — Security
- RFC §16 — Forward-compatibility strategy
- RFC §7 — The MCP Apps extension (audited surface)
- RFC §8 — The MCP Tasks extension (audited surface)
- RFC §13 — Persistence & the storage seam (audited surface)
- RFC §11 — Observability (capture policy)

## Briefs informing this phase

- brief 01 — MCP Apps extension
- brief 02 — MCP Tasks extension
- brief 03 — Official Go MCP SDK audit
- brief 05 — Observability & competitive landscape (the CVE-2025-49596 inspector lesson)

## Brief findings incorporated

- **Brief 02 §4.5 "Avoid":** "Do not leak another context's task existence."
  The Tasks adversarial sweep asserts cross-context access is reported
  indistinguishably from "task not found" — same error class, same JSON-RPC
  code, same message.
- **Brief 02 §4.6:** the per-requestor concurrency cap is the resource-
  exhaustion guard. The adversarial sweep saturates a requestor to the cap,
  proves the cap rejects further work, and proves the TTL purge sweep reaps
  expired tasks without racing in-flight handlers.
- **Brief 02 §4.5 task-ID entropy:** ≥128 bits, drawn from `crypto/rand`.
  The sweep asserts both structurally (the source is `crypto/rand`) and
  statistically (many IDs, vanishing duplicate probability).
- **Brief 03 §2.3:** the go-sdk's security defaults have flipped between
  releases (`CrossOriginProtection` on in v1.4.1, off in v1.6.0). The
  HTTPSecurity stress fixes Dockyard's posture EXPLICITLY and asserts that
  every adversarial request is rejected at the right layer regardless of
  what the linked SDK defaults to.
- **Brief 05 §4.2 (CVE-2025-49596):** the inspector is dev-mode-gated and
  localhost-only. The inspector re-audit drives every plausible non-loopback
  bind shape (IPv6 unspecified, IPv6 interface, IPv4 wildcard, hostname-as-
  external) at `New` and asserts each is rejected before the listener opens.
- **Brief 03 §RFC §16:** spec compliance must survive a moving protocol;
  codecs are versioned + keyed on negotiated `protocolVersion`; deprecated
  shapes tolerated on read, never emitted. The conformance suite exercises
  this for every supported version + every codec method.

## Findings I'm departing from (if any)

None. Phase 27 is an audit — it tightens existing properties rather than
trading any off.

## Goals

- Every wire-decoding surface in Dockyard has hostile-input fuzz coverage
  with a meaningful seed corpus + a uniform "never panic; round-trip stable
  where applicable; typed error on malformed input" invariant.
- The explicit HTTPSecurity middleware chain
  (`CrossOriginProtection( ContentType( Mount( SDKHandler )))`) is proven
  under concurrent + adversarial load: every malicious request is rejected
  at the right layer; no valid request is incorrectly rejected; no
  goroutine leak; no panic; no Tasks lifecycle corruption.
- The Tasks security model is proven end-to-end against the live wire:
  cross-context rejection is indistinguishable from "not found", `tasks/list`
  is correctly withheld for unauthenticated requestors, the per-requestor
  concurrency cap holds under load, the TTL purge sweep is race-free, task
  IDs are crypto-strong + ≥128-bit, and `supplyInput` rejects every
  adversarial path with a typed error.
- A real **MCP spec-compliance conformance suite** rounds-trips every
  Apps + Tasks wire shape through the codec for every supported
  `protocolVersion`, byte-comparing against fixtures keyed on the vendored
  spec snapshots' pinned commit SHAs. Deprecated shapes (the flat
  `_meta["ui/resourceUri"]`) are read-tolerated and never emitted.
  Capability-negotiation invariants are asserted: an unknown version yields
  a typed error from `CodecForStrict`; an advertised capability is served;
  an unadvertised one is not.
- The inspector security re-audit holds: localhost-only is mechanically
  enforced for every bind shape; the "read-only" framing is precisely
  re-cast as "operator-initiated only" with a clarifying decision; the
  production `mcp.NewClient` call surface is documented and bounded.
- The folded-in CI hygiene fix lands: `make coverage`'s suppressed `go test`
  output is captured to a temp log, suppressed on success, surfaced on
  failure so a CI flake is diagnosable from the log alone.

## Non-goals

- Adding any new product surface. Phase 27 only audits and hardens what
  V1 already ships.
- Re-litigating a settled decision. A finding that requires a design change
  is filed as a decision and a follow-up phase plan, not a silent patch.
- Building a fuzz-orchestration CI runner. CI runs the fuzz seed corpora as
  ordinary tests (the Go fuzz convention); a longer fuzz session is run
  on-demand.

## Acceptance criteria

- [ ] Hostile-input fuzz coverage exists at every wire-decoding surface
      (`internal/protocolcodec`, `internal/manifest`, `internal/codegen`,
      `runtime/tool`, every inspector HTTP endpoint, the Tasks JSON-RPC
      frame parser). Each target ships a meaningful seed corpus + a
      uniform "never panic" invariant.
- [ ] `test/integration/phase27_httpsecurity_stress_test.go` ships an
      adversarial-concurrent stress test (≥20 clients, mixed
      valid + invalid + cross-origin + wrong-Content-Type + oversized +
      malformed-JSON + tasks/* + non-tasks frames) and passes under
      `-race` with no goroutine leak and no panic.
- [ ] `test/integration/phase27_tasks_security_test.go` ships the Tasks
      security adversarial sweep — cross-context indistinguishable from
      not-found, `tasks/list` withheld for unauth, concurrency cap +
      TTL purge under load, ≥128-bit crypto-strong ID assertion,
      `supplyInput` cross-context + double + missing — and passes
      under `-race`.
- [ ] `test/conformance/` ships an MCP spec-compliance conformance suite
      that round-trips every Apps + Tasks wire shape against vendored-
      spec-derived fixtures for every supported `protocolVersion`,
      asserts the deprecated-shape read/emit policy, and asserts the
      capability-negotiation invariants. Each conformance test cites the
      vendored spec source + section.
- [ ] `test/integration/phase27_inspector_security_test.go` ships the
      inspector security re-audit — every adversarial bind shape rejected
      at `New`, the `mcp.NewClient` audit captured.
- [ ] `Makefile`'s `coverage` target captures `go test` output to a temp
      log, suppresses it on success, and surfaces it on failure;
      `scripts/smoke/phase-27.sh` asserts the `>/dev/null` foot-gun is
      no longer in the recipe.
- [ ] `docs/decisions.md` carries the D-143..D-N entries the phase
      produced.
- [ ] `scripts/smoke/phase-27.sh` asserts every acceptance criterion above
      with `OK ≥ count(criteria)` and `FAIL = 0`.
- [ ] Coverage on touched packages ≥ the §11 / Phase 27 90% bar where it
      lands cleanly; documented overrides in
      `internal/coveragecheck/coverage.json` for any package that cannot
      reach 90% honestly through unit tests, citing the integration test
      as the coverage path.

## Files added or changed

- `docs/plans/phase-27-security-conformance.md` (new — this plan)
- `scripts/smoke/phase-27.sh` (new — the per-phase smoke check)
- `internal/protocolcodec/fuzz_test.go` (extended — additional fuzz
  targets for every codec method + extended seed corpora)
- `internal/manifest/fuzz_test.go` (extended — additional adversarial
  YAML seeds)
- `internal/codegen/fuzz_test.go` (extended — additional crashers)
- `runtime/tool/fuzz_test.go` (extended — additional adversarial seeds)
- `runtime/tasks/fuzz_test.go` (new — the JSON-RPC frame parser at
  `Mount.HandleFrame` and `Mount.HTTPMiddleware`)
- `internal/inspector/fuzz_test.go` (new — adversarial bytes against
  every HTTP endpoint)
- `internal/inspector/inspector_security_test.go` (new — adversarial
  bind shape rejection sweep)
- `test/integration/phase27_httpsecurity_stress_test.go` (new)
- `test/integration/phase27_tasks_security_test.go` (new)
- `test/integration/phase27_inspector_security_test.go` (new)
- `test/conformance/conformance_test.go` (new — the MCP spec-compliance
  conformance suite)
- `test/conformance/fixtures/` (new — vendored-spec-derived JSON
  fixtures with SHA + section citations in headers)
- `Makefile` (the `coverage` recipe diagnostic-hygiene fix)
- `docs/decisions.md` (D-143..D-N appended)
- `internal/coveragecheck/coverage.json` (any new override entries
  documenting integration-test coverage paths for new packages)

## Public API surface

No new public API. Phase 27 is an audit. The only surface delta is the
`runtime/tasks` package gaining a test-only helper to allow
adversarial in-process driving (lives under `_test.go`); the production
surface is unchanged.

## Test plan

- **Unit:** the new fuzz targets (every wire-decoding surface) run their
  seed corpora as ordinary unit tests; the `inspector_security_test.go`
  bind-shape rejection sweep runs in-package; the conformance round-trip
  asserts run as table-driven tests.
- **Integration:** the HTTPSecurity stress test (real
  `runtime/server` + real `tasks.Engine` + real HTTP client + the
  explicit posture); the Tasks security adversarial sweep (real
  engine + real Mount + real auth-context propagation); the inspector
  security re-audit (real `inspector.New`).
- **Concurrency:** the HTTPSecurity stress test drives N≥20 concurrent
  clients under `-race`; the Tasks concurrency-cap test saturates a
  requestor in parallel; the TTL purge runs concurrently with in-flight
  handlers.
- **Conformance:** `test/conformance/` builds fixtures from the vendored
  spec snapshots; each test cites its source.
- **Golden:** the conformance round-trip is itself a golden comparison
  (decoded → re-encoded bytes match the fixture).

## Smoke script additions

- `scripts/smoke/phase-27.sh` asserts:
  - the new fuzz targets exist at every named surface;
  - the HTTPSecurity stress test file exists + names the right function;
  - the Tasks security adversarial test exists;
  - the conformance suite directory + at least one test file exist;
  - the inspector security re-audit test exists;
  - the `Makefile` `coverage` recipe is free of the suppressing
    `>/dev/null` foot-gun.

## Coverage target

- `internal/inspector` — 90% (new tests folded into the existing suite)
- `runtime/tasks` — 90% (existing 90% bar held; new tests add
  cross-context + adversarial coverage)
- `internal/protocolcodec` — 90% (existing bar held; new fuzz +
  conformance coverage)
- `runtime/server` — held at its current band; the HTTPSecurity stress
  test contributes to it
- A new `test/conformance/` package has its own band 90% if measured;
  otherwise the assertion is on the codec packages it exercises.

## Dependencies

- 09 — Apps extension surface (the audited Apps wire shapes).
- 13 — Tasks server surface (the audited engine + capability set).
- 14 — TaskStore (the audited Store seam + auth-context binding).
- All wave-9 + wave-10-thus-far phases ship the surface in scope: server
  core, codegen, manifest, Apps, Tasks (with `input_required` +
  transport mount D-108/D-110), obs (with SSE + OTel + trace
  correlation D-114/D-121/D-122), the CLI (8 verbs), the inspector
  (operator-invoke D-131 + App rendering D-103 + elicitation D-134),
  the two templates.

## Risks / open questions

- **A latent defect surfaced under load.** Likely — the audit's whole
  point. The phase plan budgets per-defect `fix(...)` commits in the
  same PR (per the user's "find them and fix them" bar).
- **A spec wording the vendored snapshot is silent on.** The
  conformance fixture-derivation discipline guards this: a shape we
  cannot point at a snapshot line for is not asserted in the suite.
  Surfaces such a gap is a real finding — file a decision and an
  upstream-spec follow-up.
- **The fuzz seed corpus growing past CI-runtime budget.** Mitigated:
  CI runs the seed corpus as ordinary unit tests (fast); a longer fuzz
  session is on-demand only.

## Glossary additions

- "**Conformance suite**" — the `test/conformance/` package that round-
  trips every wire shape through `internal/protocolcodec` against
  vendored-spec-derived fixtures, asserting Dockyard's spec compliance
  for V1 (RFC §16).
- "**Operator-initiated**" — an inspector write surface driven by a
  deliberate UI action from the local operator; never by the server,
  never by an off-localhost actor. The precise framing the inspector's
  doc claim uses post-D-NNN.

## Pre-merge checklist

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check in this PR
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`

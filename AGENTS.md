# Dockyard — Contributor & Agent Normatives

> This file is **binding** for anyone — human or AI — modifying this repository.
> It is mirrored **verbatim** in `CLAUDE.md` so Claude Code picks it up
> automatically. If the two files diverge, the most recent commit timestamp wins;
> flag the drift in your PR.

If a rule below conflicts with the RFC or a phase plan, the **RFC wins**, then the
**phase plan**, then this file. Update whichever artifact is wrong; never silently
ignore the conflict.

---

## Starting a new session — orientation (READ THIS FIRST)

Dockyard is a multi-phase, doc-driven build. The design surface is large on purpose:
hygiene up front is cheaper than retrofitting it. Before substantive work, skim, in
order:

1. **§1 — What Dockyard is.** The product and its four binding properties.
2. **§2 — Authoritative sources.** The priority chain: RFC > phase plans > this file
   > research briefs > code comments.
3. **§16 — Authoring a phase plan.** The binding workflow for any contributor
   touching a phase. Skipping it is the single largest source of design drift.

**Drift-hygiene artifacts (live references):**

- `RFC-001-Dockyard.md` — the design source of truth.
- `docs/decisions.md` — append-only log of settled decisions (`D-NNN`). When tempted
  to re-litigate something, grep here first.
- `docs/glossary.md` — Dockyard vocabulary. New terms land here in the same PR.
- `docs/research/INDEX.md` — subsystem → research-brief reverse index.
- `docs/plans/_template.md` — phase plan template; new phases start as a copy.
- `scripts/drift-audit.sh` — mechanical drift checks (`make drift-audit`).

If asked to do something that doesn't fit a phase (a one-off fix, a question, a small
doc edit), proceed without the full §16 ritual — but mention any drift risk you spot.

---

## 1. What Dockyard is

Dockyard is a Go-native, web-aware framework for building **production-grade MCP
Servers and MCP Apps**. It is the third product in a three-part ecosystem:

```text
Portico  — the MCP gateway       (connects and governs tools)
Harbor   — the agent framework   (builds and runs agents; owns the MCP client)
Dockyard — the MCP Apps framework (builds the MCP servers and apps users touch)
```

Dockyard ships **one CGo-free static binary** — the `dockyard` CLI — plus an app
runtime library that generated apps import. A developer scaffolds or starts blank,
writes typed Go tool handlers, optionally attaches Svelte UI resources, and gets
generated contracts, a local inspector, quality gates, an intrinsic observability
stream, and one-command packaging.

**Four binding properties.** Three are product properties; one is a scope boundary.
A change that weakens any of them is wrong — reach for the RFC, not the keyboard.

1. **P1 — Contract-first.** A tool's input and output are typed Go structs; JSON
   Schema, TypeScript types, and fixtures are *generated*, never hand-written.
2. **P2 — Observability is a protocol.** The runtime is headless and emits the
   canonical `obs/v1` event stream. The inspector and any future console are pure
   clients of that contract; they never read runtime internals.
3. **P3 — Forward-compatibility by isolation.** All MCP extension wire formats live
   behind one internal seam (`internal/protocolcodec`); a spec bump is localized.
4. **P4 — Server-side only.** Dockyard builds MCP *servers* and apps. Harbor owns
   the MCP client. The one client-shaped component — the inspector — is a local,
   test-only, dev-mode-gated surface.

---

## 2. Authoritative sources (in priority order)

1. `RFC-001-Dockyard.md` — product intent and design decisions.
2. `docs/plans/phase-NN-*.md` — implementation specifications. Acceptance criteria
   are binding.
3. `docs/plans/README.md` — the master phase plan: cross-cutting conventions and the
   phase index.
4. This file (`AGENTS.md` / `CLAUDE.md`) — operational rules.
5. `docs/research/*.md` — phase-planning research briefs. Authoritative for
   *context*, not for design — the RFC and phase plans are where decisions land.
6. Code comments and godoc — last and least authoritative.

When a phase plan and the RFC drift, the RFC wins. File a follow-up to fix the plan.

---

## 3. Repository layout

```text
.
├── RFC-001-Dockyard.md          # design RFC — source of truth
├── README.md
├── AGENTS.md / CLAUDE.md        # this file (verbatim copies)
├── LICENSE                      # Apache-2.0
├── Makefile                     # canonical build / test / lint commands
├── go.mod / go.sum
├── .github/                     # CI, PR template, codeowners, dependabot
├── .golangci.yml / .markdownlint.yaml / .editorconfig / .gitignore
├── cmd/
│   └── dockyard/                # the `dockyard` CLI binary entrypoint
├── internal/                    # CLI / generator internals (not externally importable)
│   ├── cli/                     # cobra command tree
│   ├── scaffold/                # `dockyard new` project generation
│   ├── codegen/                 # Go → JSON Schema + TypeScript (Design A, RFC §6)
│   ├── manifest/                # dockyard.app.yaml schema + loader (RFC §4.2)
│   ├── devloop/                 # `dockyard dev` fsnotify orchestrator (RFC §9.2)
│   ├── inspector/               # the local inspector (RFC §12)
│   └── protocolcodec/           # MCP extension wire-format isolation seam (RFC §5.4)
├── runtime/                     # the Dockyard app runtime — a LIBRARY imported by apps
│   ├── server/                  # MCP server core over go-sdk (RFC §5)
│   ├── apps/                    # MCP Apps extension layer (RFC §7)
│   ├── tasks/                   # MCP Tasks extension layer (RFC §8)
│   ├── obs/                     # obs/v1 observability runtime (RFC §11)
│   └── store/                   # the Store seam + drivers {sqlite, inmem} (RFC §13)
├── web/
│   └── bridge/                  # the Svelte bridge shell library (RFC §7.3)
├── templates/                   # `dockyard new --template` sources (RFC §10)
├── examples/
├── test/integration/
├── scripts/
│   ├── preflight.sh             # the preflight gate
│   ├── drift-audit.sh           # design-coherence checks
│   ├── smoke/                   # per-phase smoke scripts
│   ├── hooks/pre-commit
│   └── install-hooks.sh
└── docs/
    ├── plans/                   # master plan (README.md) + phase plans + _template.md
    ├── research/                # phase-planning research briefs + INDEX.md
    ├── specifications/          # vendored MCP spec snapshots
    ├── decisions.md             # append-only D-NNN log
    └── glossary.md
```

Directories are created as the phases that own them land. Anything that doesn't have
a home above is wrong — if you need a new top-level directory, propose it in the RFC
first; `§3` is the binding layout.

---

## 4. Build, test, lint, run

All targets are canonical and run by CI. Targets no-op gracefully before the code
they act on exists.

```bash
make build         # build the dockyard binary (CGo-free static)
make test          # go test -race ./...
make vet           # go vet ./...
make lint          # golangci-lint run
make drift-audit   # design-coherence checks (RFC/plans/briefs/mirror/forbidden names)
make check-mirror  # verify AGENTS.md == CLAUDE.md
make preflight     # build + smoke checks + drift-audit
make install-hooks # install the pre-commit hook (one-time, per clone)
```

### 4.1 Preflight gate — non-negotiable

`make preflight` is the same gate the pre-commit hook and CI enforce: it builds,
runs every per-phase smoke script (which SKIP gracefully where the surface isn't
built yet), and runs `drift-audit`. Do not bypass the pre-commit hook with
`--no-verify` outside a documented emergency.

### 4.2 Phase implementor contract

A phase is **done** only when: (a) every acceptance criterion in its plan passes;
(b) coverage targets for touched packages are met; (c) `scripts/smoke/phase-NN.sh`
reports `OK ≥ count(criteria)` and `FAIL = 0`; (d) prior phases' smoke scripts still
pass. A new CLI command, RPC surface, or public API ⇒ a smoke check in the **same**
PR. A new config key or manifest field ⇒ documented in the plan, the example
manifest, and a smoke check.

### 4.3 Reasonable plan deviations

Plans are specifications, not straitjackets. A reasonable deviation discovered
during implementation is fine — document it in the PR description and update the
plan file **in the same PR**. Silent divergence from a plan or the RFC is drift.

### 4.4 Extensibility seams (project-wide policy)

Any subsystem with a plausible alternate backend lives behind an **interface +
factory + driver** pattern. V1 mandates this for: the `Store` (RFC §13 — sqlite +
inmem now, Postgres later), the `obs` emitter (ring buffer / SSE / OTel), and the
MCP host profiles (RFC §7.5). Drivers register via `init()` blank-import.

---

## 5. Code conventions (Go)

- **Toolchain.** Go 1.26, pinned in every `go.mod`. **No CGo** — `CGO_ENABLED=0` is
  enforced by CI; a dependency that needs CGo is rejected.
- **Style.** `gofmt -s`; `go vet` and `golangci-lint run` clean. Generated code is
  marked with a `// Code generated … DO NOT EDIT.` header and stays boring and
  readable (RFC: "generated code teams are happy to own").
- **Errors.** `errors.Is`/`errors.As`, `%w` wrapping, sentinel errors,
  `errors.Join`. Wrap with context. **Never `panic` for control flow** and never
  panic across the MCP boundary.
- **Context.** `context.Context` is the first parameter of any call that does I/O,
  blocks, or can be cancelled. Honour cancellation.
- **Logging.** `log/slog` only — no `log.Printf`, no `logrus`/`zap`. JSON handler in
  production, text under `dockyard dev`. No unredacted secrets in logs.
- **Concurrency.** Race detector mandatory on tests. A reusable artifact (a server,
  an emitter, a store, a codec) must be safe under concurrent use; prove it.
- **Tests.** Table-driven where it fits; golden tests for codegen output; `-race`
  always.
- **JSON.** Stdlib `encoding/json` (v1). `encoding/json/v2` is deferred (RFC §17).

---

## 6. The non-negotiable product rules

These enforce P1–P4 (§1). They are binding on every phase.

- **Contract-first (P1).** No hand-written JSON Schema or TypeScript types for a
  tool contract. The Go struct is the source of truth; `dockyard generate` produces
  the rest; `dockyard validate` fails on stale or drifted generated output.
- **Observability is a protocol (P2).** The runtime emits `obs/v1`; the inspector
  consumes it. No component reads runtime internals to observe — if you need a
  signal, add an `obs/v1` event, don't add a back channel.
- **Forward-compatibility by isolation (P3).** MCP extension wire formats
  (`_meta` key shapes, capability blocks, Tasks envelopes) live **only** in
  `internal/protocolcodec`. Handler-facing and manifest-facing APIs never expose raw
  protocol structs.
- **Server-side only (P4).** No production MCP client. The inspector is the lone
  client-shaped component; it is test-only, dev-mode-gated, and localhost-bound.
- **Capability-driven, never a host matrix.** Host support is read from the MCP
  capability-negotiation handshake at run time; features degrade gracefully. Do not
  hardcode a per-host capability matrix — it would always drift. Host-specific
  *derivations* (e.g. a signed iframe origin) live behind pluggable host profiles.
- **No-CGo, single binary.** Every artifact compiles CGo-free and cross-compiles.

---

## 7. Security — non-negotiable rules

- No hardcoded secrets, anywhere — including generated code and tests.
- MCP Apps render in a sandboxed iframe under a deny-by-default CSP; single-file
  bundles are the default. Domains and iframe permissions are opt-in via the
  manifest. A host may further restrict but never loosen these.
- Tasks: crypto-strong (≥128-bit) task IDs; auth-context binding rejects
  cross-context access; `tasks/list` is withheld when requestors aren't
  identifiable; enforced max TTL and per-requestor concurrency caps.
- HTTP transport: DNS-rebinding, Origin/Content-Type, and cross-origin protections
  are set **explicitly** — never inherited from an SDK default (defaults have
  flipped between SDK releases).
- The inspector is dev-mode-gated, localhost-only, read-only — never a production
  client and never an arbitrary-execution proxy.
- `obs/v1` tool input/output capture defaults to shape + size; full-content capture
  is opt-in and redaction-aware.

---

## 8. Observability — the `obs/v1` rules

- `obs/v1` is a **versioned, public, third-party-consumable** contract. A change to
  the event shape is a versioned change, documented, never silent.
- The runtime emits; it never blocks on a slow consumer. Emit paths are
  non-blocking (ring buffer, bounded fan-out).
- OTel export is a V1 adapter behind the `obs` emitter seam, off by default; it is
  never a prerequisite to observe locally.
- The MCP `logging` capability is *bridged* into `obs/v1` `log` events — a Dockyard
  server still speaks standard MCP logging to any client.

---

## 9. Persistence — the `Store` seam rules

- All durable state goes through the `Store` interface (RFC §13). V1 driver:
  `modernc.org/sqlite` (pure-Go, CGo-free). In-memory driver for stdio single-user.
- A new persistence concern adds a method to the seam and is implemented by **every**
  driver, proven by the shared conformance suite — not bolted onto one driver.
- Migrations are forward-only; never edit a migration after it merges.

---

## 10. Forward-compatibility — the `protocolcodec` rules

- Every MCP spec Dockyard consumes is vendored into `docs/specifications/`, pinned
  by upstream commit SHA + date. A spec bump is a deliberate, reviewed update.
- `internal/protocolcodec` is the only package that imports raw MCP extension wire
  types. Codecs are versioned and keyed on the negotiated `protocolVersion`;
  deprecated shapes are tolerated on read, never emitted.
- The Tasks wire layer is code-generated from the vendored experimental schema, so a
  spec revision is regenerate-and-diff.

---

## 11. Testing rules

- `-race` on every test run. CI fails on a race.
- Codegen output is covered by **golden tests** (fixed input → fixed output).
- A phase that consumes another subsystem's surface, or closes a cross-subsystem
  seam, ships an **integration test** with real drivers — see §17.
- Spec compliance is tested against the vendored specs, not against live hosts.
- Coverage defaults (override per phase): 80% new packages; 85% the `Store` drivers
  and the conformance-tested subsystems; 70% CLI / tooling.

---

## 12. Commit and PR conventions

- **Commits:** imperative mood, scoped (`feat(apps): …`, `fix(codegen): …`,
  `chore: …`, `docs: …`). Small and coherent.
- **Branches:** never commit feature work directly to `main`; use `feat/phase-NN-*`
  (or `chore/*`, `docs/*`). Once the project is past scaffolding, do not modify
  `main` directly — use a worktree or branch.
- **PRs:** reference the RFC section(s) and the phase. State any plan deviation and
  update the plan in the same PR. The pre-merge checklist (§14) gates the PR.
- **Merge:** squash unless history is meaningful. CI green is mandatory.

---

## 13. Forbidden practices

- Hardcoded secrets, including in tests.
- `panic` for control flow; panicking across the MCP boundary.
- A CGo dependency, or building with `CGO_ENABLED=1`.
- Hand-written JSON Schema or TypeScript for a tool contract (violates P1).
- Reading runtime internals to observe instead of emitting an `obs/v1` event
  (violates P2).
- Importing raw MCP extension wire types outside `internal/protocolcodec`
  (violates P3).
- Shipping a production MCP client, or making the inspector reachable off-localhost
  (violates P4).
- Hardcoding a per-host capability matrix (§6).
- Adding a CLI command, manifest field, or public API without a smoke check in the
  same PR.
- Editing a migration after merge.
- Bypassing the pre-commit hook with `--no-verify` outside a documented emergency.

---

## 14. Pre-merge checklist

- [ ] `make drift-audit` passes.
- [ ] `make check-mirror` passes (`AGENTS.md` == `CLAUDE.md`).
- [ ] `make preflight` passes.
- [ ] `go test -race ./...` and `golangci-lint run` are clean.
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve.
- [ ] Coverage on touched packages ≥ the phase's stated target.
- [ ] A new CLI command / manifest field / public API has a smoke check in this PR.
- [ ] If a reusable artifact changed: a concurrent-reuse test passes under `-race`.
- [ ] If a cross-subsystem seam was opened or consumed: an integration test exists
      (§17).
- [ ] New vocabulary added to `docs/glossary.md` in this PR.
- [ ] A new architectural decision (or a departure from a brief) is filed in
      `docs/decisions.md`.

---

## 15. When in doubt

The RFC wins. If the RFC is silent, the phase plan decides; if both are silent,
raise it — do not invent a decision and bury it in code. A new settled decision is
an entry in `docs/decisions.md`; a change to a settled decision is an RFC PR plus a
superseding decision entry, never a silent edit.

---

## 16. Authoring a phase plan (workflow)

The canonical workflow for any contributor starting a phase. The drift-audit gate
enforces what it can; this workflow covers what it can't.

1. **Read the master plan entry.** Open `docs/plans/README.md`, find the Phase N
   detail block. Note owning subsystem, RFC sections, dependencies, risks.
2. **Read the cited RFC sections** in `RFC-001-Dockyard.md`.
3. **Read the relevant briefs** per `docs/research/INDEX.md`. A phase plan that
   cites no informing brief is a drift signal.
4. **Read the glossary** for any term you're unsure about; pre-write the entry for
   any new term you introduce.
5. **Read the decisions log** (`docs/decisions.md`) for entries touching this
   subsystem. Settled decisions are not re-litigated silently.
6. **Copy the template:** `cp docs/plans/_template.md docs/plans/phase-NN-slug.md`.
   Fill every section. "Brief findings incorporated" and "Findings I'm departing
   from" are forcing functions — they make brief inheritance visible.
7. **Author the smoke skeleton:**
   `cp scripts/smoke/_template.sh scripts/smoke/phase-NN.sh`.
8. **Run `make drift-audit` and `make preflight`** before committing.
9. **Commit only when both pass.** The PR references the RFC section and any
   superseded decision.

---

## 17. End-to-end + integration testing

Per-package unit tests miss two classes of bug: **cross-package wiring gaps** (two
phases each ship their half of a seam, neither connects them) and **cross-subsystem
concurrency interactions**.

A phase ships an integration test whenever its `Deps` name a different subsystem's
shipped phase, or it closes a seam another phase opened, or it introduces a public
interface other phases will build on. Integration tests use **real drivers** on the
seam (no mocks at the boundary), prove identity/capability propagation, cover ≥1
failure mode, and run under `-race`. They live in-package when the package *is* the
wiring boundary, otherwise in `test/integration/`.

At wave boundaries a read-only **checkpoint audit** reviews every shipped phase for
wiring gaps, RFC drift, weak tests, and hygiene regressions, and lands its punch
list as one `chore(checkpoint)` PR. When an integration test surfaces a bug, fix it
in the same PR — even when the root cause is in an earlier phase.

---

## 18. Mirroring

`AGENTS.md` and `CLAUDE.md` are kept **verbatim identical**. After any edit:

```bash
diff -q AGENTS.md CLAUDE.md   # expected: no output
```

CI enforces this; the `mirror` job fails the build if they differ.

# Phase 29 — Agent skills & published tech-docs site

<!--
The first phase of Wave 10 (Hardening & release). Ships:
  1. Dockyard's V1 agent-skill set under skills/, authored in the
     SKILL.md format per agentskills.io conventions, covering the
     core developer workflows an MCP App builder needs from day one
     (scaffold, add a tool, attach a UI resource, define contracts,
     run the dev loop, validate, package, test with the inspector).
  2. The GitHub Pages technical-documentation site under docs/site/,
     built with VitePress (D-137) and deployed by a new CI workflow,
     stitched together from the in-repo docs (RFC, plans, decisions,
     glossary, design conventions, the skills index, and per-template
     getting-started walkthroughs).
  3. The mechanical guardrails the §19 hygiene rule needs once the
     two surfaces exist: a SKILL.md frontmatter validator
     (internal/skillcheck) and an extended scripts/drift-audit.sh
     that asserts every CLI verb + template has a documentation
     surface (D-138).

The master plan's Phase 29 detail block lists `Deps. 21, 26`. Phase
26 (the `inspector` template) was deferred post-V1 in D-136, so the
effective deps are 21 and 25. The plan is updated in this PR.
-->

## Summary

Phase 29 turns the framework into one a real developer — human or AI
agent — can pick up cold and ship with. It ships eight Agent Skills
(`SKILL.md` format, agentskills.io conventions) covering the canonical
MCP-App-on-Dockyard workflows, plus a static technical-documentation
site (VitePress, GitHub Pages) built from the in-repo docs with
getting-started walkthroughs of the two shipped V1 templates
(`analytics-widgets`, `approval-flows`), an auto-derived CLI reference
from the cobra command tree, and the in-repo screenshots. From this
phase on, the AGENTS.md §19 hygiene rule is mechanically enforced: a
new `internal/skillcheck` validator runs against every `SKILL.md` in
CI and via drift-audit, and `scripts/drift-audit.sh` fails when a CLI
verb or shipped template has no docs/skills surface.

## RFC anchor

- RFC §1 — the four binding properties (P1 contract-first, P2 obs
  as a protocol, P3 forward-compatibility by isolation, P4 server-
  side only) — the skills + docs teach the framework's identity, so
  they must reflect P1–P4 verbatim.
- RFC §2 — authoritative sources priority chain — the docs site
  surfaces the chain (RFC > plans > AGENTS.md > briefs > comments).
- RFC §10 — Templates — the V1 template set is the worked example
  the docs walk through; the skills reference the shipped
  templates as canonical "this is how you do it" patterns.
- RFC §9 — CLI / DX — the CLI reference is auto-derived from the
  command tree; the eight skills mirror the verb-shaped workflows.

## Briefs informing this phase

- brief 04 — the mcp-use DX teardown (Dockyard's DX bar; the
  "templates are workflows, not transports" framing; the gaps
  Dockyard's CLI closes that mcp-use leaves open — these are the
  hooks the skills sell).

## Brief findings incorporated

- **brief 04 §2.2 — "no `test`, no `validate`, no `install`, no
  typegen command".** Each of these is a Dockyard CLI verb and gets
  a skill of its own (`validate`, `test-with-the-inspector` for
  test, `package` for build+install, `define-contracts` for the
  generate / typegen surface). Naming the closed gap in each
  skill's body is what makes the docs sell the framework — not just
  document it.
- **brief 04 §2.8.1 — "one-command start to running app".** The
  `scaffold-a-server` skill leads with one command (`dockyard new
  --template analytics-widgets`); the getting-started page on the
  docs site is one runnable session end-to-end. The bar is "minutes
  to first render" — the skills + docs are measured against it.
- **brief 04 §2.8.5 — "HMR through to widgets".** The
  `run-the-dev-loop` skill leads with the embedded `dockyard dev`
  watcher — one process, no external dev tool. Mirrors the
  comparative DX win directly.
- **brief 04 §2.6 — "mcp-use has *types* but not *contracts*".**
  The `define-contracts` skill foregrounds Design A: the Go struct
  is the source of truth; `dockyard generate` produces JSON Schema +
  TypeScript; `dockyard validate` fails on drift. This is the P1
  story, taught as a workflow.
- **brief 04 §2.9.7 — "no manifest / control plane".** The
  `scaffold-a-server` and `attach-a-ui-resource` skills both root
  in `dockyard.app.yaml`. The docs site's getting-started page
  surfaces the manifest as the project's control plane.

## Findings I'm departing from (if any)

None. Brief 04's findings inform but don't constrain the skills'
shape — the skills are the framework's documentation surface, not a
re-statement of the brief.

## Goals

- A new developer onboarding to Dockyard via an AI coding agent is
  productive on the first try: the agent picks the right skill,
  follows it, the commands work, the result matches the prose.
- A new developer onboarding to Dockyard *without* an agent reads
  the published docs site, follows the getting-started page end-
  to-end, and ends with a running, inspectable MCP server.
- The framework's CLI surface + the two V1 templates each have a
  documentation surface (a skill, a docs page, or both). Drift in
  either is mechanically caught.
- The AGENTS.md §19 hygiene rule becomes effective and enforced —
  the PR that changes a CLI verb in the future must update the
  affected skill(s) and docs page(s) in the same PR.

## Non-goals

- A reference manual generated from godoc — that lives in Phase
  28's hygiene pass. Phase 29 publishes the docs site and the
  per-template walkthroughs; an exhaustive godoc surface is Phase
  28's deliverable.
- Skills for not-yet-shipped surfaces (the deferred `inspector`
  template, post-V1 host profiles beyond Claude, `dockyard publish`,
  enterprise auth). Skills must reflect actually-shipped behaviour
  (§19 hygiene rule); a skill for a future verb would lie.
- A docs CMS / versioned docs / search backend. VitePress's built-in
  client-side search is enough for V1; a versioned doc tree lands
  with Phase 30's release cut if needed.

## Acceptance criteria

- [ ] Eight Agent Skills exist under `skills/<slug>/SKILL.md` and
  every one parses cleanly against the SKILL.md spec via the
  `internal/skillcheck` validator: `scaffold-a-server`,
  `add-a-tool`, `attach-a-ui-resource`, `define-contracts`,
  `run-the-dev-loop`, `validate`, `package`,
  `test-with-the-inspector`.
- [ ] Every skill's named CLI verbs + commands have been live-tested
  against the real `bin/dockyard` and a scaffolded
  `analytics-widgets` / `approval-flows` project.
- [ ] The VitePress docs site builds cleanly (`make docs`) and
  produces a static `docs/site/.vitepress/dist/` tree. The site
  renders: the home page, a getting-started walkthrough per V1
  template, an auto-derived CLI reference, the agent-skills index,
  and the RFC / plans / decisions / glossary / design conventions
  via in-repo transclusion.
- [ ] `.github/workflows/docs.yml` builds the site on every PR
  (build-only validation) and deploys to GitHub Pages on a push
  to `main`.
- [ ] `internal/skillcheck` has a CLI (`go run
  ./internal/skillcheck/cmd/skillcheck skills/`) that exits non-
  zero on a malformed `SKILL.md`. Golden-test fixtures cover a
  valid and an invalid skill.
- [ ] `scripts/drift-audit.sh` carries the new §19 hook: every CLI
  verb has a referencing skill or docs page, and every shipped
  template has a docs walkthrough. A synthetic missing-skill case
  trips it (covered by phase-29 smoke).
- [ ] `scripts/smoke/phase-29.sh` reports `OK ≥
  count(acceptance criteria)` and `FAIL = 0`.
- [ ] `docs/screenshots/phase-29/` carries Playwright captures of
  the docs-site home, getting-started, CLI reference, and agent-
  skills index pages.

## Files added or changed

```text
.github/workflows/
  docs.yml                                    # NEW — build on PR, deploy on main
docs/
  plans/
    phase-29-skills-docs.md                   # NEW — this plan
    README.md                                 # touched — Phase 29 detail Deps fix
  decisions.md                                # touched — D-137..D-142
  glossary.md                                 # touched — agent skill, SKILL.md, skillcheck
  screenshots/phase-29/                       # NEW — docs-site Playwright captures
  site/                                       # NEW — VitePress docs site
    .vitepress/
      config.ts                               # site config + nav + theme
      theme/                                  # minimal theme overrides if needed
    index.md                                  # home page
    getting-started/
      index.md                                # one-command tour
      analytics-widgets.md                    # walkthrough
      approval-flows.md                       # walkthrough
    guides/
      contracts.md
      ui-resources.md
      dev-loop.md
      validate.md
      packaging.md
      inspector.md
    cli/
      index.md                                # auto-derived CLI reference
    reference/
      rfc.md                                  # transclude RFC-001
      master-plan.md                          # transclude docs/plans/README.md
      decisions.md                            # transclude docs/decisions.md
      glossary.md                             # transclude docs/glossary.md
      design-conventions.md                   # transclude docs/design/CONVENTIONS.md
    agent-skills/
      index.md                                # the eight V1 skills
    package.json                              # vitepress dep + npm scripts
    tsconfig.json
internal/skillcheck/                          # NEW — SKILL.md validator
  doc.go
  parse.go                                    # YAML frontmatter parser + validator
  parse_test.go                               # golden-test fixtures
  testdata/
    valid/SKILL.md
    invalid-name/SKILL.md
    invalid-description/SKILL.md
  cmd/skillcheck/main.go                      # CLI: walks skills/ and validates
scripts/drift-audit.sh                        # touched — §19 hook
scripts/smoke/phase-29.sh                     # NEW — phase smoke
skills/                                       # NEW top-level directory (AGENTS.md §3 updated)
  scaffold-a-server/SKILL.md
  add-a-tool/SKILL.md
  attach-a-ui-resource/SKILL.md
  define-contracts/SKILL.md
  run-the-dev-loop/SKILL.md
  validate/SKILL.md
  package/SKILL.md
  test-with-the-inspector/SKILL.md
Makefile                                      # touched — `docs` target + `docs-install`
AGENTS.md / CLAUDE.md                         # touched — §3 layout: skills/, docs/site/
```

## Public API surface

- `internal/skillcheck.Validate(dir string) (Report, error)` — walks
  a directory of skills, parses each `SKILL.md`, returns a typed
  Report. Used by the CLI and the smoke script.
- `internal/skillcheck/cmd/skillcheck` — the small CLI used by
  drift-audit + smoke. Exits non-zero on a malformed skill.

No new runtime API is exposed; the docs site is a build artifact
and the skills are markdown files. The new build target `make docs`
is documented in the Makefile help.

## Test plan

- **Unit:** `internal/skillcheck/parse_test.go` — golden-style
  tests over `testdata/valid` (passes) and the `testdata/invalid-*`
  fixtures (each fails with the expected diagnostic). One test per
  spec constraint: `name` shape (lowercase, hyphens, no leading/
  trailing/consecutive hyphens, ≤64 chars), `name` matches parent
  directory, `description` non-empty + ≤1024 chars, frontmatter
  parses, body non-empty.
- **Integration:** `scripts/smoke/phase-29.sh` — runs the validator
  against the real `skills/` tree (must report all valid), runs
  it against a synthetic malformed fixture (must report invalid),
  and runs `make docs` if VitePress is installed (skips gracefully
  otherwise — same convention as `make web`).
- **Concurrency / golden:** the validator is pure (read-only stdlib
  YAML + stdlib regexp); no concurrency surface. Golden coverage
  via testdata fixtures.

## Smoke script additions

- `skills/` has eight `SKILL.md` files — one per named skill in the
  acceptance list. Each is well-formed under the skillcheck
  validator.
- The skillcheck CLI exits non-zero against an injected malformed
  fixture (`internal/skillcheck/testdata/invalid-name/SKILL.md`).
- `docs/site/.vitepress/config.ts` exists.
- `.github/workflows/docs.yml` exists.
- `make docs` succeeds when VitePress is available (SKIP when npm /
  the docs/site/ tree is absent).
- The §19 drift hook fires when a synthetic missing-skill case is
  injected (validator + drift-audit catch the gap).

## Coverage target

- `internal/skillcheck`: 80% (new package; tooling band).
- No coverage target for `docs/site/` (markdown + config; no Go).
- No coverage target for `skills/` (markdown; the skillcheck
  validator is the coverage equivalent).

## Dependencies

- Phase 21 (CLI test gate — `dockyard test` is a verb the skills
  reference).
- Phase 25 (the second shipped V1 template, `approval-flows` — one
  of the two getting-started walkthroughs).

The master-plan detail block lists `Deps. 21, 26`; Phase 26 was
deferred post-V1 in D-136. The plan is corrected in this PR (one-
line edit to docs/plans/README.md's Phase 29 entry).

## Risks / open questions

- **VitePress install latency in CI.** Mitigated by caching
  `docs/site/node_modules` in the workflow (the same pattern the
  existing `web/` projects use in `ci.yml`). The build step uses
  `npm ci --no-audit --no-fund`.
- **GitHub Pages deployment requires repo permissions.** The
  workflow uses `permissions: pages: write, id-token: write` and
  the official `actions/deploy-pages` action — the standard,
  documented path. If Pages is not yet enabled on the repo, the
  deploy job will require a one-time UI toggle (documented in the
  PR body).
- **Skill drift over time.** The §19 drift-audit hook + the
  in-PR pre-merge checklist catch the most likely drift modes (a
  new CLI verb without a skill, a removed template without a docs
  walkthrough). Subtler drift — a skill whose prose still works
  but is no longer the "best" path — needs a periodic walkthrough
  during release engineering (Phase 30).

## Glossary additions

- **Agent Skill** — a directory under `skills/` containing a
  `SKILL.md` file (per the agentskills.io specification) that
  teaches an AI coding agent one coherent Dockyard workflow.
- **`SKILL.md`** — the Markdown-with-YAML-frontmatter file at the
  root of an Agent Skill, per the agentskills.io specification.
- **skillcheck** — the in-repo SKILL.md validator
  (`internal/skillcheck`), used by drift-audit and the phase-29
  smoke script to mechanically catch malformed skills.
- **docs site** — the published technical documentation at
  `docs/site/`, built by VitePress and deployed by CI to GitHub
  Pages.
- **§19 hygiene rule** — the AGENTS.md §19 rule that a PR
  changing user-facing surface (a CLI verb, a manifest field, a
  template, the generated-project shape, a public runtime API)
  also updates the affected skill(s) and docs page(s) in the same
  PR. Mechanically enforced by the new §19 drift-audit hook.

## Pre-merge checklist

- [ ] `make drift-audit` passes (incl. the new §19 hook)
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke
  check in this PR (the new `make docs` target is documented in
  the Makefile help; the skillcheck CLI has smoke coverage)
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
  (skillcheck is pure-fn and read-only; N/A)
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test
  (AGENTS.md §17) — the §19 drift hook + the phase-29 smoke fire
  on synthetic missing-skill fixtures, the seam's "real driver"
  test
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in
  `docs/decisions.md` (D-137..D-142 reserved)
- [ ] If UI was touched (Phase 10a+): composes the shared `web/ui/`
  inventory — N/A here, the docs site is its own VitePress theme
- [ ] The §19 hygiene rule is in force: this PR adds a Makefile
  target (`make docs`) and a CLI tool (`skillcheck`), both
  documented in the Makefile help and the skills/docs site

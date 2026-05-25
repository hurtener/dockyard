# Phase 30 — V1 release engineering + cut

<!--
The final phase of Dockyard's V1 roadmap. Ships the release plumbing that
lets the v1.0.0 tag actually publish:
  1. A top-level CHANGELOG.md telling the V1 story (Keep a Changelog).
  2. A `.github/workflows/release.yml` that, on a `v*` tag push, runs the
     existing `make preflight` gate, drives the existing `internal/buildpkg`
     cross-compile matrix, computes a SHA-256 checksum per artifact, and
     creates a GitHub Release with the artifacts attached and the
     CHANGELOG section as the release body. Idempotent and safe; also
     callable via `workflow_dispatch` for dry-runs.
  3. A `docs/V2-BACKLOG.md` consolidating every recorded post-V1 deferral
     (D-088, D-101, D-108, D-136, D-139, and the analytics-widgets/
     Claude signed-origin follow-up) into one canonical, navigable list.
  4. A `docs/RELEASING.md` documenting the semver policy, the exact
     tag-push commands, the post-release verification checklist, and
     the rollback procedure.
  5. README + skills + docs-site polish for the post-v1.0.0 reality:
     a `go install github.com/hurtener/dockyard/cmd/dockyard@v1.0.0`
     path becomes the recommended first step; the pre-publish
     `--dockyard-path` workflow stays as the "build from source" path.
  6. The final consistency sweep — phase index status, decision-log
     ascending check, mirror, CLI reference, glossary, cross-references.

The release tag itself is NOT pushed by this PR. Merging the PR lands the
plumbing; the user runs the `git tag -a v1.0.0 …` + `git push` sequence
documented in docs/RELEASING.md to actually publish.
-->

## Summary

Phase 30 cuts V1. It ships the release pipeline (a `release.yml` workflow
that turns a `v*` tag push into a GitHub Release with cross-compile
artifacts and SHA-256 checksums), the V1 story (`CHANGELOG.md`, framed by
the four binding properties P1–P4), the post-V1 backlog reconciliation
(`docs/V2-BACKLOG.md`), and the release procedure (`docs/RELEASING.md`).
The post-v1.0.0 docs surface — README, the `scaffold-a-server` skill, the
docs-site getting-started page — gains a `go install …@v1.0.0` path
alongside the pre-publish `--dockyard-path` build-from-source path.
Merging the PR lands the plumbing; pushing the `v1.0.0` tag is the
user's release moment.

## RFC anchor

- RFC §1 — the four binding properties (P1 contract-first, P2 obs as a
  protocol, P3 forward-compatibility by isolation, P4 server-side only).
  The CHANGELOG's V1.0.0 entry is framed by these — they are the
  developer-meets-V1 story, not a phase-by-phase diary.
- RFC §14 — Packaging & deployment modes. The release workflow drives
  the cross-compile matrix this section specifies (darwin/linux/windows
  × amd64/arm64, CGo-free, one static binary per target, SHA-256
  checksum sidecars). The existing `internal/buildpkg` package is the
  in-repo realisation; Phase 30 wraps it in a CI release pipeline.

## Briefs informing this phase

- brief 04 — the mcp-use DX teardown. The CHANGELOG + the post-publish
  docs polish reinforce the "one-command start" and "minutes to first
  render" bar brief 04 sets; the `go install …@v1.0.0` path is the
  shortest possible on-ramp the framework can offer post-publish.

## Brief findings incorporated

- **brief 04 §2.8.1 — "one-command start to running app".** The
  post-publish recommended path is one command: `go install
  github.com/hurtener/dockyard/cmd/dockyard@v1.0.0`. The CHANGELOG's
  "Developer experience" highlights section foregrounds this as the
  V1 on-ramp; the README's "Try it" section and the docs-site
  getting-started page both lead with it.
- **brief 04 §2.2 — "no `test`, no `validate`, no `install`, no
  typegen command".** The CHANGELOG's "The CLI" section names every
  shipped Dockyard verb (`new` / `generate` / `validate` / `dev` /
  `build` / `run` / `install` / `test` / `inspect`) as the closed-gap
  story; the V1 cut is the moment that story is dial-tone available
  to every developer who runs `go install …@v1.0.0`.

## Findings I'm departing from (if any)

None. Phase 30 is release engineering — brief 04 informs the framing
but does not constrain the workflow shape; the workflow design
decisions land as D-NNN entries in this PR rather than as departures.

## Goals

- A `git push origin v1.0.0` from a clean `main` produces a GitHub
  Release with: six cross-compile artifacts (the RFC §14 matrix), a
  SHA-256 checksum sidecar per artifact, a `checksums.txt` aggregate
  for `sha256sum -c`, and a release body extracted from
  `CHANGELOG.md`'s v1.0.0 section — without any manual post-tag
  intervention.
- The release pipeline is **idempotent** — running it twice against
  the same tag updates the existing release rather than creating a
  duplicate, and a failed `make preflight` gate fails the release
  cleanly (no partial GitHub Release left behind).
- The release pipeline is **verifiable without pushing a real tag** —
  the workflow's `workflow_dispatch` trigger lets a maintainer run a
  dry-run end-to-end without creating a release; `actionlint`
  validates the workflow's YAML before merge.
- Every recorded post-V1 deferral has a single, navigable home in
  `docs/V2-BACKLOG.md` with originating decision number, deferral
  rationale, and the "definition of done" for a future phase to claim
  it.
- The post-publish developer experience is real: a fresh developer
  running `go install github.com/hurtener/dockyard/cmd/dockyard@v1.0.0`
  on a tagged version gets a working `dockyard` binary. The README,
  skills, and docs site all reflect this.
- The §19 hygiene rule continues to be honoured: every user-facing
  change in this PR (the new `make` interactions, the new docs
  pages, the post-v1.0.0 README path) updates the affected skill(s)
  and docs page(s) in the same PR.

## Non-goals

- **Pushing the `v1.0.0` tag.** This PR ships the plumbing; the user
  pushes the tag (per `docs/RELEASING.md`). Merging the PR is the
  permission to release, not the act of releasing.
- **A signed-release / SLSA-provenance pipeline.** Out of scope for
  V1; the V1 release ships SHA-256 checksums (the same convention
  `internal/buildpkg` already emits) and the source URL pin via the
  tag — sufficient for `go install` to verify against `go.sum` /
  `sumdb`. Cosign signing, SLSA attestation, and a signed-checksums
  workflow are recorded in `docs/V2-BACKLOG.md` as a deferred
  hardening item.
- **A versioned docs site.** VitePress's built-in client-side search
  paired with the v1.0.0 release callout on the docs home page is
  V1's versioning story. A multi-version doc tree (1.0 / 1.1 / 2.0
  side by side) lands when there are multiple stable releases to
  switch between — recorded in V2-BACKLOG.
- **An autogenerated CHANGELOG.** The v1.0.0 entry tells the V1
  story in human-authored prose framed by P1–P4 — a Conventional
  Commits-generated changelog would be a phase-by-phase diary. From
  v1.1.0 onward the format is open: a maintainer may augment with
  generated PR/commit lists, but the canonical first entry is
  hand-authored.
- **Removing the `--dockyard-path` workflow.** D-139 stays valid as
  the "build from source" path even after `go install …@v1.0.0`
  becomes the recommended path. The docs reframe it as the
  alternative; they do not remove it.

## Acceptance criteria

- [ ] `CHANGELOG.md` exists at the repo root with a v1.0.0 entry
  framed by the four binding properties (P1–P4) and covering: the
  runtime + extensions, the CLI's eight verbs, the inspector, the
  two shipped templates, the agent-skills + docs site, the quality
  bar (conformance + coverage + fuzz), the deferred V2 surface, and
  acknowledgements.
- [ ] `.github/workflows/release.yml` exists; triggers on `v*` tag
  push and `workflow_dispatch`; runs `make preflight` as a release
  gate; drives the `internal/buildpkg.DefaultMatrix()` cross-compile
  matrix (darwin/linux/windows × amd64/arm64) via the in-repo helper
  in `internal/releasebuild`; produces one CGo-free static binary
  per target with a per-artifact SHA-256 sidecar and a
  `checksums.txt` aggregate; creates / updates a GitHub Release
  (idempotent against re-runs) with the artifacts attached and the
  CHANGELOG section as the body. The workflow passes `actionlint`.
- [ ] `internal/releasebuild` is a small Go package (`+ cmd/`) that
  drives `internal/buildpkg.Build` to produce the matrix into a
  caller-supplied directory and writes the `checksums.txt`
  aggregate. Unit-tested (`Build`-driven golden artifact +
  checksum-file shape).
- [ ] `internal/changelogx` is a small Go package (`+ cmd/`) that
  parses `CHANGELOG.md` and extracts the section for a named
  version. Unit-tested against the in-repo `CHANGELOG.md` (extract
  v1.0.0 + the unreleased section).
- [ ] `docs/V2-BACKLOG.md` exists and lists every recorded
  post-V1 deferral with its originating decision number, the
  deferral rationale, and a "definition of done" for a future
  phase to claim it. Covers at least: D-088, D-101, D-108, D-136,
  D-139, the analytics-widgets / Claude signed-origin follow-up
  (Phase 29 worked around it via the synthetic-URL fix in
  `internal/testgate`), the deferred SLSA / signed-release
  hardening, and the deferred versioned-docs surface.
- [ ] `docs/RELEASING.md` exists and documents: the semver policy
  going forward (when patch / minor / major); the exact tag-push
  commands the user runs after merge; the post-release verification
  checklist (download an artifact, verify its checksum, run
  `--help`); the rollback procedure if a release goes out broken.
- [ ] `README.md` carries a `go install
  github.com/hurtener/dockyard/cmd/dockyard@v1.0.0` path as the
  recommended post-v1.0.0 install; the `--dockyard-path` path stays
  as the "build from source" alternative. The "What's shipped"
  status table marks Phase 30 ✓.
- [ ] `skills/scaffold-a-server/SKILL.md` carries the `go install`
  recommended-path block.
- [ ] `docs/site/getting-started/index.md` carries the `go install`
  recommended-path block alongside the existing build-from-source
  block; the docs home page has a "v1.0.0" release callout.
- [ ] `docs/plans/README.md` marks Phase 30 as `Shipped`.
- [ ] `docs/decisions.md` has the new D-154..D-160 entries.
- [ ] `docs/glossary.md` carries the new vocabulary
  (CHANGELOG, release pipeline, V2 backlog, etc.).
- [ ] The §19 hook + the §14 mirror invariant + the drift-audit
  gate all pass on the V1 codebase.
- [ ] `scripts/smoke/phase-30.sh` reports `OK ≥
  count(acceptance criteria)` and `FAIL = 0`.
- [ ] The release dry-run is documented under
  `docs/release/v1.0.0/` — a transcript of the cross-compile
  matrix output, a transcript of one of the artifacts booting, a
  transcript of `make preflight` from a clean checkout.

## Files added or changed

```text
.github/workflows/
  release.yml                                 # NEW — tag-triggered release pipeline
AGENTS.md / CLAUDE.md                         # touched — §3 layout (CHANGELOG.md + the new internals)
CHANGELOG.md                                  # NEW — v1.0.0 release notes (Keep a Changelog)
Makefile                                      # touched — `release-matrix` helper + Makefile help
README.md                                     # touched — `go install` recommended path, status table
docs/
  RELEASING.md                                # NEW — semver, tag-push, post-release checks, rollback
  V2-BACKLOG.md                               # NEW — consolidated post-V1 deferrals
  decisions.md                                # touched — D-154..D-160
  glossary.md                                 # touched — CHANGELOG, release pipeline, V2 backlog, semver
  plans/
    phase-30-v1-cut.md                        # NEW — this plan
    README.md                                 # touched — Phase 30 marked Shipped
  release/
    v1.0.0/                                   # NEW — dry-run transcripts
      cross-compile-matrix.txt
      preflight.txt
      binary-help.txt
      README.md                               # one-paragraph index
  site/
    .vitepress/
      config.ts                               # touched — v1.0.0 release callout in the home page section
    getting-started/
      index.md                                # touched — `go install` recommended path
    index.md                                  # touched — "Released: v1.0.0" home callout
internal/
  changelogx/                                 # NEW — CHANGELOG section extractor
    doc.go
    parse.go
    parse_test.go
    testdata/
      CHANGELOG.md                            # fixture: a CHANGELOG with two versions
    cmd/changelogx/main.go                    # the small CLI the release workflow consumes
  releasebuild/                               # NEW — release pipeline driver
    doc.go
    release.go                                # wraps internal/buildpkg.Build for tag-triggered runs
    release_test.go
    checksums.go                              # writes the checksums.txt aggregate sidecar
    checksums_test.go
    cmd/releasebuild/main.go                  # the small CLI the release workflow consumes
scripts/
  smoke/phase-30.sh                           # NEW — phase smoke
skills/
  scaffold-a-server/SKILL.md                  # touched — `go install …@v1.0.0` recommended path block
templates/                                    # untouched — D-139 pre-publish workflow stays
```

## Public API surface

- `internal/changelogx.ExtractSection(content []byte, version string)
  ([]byte, error)` — extract a named version's section from a
  CHANGELOG.md in Keep a Changelog format. Returns the section body
  without its `## [version]` header. `ErrSectionNotFound` if absent.
- `internal/changelogx/cmd/changelogx` — the CLI: reads
  `CHANGELOG.md`, prints the extracted section to stdout, exits
  non-zero on failure. Consumed by `.github/workflows/release.yml`.
- `internal/releasebuild.Release(ctx, opts) (Result, error)` — the
  reusable driver that calls `internal/buildpkg.Build` with the
  cross-compile matrix, copies artifacts into the release output
  directory under their conventional release names
  (`dockyard-v1.0.0-linux-amd64`, …), and writes the aggregate
  `checksums.txt`.
- `internal/releasebuild/cmd/releasebuild` — the CLI consumed by the
  release workflow. Flags: `-version <semver>`, `-output <dir>`.

No new runtime API. The two new packages are CLI/tooling internals;
they sit under `internal/` per the AGENTS.md §3 layout rule.

## Test plan

- **Unit:** `internal/changelogx/parse_test.go` — golden-style tests
  over the in-repo `testdata/CHANGELOG.md` fixture (extract the
  unreleased section, extract a known version, error on a missing
  version, error on a malformed header). `internal/releasebuild/
  release_test.go` — golden-style: `Release` invoked against a
  minimal scaffolded fixture project produces the expected file
  set under the output directory; the `checksums.txt` line shape
  matches `sha256sum -c`'s parser. `internal/releasebuild/
  checksums_test.go` — the aggregate-file builder is pure-fn over
  per-artifact paths + digests.
- **Integration:** `scripts/smoke/phase-30.sh` runs the changelogx
  CLI against the in-repo `CHANGELOG.md` and asserts the v1.0.0
  section is extracted; runs the releasebuild CLI in dry-run mode
  (against a minimal fixture); asserts the workflow file exists +
  references the matrix targets + carries the `workflow_dispatch`
  trigger. The release workflow itself is verified end-to-end via
  the dry-run captured under `docs/release/v1.0.0/`.
- **Concurrency / golden:** the two new packages are read-only,
  pure-functional, and per-call — no shared state, no concurrency
  surface. Golden coverage via testdata fixtures.

## Smoke script additions

- `CHANGELOG.md` exists at the repo root and carries a `## [1.0.0]`
  (or `## [v1.0.0]`) heading.
- `.github/workflows/release.yml` exists, references the `v*` tag
  push trigger, declares the `workflow_dispatch` trigger for
  dry-runs, names the cross-compile matrix targets, and references
  `make preflight` as the gate.
- `docs/V2-BACKLOG.md` exists and references at least: D-088,
  D-101, D-108, D-136, D-139 (and the analytics-widgets / Claude
  signed-origin follow-up).
- `docs/RELEASING.md` exists, documents the `git tag -a v1.0.0`
  command, the semver policy, the verification checklist, and the
  rollback procedure.
- The `internal/changelogx` CLI extracts the v1.0.0 section from
  the in-repo `CHANGELOG.md` and exits zero; errors on a missing
  version.
- The `internal/releasebuild` CLI exists and runs `-help`
  successfully.
- `README.md`, `skills/scaffold-a-server/SKILL.md`, and
  `docs/site/getting-started/index.md` reference the post-publish
  `go install …@v1.0.0` path.
- `docs/plans/README.md`'s Phase 30 row is marked Shipped.

## Coverage target

- `internal/changelogx`: 80% (new package; tooling band).
- `internal/releasebuild`: 80% (new package; tooling band).
- No coverage target for the YAML workflow or markdown deliverables.

## Dependencies

- Phase 27 (Security pass + spec-compliance conformance) — the
  CHANGELOG's "Quality bar" section names the conformance suite.
- Phase 28 (Examples, godoc, docs hygiene) — the CHANGELOG's
  "Developer experience" section names the three worked examples
  and the godoc surface.
- Phase 29 (Agent skills + published docs site) — the post-publish
  docs polish updates the skill and the docs-site getting-started
  page in the same PR (§19 hygiene rule).

## Risks / open questions

- **GitHub Release deduplication on workflow re-run.** The
  `softprops/action-gh-release` action handles the "release already
  exists" case via its `update` flag; we set it. A re-run uploads
  any artifacts not already present and updates the body; it never
  creates a duplicate release. Tested via `workflow_dispatch` in
  the dry-run.
- **`go install` requires the tag to be pushed before the install
  succeeds.** The PR cannot end-to-end prove `go install
  …@v1.0.0` works because the tag is not yet pushed. The dry-run
  verifies: (a) the cross-compile matrix produces the same binary
  shape `go install` would produce; (b) the binary's `--help`
  output is sane; (c) the module path in `go.mod` matches the
  `go install` target the docs now reference. The actual `go
  install` is verified after the tag push by the rollback
  procedure in `docs/RELEASING.md`.
- **A maintainer's local `act` setup is not always available.** The
  dry-run path uses `workflow_dispatch` against the test branch
  before tagging; `actionlint` validates the YAML syntactically in
  CI even when `act` is not run. A maintainer with `act` available
  may additionally run the workflow locally; the docs note this as
  optional.
- **A release that goes out broken needs a clean rollback.**
  `docs/RELEASING.md` documents the rollback procedure: delete the
  GitHub Release (preserves the tag) or delete the tag (forces a
  full redo). The choice depends on the failure mode — a bad
  binary needs the artifacts re-uploaded, a bad changelog body
  needs only the body edited.

## Glossary additions

- **CHANGELOG** — the top-level `CHANGELOG.md` in Keep a Changelog
  format. Authoritative for "what shipped in version N"; the v1.0.0
  entry frames the V1 story around the four binding properties.
- **Release pipeline** — the `.github/workflows/release.yml`
  workflow that turns a `v*` tag push into a GitHub Release with
  cross-compile artifacts and SHA-256 checksums. Idempotent against
  re-runs.
- **V2 backlog** — `docs/V2-BACKLOG.md`, the consolidated post-V1
  deferral list. Every recorded deferral (D-NNN-numbered) has a
  rationale and a "definition of done" entry there; a future phase
  that wants to claim one cites the V2-BACKLOG line.
- **Semver policy** — Dockyard's post-v1.0.0 versioning rule
  documented in `docs/RELEASING.md`. Major = breaking API or
  manifest schema change; minor = additive feature; patch =
  bug/docs/security fix. The release pipeline keys on the tag
  shape (`v<major>.<minor>.<patch>`).
- **`releasebuild`** — `internal/releasebuild`, the in-repo Go
  driver the release workflow consumes. Wraps
  `internal/buildpkg.Build` with the matrix + the aggregate
  `checksums.txt` writer.
- **`changelogx`** — `internal/changelogx`, the in-repo Go CLI the
  release workflow consumes to extract one version's section from
  `CHANGELOG.md` as the GitHub Release body.

## Pre-merge checklist

- [ ] `make drift-audit` passes (incl. the §19 hook)
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke
  check in this PR (`changelogx` + `releasebuild` CLIs both have
  smoke coverage; the release workflow is dry-run-verified)
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
  (the two new packages are read-only, pure-functional, per-call —
  no shared state; N/A)
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test
  (AGENTS.md §17) — the release workflow consumes
  `internal/buildpkg`'s cross-compile matrix; `releasebuild` is
  the integration seam, covered by `release_test.go` end-to-end
  against a fixture project
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in
  `docs/decisions.md` (D-154..D-160 reserved)
- [ ] If UI was touched (Phase 10a+): N/A — no `web/ui` change
- [ ] §19 hygiene rule in force: the post-publish `go install` path
  lands in the README, the `scaffold-a-server` skill, and the
  docs-site getting-started page in this PR

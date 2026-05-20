# Phase NN — <slug>

<!--
Phase plan template. Copy to phase-NN-slug.md and fill in every section.
Do not delete sections; if a section is N/A, write "N/A — <reason>".
The drift-audit script checks required cross-references resolve and that a matching
scripts/smoke/phase-NN.sh exists. See AGENTS.md §16 for the authoring workflow.
-->

## Summary

<!-- 2-3 sentences: what this phase delivers. -->

## RFC anchor

<!-- Required. The RFC sections this phase implements. Format: RFC §6.X.
     drift-audit verifies every RFC §N.M reference resolves to a real heading. -->

- RFC §

## Briefs informing this phase

<!-- Required. The research briefs whose findings this phase depends on, per
     docs/research/INDEX.md. Format: `brief NN`. A phase citing no brief is a
     drift signal. -->

- brief

## Brief findings incorporated

<!-- Required. Quote 2-5 specific findings from the briefs above that this phase
     adopts, citing the brief number + section. Forcing function: hard-won
     research makes it INTO the implementation, not just the briefs. -->

-

## Findings I'm departing from (if any)

<!-- Required (may be "None."). A deliberate departure from a brief finding or an
     RFC decision is listed here with explicit justification AND filed in
     docs/decisions.md. Silent departure is forbidden (AGENTS.md §15). -->

-

## Goals

<!-- Outcomes this phase must achieve. Not implementation detail. -->

-

## Non-goals

<!-- Explicit out-of-scope items — the "later phase" list. -->

-

## Acceptance criteria

<!-- Required. Bulleted, testable, binding. -->

- [ ]

## Files added or changed

<!-- Tree-style list. A phase adding a new top-level directory updates AGENTS.md §3
     in the same PR. -->

-

## Public API surface

<!-- What other phases depend on. Go-flavored signatures. No internal types. -->

-

## Test plan

<!-- Required. Categorize: unit / integration / conformance / concurrency / golden.
     Integration is binding when Dependencies names another shipped phase or this
     phase opens a cross-subsystem seam (AGENTS.md §17). -->

- **Unit:**
- **Integration:**
- **Concurrency / golden:**

## Smoke script additions

<!-- Required. The assertions scripts/smoke/phase-NN.sh adds. drift-audit verifies
     that file exists for every phase plan. -->

-

## Coverage target

<!-- Required, per touched package. Defaults: 80% new packages; 85% Store drivers
     and conformance-tested subsystems; 70% CLI/tooling. -->

-

## Dependencies

<!-- Required. Phase numbers that must land before this one. -->

-

## Risks / open questions

<!-- Real risks. Reference RFC §18 Q-N where applicable. -->

-

## Glossary additions

<!-- New vocabulary introduced by this phase — list it here AND add it to
     docs/glossary.md in the same PR. -->

-

## Pre-merge checklist

<!-- Mirrors AGENTS.md §14. -->

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

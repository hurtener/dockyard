<!-- See AGENTS.md §12 (Commit and PR conventions) and §14 (Pre-merge checklist). -->

## What this PR does

<!-- 2-3 sentences. -->

## RFC anchor

<!-- The RFC section(s) this implements, e.g. RFC §7.3. -->

## Phase / scope

<!-- The phase number, or "chore"/"docs" if not a phase. -->

## Plan deviations

<!-- Any deviation from the phase plan or RFC, with justification. The plan file
     must be updated in this same PR. "None." is a valid answer. -->

## Pre-merge checklist (AGENTS.md §14)

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] Cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check here
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (§17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`

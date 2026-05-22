// Package coveragecheck is the mechanical coverage gate (Phase 21.5).
//
// Until Phase 21.5, the AGENTS.md §11 coverage bands — 80% new packages, 85%
// the Store drivers and conformance-tested subsystems, 70% CLI / tooling —
// were aspirational: `make test` ran `go test -race ./...` with no
// `-coverprofile` and no threshold, so a coverage regression slipped through
// unless a reviewer caught it by eye. coveragecheck makes the bands
// mechanical: it parses a Go coverage profile, computes per-package statement
// coverage, compares each package against its required threshold, and exits
// non-zero on any shortfall.
//
// The threshold map lives in coverage.json next to this package: each package
// maps to a required percentage keyed to its AGENTS.md band, with explicit,
// documented overrides where a package genuinely cannot reach its band
// hermetically (the subprocess-orchestrating CLI packages, the conformance
// harness packages whose statements are exercised only when a driver runs
// them). An override carries a reason string so the justification travels with
// the config — never a silent lowering of a band.
//
// The checker is wired into `make coverage` and the CI `go` job; a regression
// fails the build (CLAUDE.md §11, §4.2).
package coveragecheck

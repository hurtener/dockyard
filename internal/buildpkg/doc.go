// Package buildpkg implements the `dockyard build` pipeline — the step that
// turns a Dockyard project into the shippable artifact (RFC §14).
//
// Build sequences five stages, in order:
//
//  1. regenerate the project's contract artifacts (internal/generate);
//  2. run the `dockyard validate` quality gate (internal/validate) — a
//     validation BLOCKER fails the build, enforcing P1 at build time;
//  3. build the project's web/ Vite UI, when one exists, so the dist/ embed
//     target is on disk BEFORE go build reads the //go:embed directive
//     (the RFC §14 embed ordering);
//  4. go build the Go MCP server as one CGo-free, statically-linked binary
//     with the UI embedded — CGO_ENABLED=0 is pinned on every invocation;
//  5. cross-compile the RFC §14 target matrix (darwin/linux/windows ×
//     amd64/arm64) and emit a SHA-256 checksum file per artifact.
//
// buildpkg is internal — it is the reusable, testable seam the `dockyard
// build` cobra verb and the Phase 20 integration test consume. The cobra RunE
// is a thin wrapper; the pipeline logic lives here (the house pattern, D-082).
//
// Build holds no shared mutable state: it builds fresh state per call and is
// safe to call concurrently with distinct Options.
package buildpkg

// Package releasebuild drives the Dockyard release pipeline — the step
// the `.github/workflows/release.yml` workflow runs on a `v*` tag push
// to produce the cross-compile artifacts attached to the GitHub Release
// (Phase 30 — V1 release engineering, RFC §14).
//
// It is a thin, deterministic wrapper over `internal/buildpkg.Build`:
//
//  1. invoke Build with the full RFC §14 cross-compile matrix
//     (DefaultMatrix) against the dockyard CLI project (cmd/dockyard);
//  2. rename each produced artifact under its release-publish name —
//     "dockyard-<version>-<os>-<arch>[.exe]" — so a downloaded
//     binary's filename carries the version a user is auditing;
//  3. write an aggregate "checksums.txt" alongside the artifacts, in
//     the same `sha256sum -c`-compatible line shape buildpkg already
//     emits per artifact.
//
// The package is internal — it is the reusable, testable seam the
// `releasebuild` cmd (and the Phase 30 release workflow) consume.
// Release holds no shared mutable state; it is safe to invoke
// concurrently with distinct Options.
//
// Why a separate package rather than a new buildpkg knob: buildpkg's
// surface is the per-project "dockyard build" pipeline a developer
// runs against their own project. The release driver is dockyard's own
// project + a tag-shaped artifact-naming convention + the aggregate
// checksums sidecar — the same primitives, a different audience. Two
// packages keep each one's surface honest.
package releasebuild

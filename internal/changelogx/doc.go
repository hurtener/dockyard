// Package changelogx parses Dockyard's top-level CHANGELOG.md and extracts
// one version's section as plain Markdown (Phase 30 — V1 release engineering).
//
// The CHANGELOG follows the "Keep a Changelog" format
// (https://keepachangelog.com/en/1.1.0/): one Markdown document with a
// top-level title, a preamble, and one `## [<version>] - <YYYY-MM-DD>` (or
// `## [Unreleased]`) heading per release. ExtractSection returns the body
// between a named version's heading and the next H2 heading — exactly the
// text the `release` workflow attaches to the GitHub Release as the body.
//
// The package is read-only and pure-functional. It is consumed by the small
// cmd/changelogx CLI the release workflow invokes; the same Extract function
// is callable directly from a test.
//
// Why we don't lean on a third-party CommonMark parser: the Keep-a-Changelog
// section-extraction surface is tiny (heading match + next-H2 boundary), and
// the release-pipeline gate's correctness is best served by a small, pinned
// stdlib-only parser that the in-repo CHANGELOG is golden-tested against. A
// new release shape becomes a visible diff against a fixture, not a silent
// behaviour change in a transitively-updated dependency.
package changelogx

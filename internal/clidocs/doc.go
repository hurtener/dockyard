// Package clidocs is the small helper that renders the dockyard CLI
// command tree as a Markdown page for the published documentation site
// (Phase 29 — docs/site/cli/index.md).
//
// Why a helper instead of hand-written CLI docs: the cobra command tree
// already carries every verb's Long help, flag list, and short
// description. A hand-maintained CLI reference would drift the first
// time a flag was added without a docs update. The helper walks the
// command tree once at docs-build time and produces a deterministic
// Markdown page — same source of truth, no drift.
//
// The package exposes one function (Render) and a small cmd/ binary
// (cmd/clidocs) that the Makefile invokes from `make docs`.
package clidocs

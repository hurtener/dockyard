package inspector

import (
	"embed"
	"io/fs"
)

// distFS embeds the inspector frontend bundle. The directive points at the
// in-package dist/ tree; a tracked .gitkeep anchor keeps the directory
// resolvable so the Go build never fails for lack of an embed target.
//
// The real bundle is produced by the `make inspector-bundle` target (a
// prerequisite of `make build`), which runs `vite build` for `web/inspector`
// and stages the output into this tree — so a `bin/dockyard` produced by
// `make build` serves the production SPA. The bundle output (the real
// index.html plus the hashed assets/) is .gitignored — only the .gitkeep
// anchor is committed, keeping the working tree clean across rebuilds
// (remediation R4 B1; supersedes D-098's committed-placeholder scheme).
//
// When no real bundle has been staged (a fresh clone before `make build`,
// or a developer who runs `go build` directly), the embedded FS still
// resolves but carries no index.html — the inspector backend's frontend
// handler falls back to its in-Go placeholder page (see assets.go), so the
// backend is always usable. Any caller may also pass a freshly built bundle
// via [Options.Assets] — the embed is the default, not the only, source.
//
//go:embed all:dist
var distFS embed.FS

// EmbeddedAssets returns the embedded inspector frontend rooted at its dist/
// directory, ready to pass as [Options.Assets]. When no real bundle has been
// staged (the dist/ tree carries only its .gitkeep anchor), the inspector
// serves its built-in placeholder page (see assets.go) — the backend is
// always usable.
func EmbeddedAssets() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// The embed directive guarantees dist/ exists; a failure here is a
		// build-time impossibility. Return the unrooted FS as a safe fallback.
		return distFS
	}
	return sub
}

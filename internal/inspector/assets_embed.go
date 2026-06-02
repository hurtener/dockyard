package inspector

import (
	"embed"
	"io/fs"
)

// distFS embeds the inspector frontend bundle. The directive points at the
// in-package dist/ tree; a tracked .gitkeep anchor keeps the directory
// resolvable even if the bundle is ever pruned.
//
// The real bundle (the SPA index.html plus the hashed assets/) is produced by
// `make inspector-bundle` (a prerequisite of `make build`), which runs
// `vite build` for `web/inspector` and stages the output here — and it is
// COMMITTED to the repository (D-187). It must be: `go install …@latest` builds
// from the module proxy's committed source, and the cross-compiled release
// binaries build from a fresh checkout — neither runs `make inspector-bundle`,
// so a gitignored bundle (the earlier D-098 / .gitkeep-only scheme) left every
// distributed binary embedding only the placeholder. CI's
// `make inspector-bundle-check` fails the build if the committed bundle is
// missing, the placeholder, or empty (a structural check — vite output is not
// byte-reproducible across platforms, so refreshing the bundle when
// web/inspector source changes is a committer/reviewer responsibility).
//
// If the bundle is ever absent (a hand-pruned tree), the embedded FS still
// resolves but carries no index.html — the inspector backend's frontend handler
// falls back to its in-Go placeholder page (see assets.go), so the backend is
// always usable. Any caller may also pass a freshly built bundle via
// [Options.Assets] — the embed is the default, not the only, source.
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

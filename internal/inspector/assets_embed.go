package inspector

import (
	"embed"
	"io/fs"
)

// distFS embeds the inspector frontend bundle. The directive points at the
// in-package dist/ tree; a committed placeholder index.html keeps the
// //go:embed directive resolvable so the Go build never depends on a prior
// frontend build (it is not cleaned by `make clean`, which only clears the
// web/ project dist/ trees).
//
// Phase 22 ships the placeholder bundle here; wiring the production
// `web/inspector` Vite build into this tree is the inspector's packaging step,
// completed with the `dockyard inspect` command in Phase 23. Any caller may
// already pass a freshly built bundle via [Options.Assets] — the embed is the
// default, not the only, source.
//
//go:embed all:dist
var distFS embed.FS

// EmbeddedAssets returns the embedded inspector frontend rooted at its dist/
// directory, ready to pass as [Options.Assets]. When only the committed
// placeholder is present, the inspector serves its built-in placeholder page
// (see assets.go) — the backend is always usable.
func EmbeddedAssets() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// The embed directive guarantees dist/ exists; a failure here is a
		// build-time impossibility. Return the unrooted FS as a safe fallback.
		return distFS
	}
	return sub
}

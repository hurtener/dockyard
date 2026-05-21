package apps

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// ErrEmptyBundle is returned (wrapped) when a Bundle's embed target carries no
// built UI — the dist/ tree the //go:embed directive points at is absent or
// empty. Callers branch on it with errors.Is.
//
// A missing dist/ directory makes the Go *build* fail at the //go:embed
// directive itself (the compiler cannot embed a path that does not exist);
// ErrEmptyBundle is the runtime-side analogue — a clean, typed failure when the
// directive resolved but the tree it embedded holds no files (RFC §14, the
// "build fails cleanly if the dist/ embed target is absent" criterion).
var ErrEmptyBundle = errors.New("dockyard/runtime/apps: empty UI bundle")

// ErrBundleEntryNotFound is returned (wrapped) by Bundle.HTML when a discovered
// App's built HTML is not present in the embedded bundle.
var ErrBundleEntryNotFound = errors.New("dockyard/runtime/apps: bundle entry not found")

// Bundle is a read-only, embed.FS-backed view of the built Svelte UI — the
// dist/ tree produced by `vite build` and compiled into the Go binary via
// `//go:embed all:dist` (RFC §14, brief 06 §2.2).
//
// A single Bundle (one embed.FS) backs the ui:// MCP resource handler; the same
// embed.FS also backs the inspector's HTTP preview (Phase 22) — there is never
// a second copy of the UI assets. A Bundle is immutable after NewBundle, so it
// is safe for concurrent use.
type Bundle struct {
	// fsys is the filesystem the built UI was embedded into. For a generated
	// project this is an embed.FS populated by `//go:embed all:dist`.
	fsys fs.FS
	// root is the directory within fsys that holds the built UI (e.g. "dist").
	root string
}

// NewBundle returns a Bundle that serves the built UI rooted at root within
// fsys. root is the directory the //go:embed directive embedded — typically
// "dist". NewBundle does not touch the filesystem; call Validate to check the
// embed target is non-empty.
func NewBundle(fsys fs.FS, root string) Bundle {
	return Bundle{fsys: fsys, root: path.Clean(root)}
}

// Validate reports whether the Bundle's embed target carries a built UI. It
// returns an error wrapping ErrEmptyBundle when the dist/ tree is absent or
// holds no regular files — the clean, typed failure RFC §14 requires instead of
// a panic. A nil return means the bundle has at least one built file.
func (b Bundle) Validate() error {
	if b.fsys == nil {
		return fmt.Errorf("%w: no embedded filesystem", ErrEmptyBundle)
	}
	files := 0
	walkErr := fs.WalkDir(b.fsys, b.root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files++
		}
		return nil
	})
	if walkErr != nil {
		// A walk error on the root means the embed target does not exist.
		return fmt.Errorf("%w: %s: %w", ErrEmptyBundle, b.root, walkErr)
	}
	if files == 0 {
		return fmt.Errorf("%w: %s holds no built UI files", ErrEmptyBundle, b.root)
	}
	return nil
}

// HTML returns the built HTML for a discovered App entry, read from the
// embedded bundle. entry is a DiscoveredApp.Entry — a
// "web/src/apps/<stem>.svelte" path; HTML maps it to the built artifact
// "<root>/<stem>.html" (the single-file bundle `vite build` emits per App, with
// vite-plugin-singlefile — RFC §7.4).
//
// HTML never panics: a missing artifact is returned as an error wrapping
// ErrBundleEntryNotFound.
func (b Bundle) HTML(entry string) ([]byte, error) {
	stem := entryStem(entry)
	if stem == "" {
		return nil, fmt.Errorf("%w: entry %q has no .svelte stem",
			ErrBundleEntryNotFound, entry)
	}
	name := path.Join(b.root, stem+".html")
	data, err := fs.ReadFile(b.fsys, name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrBundleEntryNotFound, name, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: %s is empty", ErrBundleEntryNotFound, name)
	}
	return data, nil
}

// entryStem extracts the file stem of a web/src/apps/<stem>.svelte entry path.
// It returns "" when entry is not a .svelte path.
func entryStem(entry string) string {
	base := path.Base(path.Clean(entry))
	if !strings.HasSuffix(base, svelteExt) {
		return ""
	}
	return strings.TrimSuffix(base, svelteExt)
}

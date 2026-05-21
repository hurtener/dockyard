package apps

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hurtener/dockyard/runtime/server"
)

// ConventionDir is the path, relative to a Dockyard project root, under which a
// .svelte file is discovered by convention and registered as a ui:// resource
// (RFC §7.6). Keeping it a single constant means the convention is one edit to
// move, never a scattered literal.
const ConventionDir = "web/src/apps"

// svelteExt is the file extension that marks an App component under the
// convention directory.
const svelteExt = ".svelte"

// DiscoveredApp is one .svelte file found under ConventionDir, lifted to a
// registrable ui:// App. Its ID, URI, and Entry mirror a manifest apps[] entry
// (internal/manifest.App) so the discovered wiring can be written straight into
// dockyard.app.yaml — RFC §7.6's "convenience without hiding the architecture".
type DiscoveredApp struct {
	// ID is the manifest-local identifier, derived from the file stem with
	// hyphens normalised to underscores so it satisfies the manifest's
	// identifier grammar (a tools[].ui reference targets this id).
	ID string
	// URI is the ui:// resource URI: ui://<manifestName>/<stem>.
	URI string
	// Entry is the .svelte source path relative to the project root, e.g.
	// "web/src/apps/customer-health.svelte" — the manifest apps[].entry value.
	Entry string
	// Stem is the file stem (no extension) — the key Bundle.HTML maps to the
	// built "<stem>.html" artifact.
	Stem string
}

// Discover walks ConventionDir under root and returns every .svelte file as a
// DiscoveredApp, sorted by ID for a deterministic result. manifestName is the
// manifest's `name` field — it forms the host segment of each ui:// URI.
//
// A missing convention directory is not an error: a plain MCP server has no UI
// (RFC §7.1), so Discover returns an empty slice and a nil error. Discovery
// only reads the filesystem; it never registers anything or mutates the
// manifest — RegisterDiscovered and manifest.WriteDiscoveredApps do that.
func Discover(root, manifestName string) ([]DiscoveredApp, error) {
	if manifestName == "" {
		return nil, fmt.Errorf("%w: manifest name is required to form ui:// URIs", ErrInvalidApp)
	}
	dir := filepath.Join(root, filepath.FromSlash(ConventionDir))
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("dockyard/runtime/apps: stat %s: %w", ConventionDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrInvalidApp, ConventionDir)
	}

	var out []DiscoveredApp
	seen := map[string]string{} // id -> entry, for collision reporting
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), svelteExt) {
			return nil
		}
		stem := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		id := identFromStem(stem)
		if id == "" {
			return fmt.Errorf("%w: %s yields no valid manifest identifier", ErrInvalidApp, d.Name())
		}
		// Entry is always forward-slashed and project-root-relative — it lands
		// verbatim in dockyard.app.yaml, which is OS-independent.
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return fmt.Errorf("dockyard/runtime/apps: relativise %s: %w", p, relErr)
		}
		entry := filepath.ToSlash(rel)
		if prev, dup := seen[id]; dup {
			return fmt.Errorf("%w: %s and %s both map to manifest id %q",
				ErrInvalidApp, prev, entry, id)
		}
		seen[id] = entry
		out = append(out, DiscoveredApp{
			ID:    id,
			URI:   uiScheme + manifestName + "/" + stem,
			Entry: entry,
			Stem:  stem,
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// identFromStem normalises a file stem into a manifest identifier: lowercase,
// leading letter, letters/digits/hyphens/underscores only. Hyphens are kept
// (the manifest grammar permits them); any other character collapses out. It
// returns "" when the stem yields nothing usable.
func identFromStem(stem string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(stem) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	id := b.String()
	id = strings.Trim(id, "-_")
	if id == "" || id[0] < 'a' || id[0] > 'z' {
		return ""
	}
	return id
}

// RegisterDiscovered registers a discovered App as a ui:// resource on s,
// composing Register (Phase 09, RFC §7.1). The App's HTML body is read from the
// embedded bundle via Bundle.HTML — so the same //go:embed all:dist embed.FS
// backs the ui:// resource handler (RFC §14).
//
// RegisterDiscovered carries the deny-by-default CSP (the zero apps.App.CSP):
// a discovered App is a single-file bundle with no declared external origins,
// so the secure default applies (RFC §7.4). A developer who needs a CSP opt-out
// edits the apps[] entry in dockyard.app.yaml and registers with Register.
func RegisterDiscovered(s *server.Server, d DiscoveredApp, bundle Bundle) error {
	if s == nil {
		return fmt.Errorf("%w: RegisterDiscovered on nil server", ErrInvalidApp)
	}
	html, err := bundle.HTML(d.Entry)
	if err != nil {
		return fmt.Errorf("dockyard/runtime/apps: discovered App %q: %w", d.ID, err)
	}
	return Register(s, App{
		URI:  d.URI,
		Name: d.ID,
		HTML: html,
	})
}

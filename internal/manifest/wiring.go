package manifest

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// DiscoveredApp is one App found by the runtime/apps convention discovery
// (RFC §7.6), in the shape WriteDiscoveredApps merges into a manifest. It is a
// deliberately small, manifest-package-local value type so internal/manifest
// never depends on the runtime: the CLI (Wave 7) maps an apps.DiscoveredApp
// onto this struct, keeping the dependency direction one-way.
type DiscoveredApp struct {
	// ID is the manifest-local identifier — the apps[].id a tools[].ui targets.
	ID string
	// URI is the ui:// resource URI the App is served under.
	URI string
	// Entry is the .svelte source path relative to the project root.
	Entry string
	// DisplayModes is the subset of inline|fullscreen|pip the App supports.
	// When empty, WriteDiscoveredApps defaults a new entry to [inline] — the
	// minimal valid set (a manifest apps[] entry requires at least one mode,
	// RFC §7.2); a developer widens it by editing dockyard.app.yaml.
	DisplayModes []DisplayMode
}

// WriteDiscoveredApps merges discovered Apps into the manifest file at path and
// rewrites it (RFC §7.6 — the discovered tool↔UI wiring is written into
// dockyard.app.yaml so it stays inspectable).
//
// The merge is conservative and idempotent:
//
//   - a discovered App whose id is not yet in apps[] is appended as a new entry;
//   - a discovered App whose id already exists is left untouched — a
//     developer-authored entry (CSP opt-outs, display modes, visibility) is
//     never overwritten by discovery;
//   - apps[] is sorted by id afterwards so the file is deterministic.
//
// WriteDiscoveredApps parses the manifest *without* the cross-reference checks
// (so a manifest carrying a tools[].ui that points at an as-yet-undiscovered
// App — the natural pre-discovery state — is still mergeable), then runs full
// structural validation on the *merged* result before writing. It never writes
// an invalid dockyard.app.yaml: a merge whose result is still invalid (for
// example a still-orphan apps[] entry) fails here with a source-located error
// and the file on disk is left untouched.
//
// Re-marshalling through yaml.v3 normalises formatting and does not preserve
// inline comments (D-058); the manifest is machine-authored after
// `dockyard new`, so this is acceptable for V1.
func WriteDiscoveredApps(path string, discovered []DiscoveredApp) error {
	raw, err := os.ReadFile(path) //nolint:gosec // path is a user-supplied manifest location, by design.
	if err != nil {
		return fmt.Errorf("dockyard/internal/manifest: write discovered apps: %w", err)
	}
	m, err := parseManifest(raw, path)
	if err != nil {
		return fmt.Errorf("dockyard/internal/manifest: write discovered apps: %w", err)
	}
	if MergeDiscoveredApps(m, discovered) == 0 {
		// Nothing new — keep the call side-effect-free when discovery found
		// only already-wired Apps.
		return nil
	}
	if err := m.Validate(); err != nil {
		return fmt.Errorf("dockyard/internal/manifest: merged manifest is invalid: %w", err)
	}
	normalizeForMarshal(m)
	out, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("dockyard/internal/manifest: marshal merged manifest: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil { //nolint:gosec // a manifest is not a secret.
		return fmt.Errorf("dockyard/internal/manifest: write %s: %w", path, err)
	}
	return nil
}

// parseManifest decodes a manifest from raw into the typed struct, rejecting a
// YAML syntax error or an unknown field, but skipping the structural
// cross-reference validation Load runs. It is the discovery-write entry point:
// before discovery has run, a manifest legitimately carries a tools[].ui that
// references an apps[] entry not yet present, which Load would reject — yet
// that is precisely the manifest WriteDiscoveredApps must read to resolve.
func parseManifest(raw []byte, source string) (*Manifest, error) {
	if len(raw) == 0 {
		return nil, &Error{Source: source, Msg: "manifest is empty"}
	}
	var m Manifest
	dec := yaml.NewDecoder(byteReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, decodeError(source, err)
	}
	return &m, nil
}

// normalizeForMarshal makes a validated Manifest safe to round-trip through
// yaml.Marshal → Load. The enum fields reject an empty scalar on decode, so an
// in-memory zero value — a tool whose task_support was omitted in the source —
// must be written as its explicit default rather than as "". This is a
// semantic no-op: an absent task_support already means forbidden (RFC §8.4).
func normalizeForMarshal(m *Manifest) {
	for i := range m.Tools {
		if m.Tools[i].TaskSupport == "" {
			m.Tools[i].TaskSupport = TaskSupportForbidden
		}
	}
}

// MergeDiscoveredApps merges discovered Apps into m in place and returns the
// number of new apps[] entries added (0 means every discovered App was already
// wired). It is the pure, side-effect-free core of WriteDiscoveredApps —
// exported so a caller can merge into an in-memory Manifest without a file.
func MergeDiscoveredApps(m *Manifest, discovered []DiscoveredApp) int {
	if m == nil {
		return 0
	}
	added := 0
	for _, d := range discovered {
		if _, exists := m.App(d.ID); exists {
			continue // never overwrite a developer-authored entry.
		}
		modes := d.DisplayModes
		if len(modes) == 0 {
			modes = []DisplayMode{DisplayModeInline}
		}
		m.Apps = append(m.Apps, App{
			ID:           d.ID,
			URI:          d.URI,
			Entry:        d.Entry,
			DisplayModes: modes,
		})
		added++
	}
	if added > 0 {
		sort.Slice(m.Apps, func(i, j int) bool { return m.Apps[i].ID < m.Apps[j].ID })
	}
	return added
}

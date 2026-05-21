package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// discoveredOrderStatus is the order-status App as the runtime/apps convention
// discovery would surface it — not yet present in wiring-base.yaml's apps[].
func discoveredOrderStatus() DiscoveredApp {
	return DiscoveredApp{
		ID:           "order-status",
		URI:          "ui://storefront/order-status",
		Entry:        "web/src/apps/order-status.svelte",
		DisplayModes: []DisplayMode{DisplayModeInline},
	}
}

// discoveredCustomerHealth mirrors the customer-health App that wiring-base.yaml
// already declares — a discovered App whose id collides with an authored one.
func discoveredCustomerHealth() DiscoveredApp {
	return DiscoveredApp{
		ID:    "customer-health",
		URI:   "ui://storefront/customer-health",
		Entry: "web/src/apps/customer-health.svelte",
	}
}

// seedManifest writes the wiring-base.yaml fixture into a temp file and returns
// the path and the original bytes. It centralises the file I/O so the gosec
// taint-analysis exemption lives in one place.
func seedManifest(t *testing.T) (path string, original []byte) {
	t.Helper()
	base, err := os.ReadFile("testdata/wiring-base.yaml")
	if err != nil {
		t.Fatalf("read wiring-base.yaml: %v", err)
	}
	path = filepath.Join(t.TempDir(), "dockyard.app.yaml")
	if err := os.WriteFile(path, base, 0o600); err != nil { //nolint:gosec // path is a test temp dir.
		t.Fatalf("seed manifest: %v", err)
	}
	return path, base
}

// readManifest reads a temp manifest written by seedManifest.
func readManifest(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // path is a test temp dir.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// loadBase parses wiring-base.yaml without the cross-reference checks — the
// base manifest deliberately carries a tools[].ui that points at order-status,
// the App discovery has not yet written, which is exactly the pre-discovery
// state WriteDiscoveredApps must accept.
func loadBase(t *testing.T) *Manifest {
	t.Helper()
	raw, err := os.ReadFile("testdata/wiring-base.yaml")
	if err != nil {
		t.Fatalf("read wiring-base.yaml: %v", err)
	}
	m, err := parseManifest(raw, "testdata/wiring-base.yaml")
	if err != nil {
		t.Fatalf("parse wiring-base.yaml: %v", err)
	}
	return m
}

func TestMergeDiscoveredApps_AddsNewEntry(t *testing.T) {
	m := loadBase(t)
	if _, exists := m.App("order-status"); exists {
		t.Fatal("order-status should not be in the base manifest")
	}
	added := MergeDiscoveredApps(m, []DiscoveredApp{discoveredOrderStatus()})
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	got, ok := m.App("order-status")
	if !ok {
		t.Fatal("order-status not added")
	}
	if got.URI != "ui://storefront/order-status" {
		t.Errorf("URI = %q", got.URI)
	}
	if got.Entry != "web/src/apps/order-status.svelte" {
		t.Errorf("Entry = %q", got.Entry)
	}
	if len(got.DisplayModes) != 1 || got.DisplayModes[0] != DisplayModeInline {
		t.Errorf("DisplayModes = %v, want [inline]", got.DisplayModes)
	}
}

func TestMergeDiscoveredApps_DefaultsDisplayModes(t *testing.T) {
	m := loadBase(t)
	d := discoveredOrderStatus()
	d.DisplayModes = nil // discovery surfaced no modes
	MergeDiscoveredApps(m, []DiscoveredApp{d})
	got, _ := m.App("order-status")
	if len(got.DisplayModes) != 1 || got.DisplayModes[0] != DisplayModeInline {
		t.Errorf("DisplayModes = %v, want default [inline]", got.DisplayModes)
	}
}

func TestMergeDiscoveredApps_PreservesAuthoredEntry(t *testing.T) {
	m := loadBase(t)
	before, _ := m.App("customer-health")
	authoredCSP := append([]string(nil), before.CSP.Connect...)
	authoredVis := append([]Visibility(nil), before.Visibility...)

	added := MergeDiscoveredApps(m, []DiscoveredApp{discoveredCustomerHealth()})
	if added != 0 {
		t.Fatalf("added = %d, want 0 — an authored entry must not be overwritten", added)
	}
	after, _ := m.App("customer-health")
	if len(after.CSP.Connect) != len(authoredCSP) || after.CSP.Connect[0] != authoredCSP[0] {
		t.Errorf("authored CSP changed: %v -> %v", authoredCSP, after.CSP.Connect)
	}
	if len(after.Visibility) != len(authoredVis) {
		t.Errorf("authored visibility changed: %v -> %v", authoredVis, after.Visibility)
	}
}

func TestMergeDiscoveredApps_SortsByID(t *testing.T) {
	m := loadBase(t)
	// Discover order-status (sorts before the authored customer-health? no —
	// "customer-health" < "order-status", so order-status appends and sort is
	// a no-op for ordering, but the call must still produce a sorted slice).
	MergeDiscoveredApps(m, []DiscoveredApp{discoveredOrderStatus()})
	for i := 1; i < len(m.Apps); i++ {
		if m.Apps[i-1].ID > m.Apps[i].ID {
			t.Errorf("apps[] not sorted: %q before %q", m.Apps[i-1].ID, m.Apps[i].ID)
		}
	}
}

func TestMergeDiscoveredApps_NilManifest(t *testing.T) {
	if got := MergeDiscoveredApps(nil, []DiscoveredApp{discoveredOrderStatus()}); got != 0 {
		t.Errorf("MergeDiscoveredApps(nil) = %d, want 0", got)
	}
}

func TestWriteDiscoveredApps_RoundTrips(t *testing.T) {
	path, _ := seedManifest(t)

	if err := WriteDiscoveredApps(path, []DiscoveredApp{discoveredOrderStatus()}); err != nil {
		t.Fatalf("WriteDiscoveredApps: %v", err)
	}

	// The rewritten manifest must load AND validate — RFC §7.6 acceptance.
	m, err := LoadFile(path)
	if err != nil {
		t.Fatalf("rewritten manifest does not load: %v", err)
	}
	if _, ok := m.App("order-status"); !ok {
		t.Error("order-status not persisted")
	}
	if _, ok := m.App("customer-health"); !ok {
		t.Error("authored customer-health lost on rewrite")
	}
}

func TestWriteDiscoveredApps_Idempotent(t *testing.T) {
	path, _ := seedManifest(t)

	if err := WriteDiscoveredApps(path, []DiscoveredApp{discoveredOrderStatus()}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first := readManifest(t, path)
	// A second write with the same discovery set must be a no-op.
	if err := WriteDiscoveredApps(path, []DiscoveredApp{discoveredOrderStatus()}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second := readManifest(t, path)
	if string(first) != string(second) {
		t.Error("WriteDiscoveredApps is not idempotent — second write changed the file")
	}
}

func TestWriteDiscoveredApps_NoNewApps(t *testing.T) {
	path, base := seedManifest(t)

	// Only an already-wired App is discovered — the file must be untouched.
	if err := WriteDiscoveredApps(path, []DiscoveredApp{discoveredCustomerHealth()}); err != nil {
		t.Fatalf("WriteDiscoveredApps: %v", err)
	}
	if string(readManifest(t, path)) != string(base) {
		t.Error("WriteDiscoveredApps rewrote the file when nothing was new")
	}
}

func TestWriteDiscoveredApps_RejectsOrphan(t *testing.T) {
	path, base := seedManifest(t)

	// An App no tool wires would make the manifest invalid (orphan apps[] entry).
	orphan := DiscoveredApp{
		ID:    "unwired-panel",
		URI:   "ui://storefront/unwired-panel",
		Entry: "web/src/apps/unwired-panel.svelte",
	}
	err := WriteDiscoveredApps(path, []DiscoveredApp{orphan})
	if err == nil {
		t.Fatal("WriteDiscoveredApps accepted an orphan App — want a validation error")
	}
	// The file must be left untouched on a rejected merge.
	if string(readManifest(t, path)) != string(base) {
		t.Error("WriteDiscoveredApps mutated the file despite a rejected merge")
	}
}

func TestWriteDiscoveredApps_MissingFile(t *testing.T) {
	err := WriteDiscoveredApps(filepath.Join(t.TempDir(), "nope.yaml"),
		[]DiscoveredApp{discoveredOrderStatus()})
	if err == nil {
		t.Fatal("WriteDiscoveredApps on a missing file should error")
	}
}

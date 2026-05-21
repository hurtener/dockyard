package apps

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/hurtener/dockyard/runtime/server"
)

// newDiscoveryServer builds a server advertising the Apps extension capability,
// for the in-package (white-box) discovery tests.
func newDiscoveryServer(t *testing.T) *server.Server {
	t.Helper()
	extCap, err := ExtensionCapability()
	if err != nil {
		t.Fatalf("ExtensionCapability: %v", err)
	}
	s, err := server.New(
		server.Info{Name: "discovery-test", Version: "1.0.0"},
		&server.Options{
			Logger:     slog.New(slog.DiscardHandler),
			Extensions: []server.ExtensionCapability{extCap},
		},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return s
}

// TestDiscover_FindsSvelteFiles walks the committed testdata convention tree
// and asserts both .svelte Apps are surfaced with the right URI and entry.
func TestDiscover_FindsSvelteFiles(t *testing.T) {
	got, err := Discover("testdata", "storefront")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Discover found %d apps, want 2: %+v", len(got), got)
	}
	// Discover sorts by ID — customer-health before order-status.
	want := []DiscoveredApp{
		{
			ID:    "customer-health",
			URI:   "ui://storefront/customer-health",
			Entry: "web/src/apps/customer-health.svelte",
			Stem:  "customer-health",
		},
		{
			ID:    "order-status",
			URI:   "ui://storefront/order-status",
			Entry: "web/src/apps/order-status.svelte",
			Stem:  "order-status",
		},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("app[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}

// TestDiscover_IgnoresNonSvelte confirms a non-.svelte file under the
// convention path (testdata/.../nested/keep.txt) is not discovered.
func TestDiscover_IgnoresNonSvelte(t *testing.T) {
	got, err := Discover("testdata", "storefront")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, d := range got {
		if filepath.Ext(d.Entry) != svelteExt {
			t.Errorf("Discover surfaced a non-.svelte entry: %q", d.Entry)
		}
	}
}

// TestDiscover_MissingConventionDir proves a project with no web/src/apps/
// directory is a plain MCP server, not an error (RFC §7.1).
func TestDiscover_MissingConventionDir(t *testing.T) {
	got, err := Discover(t.TempDir(), "plain-server")
	if err != nil {
		t.Fatalf("Discover on a UI-less project errored: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Discover found %d apps in a UI-less project, want 0", len(got))
	}
}

func TestDiscover_RequiresManifestName(t *testing.T) {
	_, err := Discover("testdata", "")
	if !errors.Is(err, ErrInvalidApp) {
		t.Errorf("Discover with empty manifest name: err = %v, want ErrInvalidApp", err)
	}
}

// TestDiscover_RejectsIDCollision proves two .svelte files that normalise to
// the same manifest id are reported rather than silently colliding.
func TestDiscover_RejectsIDCollision(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(ConventionDir))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// "orderstatus.svelte" and "order.status.svelte" are distinct files that
	// both normalise to the manifest id "orderstatus" (the dot is stripped) —
	// a collision the developer must resolve.
	for _, name := range []string{"orderstatus.svelte", "order.status.svelte"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("<main/>"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	_, err := Discover(root, "shop")
	if !errors.Is(err, ErrInvalidApp) {
		t.Errorf("Discover with an id collision: err = %v, want ErrInvalidApp", err)
	}
}

// TestRegisterDiscovered_RegistersResource registers a discovered App over a
// real server and confirms it lands as a ui:// resource.
func TestRegisterDiscovered_RegistersResource(t *testing.T) {
	apps0, err := Discover("testdata", "storefront")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	srv := newDiscoveryServer(t)
	bundle := EmbeddedBundle()
	for _, d := range apps0 {
		if err := RegisterDiscovered(srv, d, bundle); err != nil {
			t.Fatalf("RegisterDiscovered %q: %v", d.ID, err)
		}
	}
}

func TestRegisterDiscovered_NilServer(t *testing.T) {
	err := RegisterDiscovered(nil, DiscoveredApp{ID: "x"}, EmbeddedBundle())
	if !errors.Is(err, ErrInvalidApp) {
		t.Errorf("RegisterDiscovered(nil server): err = %v, want ErrInvalidApp", err)
	}
}

// TestRegisterDiscovered_MissingBundleEntry proves a discovered App whose built
// HTML is not in the bundle fails cleanly, never panics.
func TestRegisterDiscovered_MissingBundleEntry(t *testing.T) {
	srv := newDiscoveryServer(t)
	d := DiscoveredApp{
		ID:    "ghost",
		URI:   "ui://storefront/ghost",
		Entry: "web/src/apps/ghost.svelte",
		Stem:  "ghost",
	}
	err := RegisterDiscovered(srv, d, EmbeddedBundle())
	if !errors.Is(err, ErrBundleEntryNotFound) {
		t.Errorf("RegisterDiscovered with no built HTML: err = %v, want ErrBundleEntryNotFound", err)
	}
}

func TestIdentFromStem(t *testing.T) {
	cases := map[string]string{
		"customer-health": "customer-health",
		"Customer Health": "customerhealth",
		"order_status":    "order_status",
		"weird!!name":     "weirdname",
		"---":             "",
		"123abc":          "",
		"":                "",
		"  spaced  ":      "spaced",
		"trailing-":       "trailing",
	}
	for in, want := range cases {
		if got := identFromStem(in); got != want {
			t.Errorf("identFromStem(%q) = %q, want %q", in, got, want)
		}
	}
}

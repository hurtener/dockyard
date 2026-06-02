package inspector

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
)

// TestFrontendHandler_Embedded verifies that when a built frontend is embedded,
// the inspector serves its index.html at "/" and at an unknown SPA route, and
// serves a real asset directly.
func TestFrontendHandler_Embedded(t *testing.T) {
	t.Parallel()
	assets := fstest.MapFS{
		"index.html":       {Data: []byte("<!doctype html><title>built</title>")},
		"assets/app.js":    {Data: []byte("console.log('app')")},
		"assets/style.css": {Data: []byte("body{}")},
	}
	insp, err := New(Options{Assets: assets})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = insp.Serve(ctx) }()
	waitReady(t, insp.URL()+"/api/info")

	// "/" serves the built index.
	if root := httpGet(t, insp.URL()+"/"); !contains(root, "built") {
		t.Fatalf("root did not serve built index: %q", root)
	}
	// An unknown route falls back to index.html (SPA routing).
	if route := httpGet(t, insp.URL()+"/events"); !contains(route, "built") {
		t.Fatalf("SPA fallback did not serve index: %q", route)
	}
	// A real asset is served directly.
	if js := httpGet(t, insp.URL()+"/assets/app.js"); !contains(js, "console.log") {
		t.Fatalf("asset not served: %q", js)
	}
}

// TestFrontendHandler_PlaceholderWhenEmpty verifies the placeholder is served
// when the embedded FS has no index.html.
func TestFrontendHandler_PlaceholderWhenEmpty(t *testing.T) {
	t.Parallel()
	cases := map[string]Options{
		"nil assets":   {},
		"empty assets": {Assets: fstest.MapFS{}},
		"no index":     {Assets: fstest.MapFS{"other.txt": {Data: []byte("x")}}},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			insp, err := New(opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = insp.Serve(ctx) }()
			waitReady(t, insp.URL()+"/api/info")
			if body := httpGet(t, insp.URL()+"/"); !contains(body, "has not been built") {
				t.Fatalf("placeholder not served: %q", body)
			}
		})
	}
}

// TestEmbeddedAssets confirms the embedded inspector bundle wiring resolves —
// the //go:embed directive points at a real, non-empty directory — and that
// the inspector accepts the embedded FS regardless of whether a real Vite
// bundle has been staged. The directory is anchored by a tracked .gitkeep so
// the embed always resolves (remediation R4 B1; supersedes D-098's committed
// `index.html` placeholder). When a `make inspector-bundle` has run, the FS
// carries a real index.html; when not, the inspector serves its in-Go
// placeholder page (covered by TestFrontendHandler_PlaceholderWhenEmpty).
func TestEmbeddedAssets(t *testing.T) {
	t.Parallel()
	assets := EmbeddedAssets()
	if assets == nil {
		t.Fatal("EmbeddedAssets returned nil")
	}
	// The directory anchor (.gitkeep, possibly augmented by a staged bundle) is
	// readable — the //go:embed all:dist directive resolves at build time.
	entries, err := fs.ReadDir(assets, ".")
	if err != nil {
		t.Fatalf("EmbeddedAssets ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("EmbeddedAssets: dist/ tree embedded empty — the .gitkeep anchor is missing")
	}
	insp, err := New(Options{Assets: assets})
	if err != nil {
		t.Fatalf("New with embedded assets: %v", err)
	}
	t.Cleanup(func() { _ = insp.Close() })
}

// TestEmbeddedAssets_RealBundleCommitted asserts the committed inspector bundle
// is the real SPA, not the bare .gitkeep anchor — the regression guard for
// D-187. The bundle is committed precisely so a `go install …@latest` binary
// and the cross-compiled release downloads (neither of which runs
// `make inspector-bundle`) serve the real inspector instead of the in-Go
// placeholder. A fresh checkout carries the committed index.html; if it is ever
// dropped, this fails and so does the distributed inspector.
func TestEmbeddedAssets_RealBundleCommitted(t *testing.T) {
	t.Parallel()
	assets := EmbeddedAssets()
	if !hasIndex(assets) {
		t.Fatal("committed inspector bundle has no index.html — the real SPA is not committed; " +
			"`go install` and the release binaries would ship only the placeholder (D-187). " +
			"Run `make inspector-bundle` and commit internal/inspector/dist/.")
	}
	// The SPA entry document references its hashed JS bundle under assets/ — a
	// cheap structural check that this is a built Vite bundle, not a stub.
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("read committed index.html: %v", err)
	}
	if !contains(string(index), "assets/index-") {
		t.Errorf("committed index.html does not reference a hashed assets/index-*.js bundle:\n%s", index)
	}
}

// TestRelay_Accessors covers the relay's small read-only accessors.
func TestRelay_Accessors(t *testing.T) {
	t.Parallel()
	r := NewRelay("http://127.0.0.1:9/obs/v1/stream")
	if r.ObsURL() != "http://127.0.0.1:9/obs/v1/stream" {
		t.Fatalf("ObsURL = %q", r.ObsURL())
	}
	if r.Dropped() != 0 {
		t.Fatalf("Dropped = %d, want 0", r.Dropped())
	}
	if r.Subscribers() != 0 {
		t.Fatalf("Subscribers = %d, want 0", r.Subscribers())
	}
}

// TestRelay_DropsOnSlowConsumer verifies the relay drops events for a UI client
// that never drains its channel — it never blocks (CLAUDE.md §8).
func TestRelay_DropsOnSlowConsumer(t *testing.T) {
	t.Parallel()
	r := NewRelay("")
	_, unsub := r.Subscribe() // a subscriber that never reads.
	defer unsub()
	for i := 0; i < subscriberBuffer+100; i++ {
		r.fanout([]byte(`{"id":"x"}`))
	}
	if r.Dropped() == 0 {
		t.Fatal("expected drops for a slow consumer, got 0")
	}
}

package inspector

import (
	"context"
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

// TestEmbeddedAssets confirms the embedded inspector bundle is wired and
// carries an index document.
func TestEmbeddedAssets(t *testing.T) {
	t.Parallel()
	assets := EmbeddedAssets()
	if !fileExists(assets, "index.html") {
		t.Fatal("EmbeddedAssets: index.html missing from the embedded bundle")
	}
	insp, err := New(Options{Assets: assets})
	if err != nil {
		t.Fatalf("New with embedded assets: %v", err)
	}
	t.Cleanup(func() { _ = insp.Close() })
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

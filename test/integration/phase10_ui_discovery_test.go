// This file is the Phase 10 cross-subsystem integration test (AGENTS.md §17).
// Phase 10's Deps name shipped phases — Phase 09's runtime/apps, Phase 06's
// internal/manifest, Phase 07's runtime/server — and Phase 10 closes the
// UI-auto-discovery + embed-pipeline seam (RFC §7.6, §14). The test drives the
// surface end to end with real components: it discovers a .svelte convention
// tree, registers each discovered App as a ui:// resource backed by the
// //go:embed all:dist bundle on a real runtime/server served over the SDK
// in-memory transport, reads the resource back through a real SDK client, and
// asserts the discovered wiring written into dockyard.app.yaml loads and
// structurally validates.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/server"
)

// connectPhase10 serves srv over the in-memory transport and returns a
// connected client session, cleaned up on test end.
func connectPhase10(t *testing.T, srv *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case <-srvErr:
		case <-time.After(2 * time.Second):
			t.Error("server did not shut down")
		}
	})
	return session
}

// TestPhase10_DiscoveredAppServesOverMCP discovers the committed .svelte
// convention tree, registers every discovered App from the embedded //go:embed
// bundle, and reads it back over a real MCP transport.
func TestPhase10_DiscoveredAppServesOverMCP(t *testing.T) {
	// 1. Discovery surfaces the .svelte files under web/src/apps/ by convention.
	discovered, err := apps.Discover("../../runtime/apps/testdata", "storefront")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("Discover found %d apps, want 2", len(discovered))
	}

	// 2. The embedded bundle (//go:embed all:dist) validates — it carries a
	//    built UI; this is the "build fails cleanly if dist/ absent" criterion's
	//    positive counterpart.
	bundle := apps.EmbeddedBundle()
	if err := bundle.Validate(); err != nil {
		t.Fatalf("embedded bundle does not validate: %v", err)
	}

	// 3. Each discovered App registers as a ui:// resource, backed by the bundle.
	extCap, err := apps.ExtensionCapability()
	if err != nil {
		t.Fatalf("ExtensionCapability: %v", err)
	}
	srv, err := server.New(
		server.Info{Name: "storefront", Version: "1.0.0"},
		&server.Options{Extensions: []server.ExtensionCapability{extCap}},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	for _, d := range discovered {
		if err := apps.RegisterDiscovered(srv, d, bundle); err != nil {
			t.Fatalf("RegisterDiscovered %q: %v", d.ID, err)
		}
	}

	// 4. The embedded bundle serves over the MCP resources/read handler.
	session := connectPhase10(t, srv)
	ctx := context.Background()
	for _, d := range discovered {
		read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: d.URI})
		if err != nil {
			t.Fatalf("ReadResource %q: %v", d.URI, err)
		}
		if len(read.Contents) != 1 {
			t.Fatalf("ReadResource %q: %d contents, want 1", d.URI, len(read.Contents))
		}
		if read.Contents[0].MIMEType != apps.MIMETypeApp {
			t.Errorf("%q MIME = %q, want %q", d.URI, read.Contents[0].MIMEType, apps.MIMETypeApp)
		}
		want, err := bundle.HTML(d.Entry)
		if err != nil {
			t.Fatalf("bundle.HTML %q: %v", d.Entry, err)
		}
		if read.Contents[0].Text != string(want) {
			t.Errorf("%q served HTML does not match the embedded bundle", d.URI)
		}
	}
}

// TestPhase10_DiscoveredWiringLandsInManifest proves the discovered tool↔UI
// wiring is written into dockyard.app.yaml and the result loads and validates
// (RFC §7.6 — convenience without hiding the architecture).
func TestPhase10_DiscoveredWiringLandsInManifest(t *testing.T) {
	discovered, err := apps.Discover("../../runtime/apps/testdata", "storefront")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// A pre-discovery manifest: tools already wire both ui ids; the apps[]
	// entries are what discovery writes.
	const base = `name: storefront
title: Storefront
version: 0.1.0
runtime:
  transports: [stdio]
  ui:
    framework: svelte
    bundle: single-file
tools:
  - name: show_customer_health
    description: Show the customer health dashboard.
    input: internal/contracts.CustomerHealthInput
    output: internal/contracts.CustomerHealthOutput
    ui: customer-health
  - name: show_order_status
    description: Show the order status panel.
    input: internal/contracts.OrderStatusInput
    output: internal/contracts.OrderStatusOutput
    ui: order-status
quality:
  require_loading_state: true
`
	path := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	// Map the runtime discovery result onto the manifest-package input type —
	// the one-way dependency boundary (internal/manifest never imports runtime).
	wiring := make([]manifest.DiscoveredApp, 0, len(discovered))
	for _, d := range discovered {
		wiring = append(wiring, manifest.DiscoveredApp{ID: d.ID, URI: d.URI, Entry: d.Entry})
	}
	if err := manifest.WriteDiscoveredApps(path, wiring); err != nil {
		t.Fatalf("WriteDiscoveredApps: %v", err)
	}

	// The rewritten manifest must load AND structurally validate.
	m, err := manifest.LoadFile(path)
	if err != nil {
		t.Fatalf("rewritten manifest does not load: %v", err)
	}
	for _, d := range discovered {
		got, ok := m.App(d.ID)
		if !ok {
			t.Errorf("discovered app %q not written to the manifest", d.ID)
			continue
		}
		if got.URI != d.URI || got.Entry != d.Entry {
			t.Errorf("app %q wiring = {%q,%q}, want {%q,%q}",
				d.ID, got.URI, got.Entry, d.URI, d.Entry)
		}
	}
}

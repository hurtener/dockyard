// This file is the Wave 4 wave-end end-to-end integration test (AGENTS.md §17 /
// §17.7 step 5). Wave 4 shipped the MCP Apps extension and the shared UI design
// system (RFC §7): runtime/apps registers a tool↔`ui://` resource pair on a
// real runtime/server — the App resource served as `text/html;profile=mcp-app`
// with a `_meta.ui` choke point, the `io.modelcontextprotocol/ui` extension
// capability advertised through internal/protocolcodec (Phase 09); `.svelte`
// convention auto-discovery lifting files under `web/src/apps/` into
// registrable Apps backed by a `//go:embed all:dist` bundle, with the
// discovered wiring written back into `dockyard.app.yaml` (Phase 10); and the
// pluggable host-profile seam that auto-derives `_meta.ui.domain` — the generic
// verbatim profile and Claude's SHA-256 signed `claudemcpcontent.com` origin
// (Phase 12). `web/bridge` (the View-half `ui/` dialect, Phase 11) and `web/ui`
// (the design system, Phase 10a) are frontend artifacts gated by `make web`;
// this Go E2E does not drive them — see the checkpoint audit for the bridge's
// View-half contract reconciliation.
//
// This test drives the integrated Wave 4 Go surface end to end with REAL
// components and no mocks at the seams: a contract-first tool linked to a
// `ui://` App is registered with the real runtime/apps Apps extension on a real
// runtime/server, served over the SDK in-memory transport to a real SDK client;
// `.svelte` discovery runs over the committed convention tree and registers
// each App from the real embedded bundle; and the resources/read `_meta.ui`
// choke point is exercised through both the generic and the Claude host
// profiles — the whole 09→10→12 chain as one wired path. It covers a failure
// mode on each seam (an invalid `ui://` App, a missing bundle entry, a Claude
// profile handed an empty ServerURL → typed ErrInvalidApp per D-064, an unknown
// host id), proves capability-driven Apps negotiation and host-profile
// selection, and runs an N>=10 concurrency stress under -race against the
// shared Server and Apps registry with a post-teardown goroutine-leak
// assertion. See decision D-068.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/server"
)

// ---- shared fixtures --------------------------------------------------------

// reportIn / reportOut is the Wave 4 contract-first tool pair — the typed Go
// structs runtime/server infers a JSON Schema from (RFC §6, P1). The tool is
// linked to a ui:// App resource, making the pair an MCP App (RFC §7.1).
type reportIn struct {
	Account string `json:"account" jsonschema:"the account to report on"`
}

type reportOut struct {
	Account string `json:"account"`
	Score   int    `json:"score"`
}

// newWave4AppsServer constructs a real runtime/server advertising the Apps
// extension capability — the capability-driven negotiation surface of Wave 4.
func newWave4AppsServer(t *testing.T) *server.Server {
	t.Helper()
	extCap, err := apps.ExtensionCapability()
	if err != nil {
		t.Fatalf("apps.ExtensionCapability: %v", err)
	}
	s, err := server.New(
		server.Info{Name: "wave4-e2e", Title: "Wave 4 E2E", Version: "0.1.0"},
		&server.Options{Logger: quietLogger(), Extensions: []server.ExtensionCapability{extCap}},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return s
}

// registerReportTool registers the contract-first report tool linked to the
// App resource at uri — the tool half of an MCP App (RFC §7.1, brief 01 §2.3).
func registerReportTool(t *testing.T, s *server.Server, uri string) {
	t.Helper()
	meta, err := apps.ToolMetaFor(apps.ToolLink{
		ResourceURI: uri,
		Visibility:  []string{apps.VisibilityModel, apps.VisibilityApp},
	})
	if err != nil {
		t.Fatalf("apps.ToolMetaFor: %v", err)
	}
	err = server.AddTool(s,
		server.ToolDef{
			Name:        "show_report",
			Description: "Show an interactive account report",
			Meta:        meta,
		},
		func(_ context.Context, in reportIn) (reportOut, error) {
			return reportOut{Account: in.Account, Score: 91}, nil
		},
	)
	if err != nil {
		t.Fatalf("server.AddTool: %v", err)
	}
}

// ---- 1. the 09→10→12 chain, exercised end to end ----------------------------

// TestWave4AppsChainEndToEnd drives the whole Wave 4 Go surface as one wired
// path: a contract-first tool linked to a ui:// App is registered with the real
// runtime/apps Apps extension on a real runtime/server, the App's resources/read
// _meta.ui is derived through a host profile, .svelte discovery surfaces the
// committed convention tree and registers each App from the real embedded
// bundle, and the discovered wiring round-trips through a real dockyard.app.yaml.
// No mocks at any seam (AGENTS.md §17).
func TestWave4AppsChainEndToEnd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s := newWave4AppsServer(t)

	// --- Phase 09: register an App + its linked contract-first tool ---
	const uri = "ui://wave4-report/main"
	const html = "<html><body>account report</body></html>"
	if err := apps.Register(s, apps.App{
		URI:  uri,
		Name: "wave4-report",
		HTML: []byte(html),
		// A declared CSP opt-out — proves the resources/read _meta.ui choke
		// point carries exactly the App's declared connect origin and nothing
		// more (RFC §7.4).
		CSP: apps.CSP{Connect: []string{"https://api.example.com"}},
		// --- Phase 12: a domain label routed through the Claude host profile,
		// which derives the signed claudemcpcontent.com origin. ---
		Domain:      "report-origin",
		HostProfile: "claude",
		ServerURL:   "https://wave4.example.com/mcp",
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}
	registerReportTool(t, s, uri)

	// --- Phase 10: discover the committed .svelte convention tree and
	// register each discovered App from the real //go:embed bundle. ---
	discovered, err := apps.Discover("../../runtime/apps/testdata", "wave4-e2e")
	if err != nil {
		t.Fatalf("apps.Discover: %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("Discover found %d apps, want 2", len(discovered))
	}
	bundle := apps.EmbeddedBundle()
	if err := bundle.Validate(); err != nil {
		t.Fatalf("embedded bundle does not validate: %v", err)
	}
	for _, d := range discovered {
		if err := apps.RegisterDiscovered(s, d, bundle); err != nil {
			t.Fatalf("RegisterDiscovered %q: %v", d.ID, err)
		}
	}

	// --- serve the wired surface over a real MCP transport ---
	session := connect(t, s)

	// Capability-driven negotiation: the server advertises the Apps extension.
	init := session.InitializeResult()
	if init == nil || init.Capabilities == nil {
		t.Fatal("no server capabilities in InitializeResult")
	}
	ext, ok := init.Capabilities.Extensions[apps.ExtensionID]
	if !ok {
		t.Fatalf("server did not advertise %q; capabilities = %#v",
			apps.ExtensionID, init.Capabilities)
	}
	if settings, _ := ext.(map[string]any); settings != nil {
		mimes, _ := settings["mimeTypes"].([]any)
		if len(mimes) != 1 || mimes[0] != apps.MIMETypeApp {
			t.Fatalf("extension mimeTypes = %#v, want [%q]", settings["mimeTypes"], apps.MIMETypeApp)
		}
	}

	// The tool↔ui:// pair is discoverable: the tool carries the nested
	// _meta.ui.resourceUri and never the deprecated flat form (brief 01 §2.3).
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "show_report" {
		t.Fatalf("ListTools = %+v, want one tool show_report", list.Tools)
	}
	ui, ok := list.Tools[0].Meta["ui"].(map[string]any)
	if !ok || ui["resourceUri"] != uri {
		t.Fatalf("tool _meta.ui.resourceUri = %#v, want %q", list.Tools[0].Meta["ui"], uri)
	}
	if _, flat := list.Tools[0].Meta["ui/resourceUri"]; flat {
		t.Error("tool _meta carries the deprecated flat ui/resourceUri key")
	}

	// resources/read on the Phase 09 App: HTML, App MIME, and a _meta.ui whose
	// domain is the Claude-derived signed origin (Phase 12) and whose CSP
	// carries exactly the declared connect origin.
	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource %q: %v", uri, err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text != html {
		t.Fatalf("App HTML = %q, want %q", read.Contents[0].Text, html)
	}
	if read.Contents[0].MIMEType != apps.MIMETypeApp {
		t.Fatalf("App MIME = %q, want %q", read.Contents[0].MIMEType, apps.MIMETypeApp)
	}
	rui, ok := read.Contents[0].Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("resources/read _meta.ui missing: %#v", read.Contents[0].Meta)
	}
	// The Claude profile derives <hash32>.claudemcpcontent.com — the core never
	// carried the verbatim "report-origin" label onto the wire (Phase 12).
	domain, _ := rui["domain"].(string)
	if domain == "report-origin" {
		t.Error("host-profile derivation skipped: _meta.ui.domain is the verbatim label")
	}
	if !strings.HasSuffix(domain, ".claudemcpcontent.com") {
		t.Fatalf("_meta.ui.domain = %q, want a *.claudemcpcontent.com signed origin", domain)
	}
	// The signed origin must be stable for the (serverURL, label) pair — the
	// same input the resource was registered with derives the same domain.
	wantDomain, derr := apps.DerivedDomain("claude", "report-origin", "https://wave4.example.com/mcp")
	if derr != nil {
		t.Fatalf("DerivedDomain: %v", derr)
	}
	if domain != wantDomain {
		t.Fatalf("_meta.ui.domain = %q, not stable (want %q)", domain, wantDomain)
	}
	csp, _ := rui["csp"].(map[string]any)
	if csp == nil {
		t.Fatalf("_meta.ui.csp missing: %#v", rui)
	}
	conn, _ := csp["connectDomains"].([]any)
	if len(conn) != 1 || conn[0] != "https://api.example.com" {
		t.Fatalf("_meta.ui.csp.connectDomains = %#v, want one declared origin", conn)
	}

	// The contract-first tool itself works end to end (P1).
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "show_report",
		Arguments: reportIn{Account: "acme"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out reportOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Account != "acme" || out.Score != 91 {
		t.Fatalf("structuredContent = %+v, want {acme 91}", out)
	}

	// Each Phase 10 discovered App serves its embedded-bundle HTML over the same
	// real resources/read handler, with the deny-by-default _meta.ui (no
	// declared CSP) — i.e. no external connect origins.
	for _, d := range discovered {
		dread, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: d.URI})
		if err != nil {
			t.Fatalf("ReadResource discovered %q: %v", d.URI, err)
		}
		want, err := bundle.HTML(d.Entry)
		if err != nil {
			t.Fatalf("bundle.HTML %q: %v", d.Entry, err)
		}
		if len(dread.Contents) != 1 || dread.Contents[0].Text != string(want) {
			t.Errorf("discovered App %q HTML does not match the embedded bundle", d.URI)
		}
		if dread.Contents[0].MIMEType != apps.MIMETypeApp {
			t.Errorf("discovered App %q MIME = %q, want %q",
				d.URI, dread.Contents[0].MIMEType, apps.MIMETypeApp)
		}
		// Deny-by-default: a discovered App declares no CSP, so _meta.ui — if
		// present at all — must carry no connect origins (RFC §7.4).
		if dui, present := dread.Contents[0].Meta["ui"].(map[string]any); present {
			if dcsp, ok := dui["csp"].(map[string]any); ok {
				if dconn, ok := dcsp["connectDomains"].([]any); ok && len(dconn) > 0 {
					t.Errorf("discovered App %q violated deny-by-default CSP: %#v", d.URI, dconn)
				}
			}
		}
	}

	// --- Phase 10: the discovered tool↔UI wiring round-trips through a real
	// dockyard.app.yaml — internal/manifest never imports runtime/apps, so the
	// runtime DiscoveredApp is mapped onto the manifest input type at the seam. ---
	const baseManifest = `name: wave4-e2e
title: Wave 4 E2E
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
`
	mPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	if err := os.WriteFile(mPath, []byte(baseManifest), 0o600); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	wiring := make([]manifest.DiscoveredApp, 0, len(discovered))
	for _, d := range discovered {
		wiring = append(wiring, manifest.DiscoveredApp{ID: d.ID, URI: d.URI, Entry: d.Entry})
	}
	if err := manifest.WriteDiscoveredApps(mPath, wiring); err != nil {
		t.Fatalf("WriteDiscoveredApps: %v", err)
	}
	m, err := manifest.LoadFile(mPath)
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
			t.Errorf("manifest app %q wiring = {%q,%q}, want {%q,%q}",
				d.ID, got.URI, got.Entry, d.URI, d.Entry)
		}
	}
}

// ---- 2. graceful degradation — the non-Apps-host path -----------------------

// TestWave4GracefulDegradation proves a server built WITHOUT the Apps extension
// still serves an App's linked tool and ui:// resource fully — capability-driven
// degradation, never a per-host matrix (RFC §7.5, AGENTS.md §6).
func TestWave4GracefulDegradation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A plain MCP server — no Extensions in Options.
	s, err := server.New(
		server.Info{Name: "wave4-plain", Version: "0.1.0"},
		&server.Options{Logger: quietLogger()},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	const uri = "ui://wave4-report/main"
	if err := apps.Register(s, apps.App{
		URI: uri, Name: "wave4-report", HTML: []byte("<html></html>"),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}
	registerReportTool(t, s, uri)

	session := connect(t, s)

	// The server advertises no Apps extension.
	if init := session.InitializeResult(); init != nil && init.Capabilities != nil {
		if _, ok := init.Capabilities.Extensions[apps.ExtensionID]; ok {
			t.Fatal("plain server advertised the Apps extension")
		}
	}

	// Yet the linked tool still works as a plain MCP tool (RFC §7.5).
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "show_report",
		Arguments: reportIn{Account: "globex"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out reportOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Account != "globex" {
		t.Fatalf("structuredContent account = %q, want globex", out.Account)
	}

	// And the App resource still reads back as a plain MCP resource.
	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text != "<html></html>" {
		t.Fatalf("resource text = %q, want <html></html>", read.Contents[0].Text)
	}
}

// ---- 3. failure modes — at least one per seam -------------------------------

// TestWave4FailureModes proves each Wave 4 seam rejects a malformed input with a
// typed error rather than panicking across a boundary (AGENTS.md §13, D-064).
func TestWave4FailureModes(t *testing.T) {
	t.Parallel()

	// runtime/apps Register: an App with a non-ui:// URI is rejected with a
	// wrapped ErrInvalidApp — the Apps registration seam (Phase 09).
	t.Run("apps/invalid-uri", func(t *testing.T) {
		t.Parallel()
		s := newWave4AppsServer(t)
		err := apps.Register(s, apps.App{
			URI: "https://not-a-ui-scheme/x", Name: "bad", HTML: []byte("<html></html>"),
		})
		if !errors.Is(err, apps.ErrInvalidApp) {
			t.Fatalf("Register(non-ui:// URI): got %v, want ErrInvalidApp", err)
		}
	})

	// runtime/apps Register: an App with empty HTML is rejected, never panics.
	t.Run("apps/empty-html", func(t *testing.T) {
		t.Parallel()
		s := newWave4AppsServer(t)
		err := apps.Register(s, apps.App{URI: "ui://bad/main", Name: "bad"})
		if !errors.Is(err, apps.ErrInvalidApp) {
			t.Fatalf("Register(empty HTML): got %v, want ErrInvalidApp", err)
		}
	})

	// runtime/apps discovery/embed: a discovered App pointing at an entry the
	// embedded bundle does not carry yields a wrapped ErrBundleEntryNotFound —
	// the Phase 10 embed-pipeline seam.
	t.Run("apps/missing-bundle-entry", func(t *testing.T) {
		t.Parallel()
		s := newWave4AppsServer(t)
		err := apps.RegisterDiscovered(s, apps.DiscoveredApp{
			ID:    "ghost",
			URI:   "ui://wave4-e2e/ghost",
			Entry: "web/src/apps/ghost.svelte",
			Stem:  "ghost",
		}, apps.EmbeddedBundle())
		if !errors.Is(err, apps.ErrBundleEntryNotFound) {
			t.Fatalf("RegisterDiscovered(missing entry): got %v, want ErrBundleEntryNotFound", err)
		}
	})

	// runtime/apps host profile: the Claude signing profile handed a non-empty
	// domain label but an empty ServerURL cannot derive a stable signed origin —
	// it returns a typed ErrInvalidApp, never a forgeable origin or a panic
	// (Phase 12, D-064).
	t.Run("apps/claude-profile-empty-serverurl", func(t *testing.T) {
		t.Parallel()
		s := newWave4AppsServer(t)
		err := apps.Register(s, apps.App{
			URI:         "ui://wave4-report/main",
			Name:        "wave4-report",
			HTML:        []byte("<html></html>"),
			Domain:      "report-origin",
			HostProfile: "claude",
			// ServerURL deliberately omitted — D-064 mandates a typed error.
		})
		if !errors.Is(err, apps.ErrInvalidApp) {
			t.Fatalf("Register(claude profile, empty ServerURL): got %v, want ErrInvalidApp", err)
		}
	})

	// runtime/apps host profile: an unregistered host id yields a wrapped
	// ErrUnknownHost — the host-profile registry seam (Phase 12).
	t.Run("apps/unknown-host-profile", func(t *testing.T) {
		t.Parallel()
		_, err := apps.DerivedDomain("nonesuch-host", "label", "https://x.example.com")
		if !errors.Is(err, apps.ErrUnknownHost) {
			t.Fatalf("DerivedDomain(unknown host): got %v, want ErrUnknownHost", err)
		}
	})

	// internal/manifest wiring: WriteDiscoveredApps on a malformed manifest file
	// fails with an error rather than panicking — the Phase 10 manifest seam.
	t.Run("manifest/malformed", func(t *testing.T) {
		t.Parallel()
		bad := filepath.Join(t.TempDir(), manifest.DefaultFilename)
		if err := os.WriteFile(bad, []byte("name: [unterminated\n"), 0o600); err != nil {
			t.Fatalf("seed malformed manifest: %v", err)
		}
		err := manifest.WriteDiscoveredApps(bad, []manifest.DiscoveredApp{
			{ID: "x", URI: "ui://wave4-e2e/x", Entry: "web/src/apps/x.svelte"},
		})
		if err == nil {
			t.Fatal("WriteDiscoveredApps on a malformed manifest: want error, got nil")
		}
	})
}

// ---- 4. host-profile selection — capability-driven derivation ---------------

// TestWave4HostProfileSelection proves the same App domain label derives a
// different _meta.ui.domain under different host profiles — the generic
// verbatim profile vs. Claude's signed origin — and that the selection happens
// through the registry seam, never a hardcoded host branch (RFC §7.5, D-062).
func TestWave4HostProfileSelection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const label = "shared-origin"
	const serverURL = "https://wave4.example.com/mcp"

	// The generic (default) profile passes the label through verbatim.
	generic, err := apps.DerivedDomain("", label, serverURL)
	if err != nil {
		t.Fatalf("DerivedDomain(generic): %v", err)
	}
	if generic != label {
		t.Fatalf("generic profile derived %q, want the verbatim label %q", generic, label)
	}

	// The Claude profile derives a distinct, signed claudemcpcontent.com origin.
	claude, err := apps.DerivedDomain("claude", label, serverURL)
	if err != nil {
		t.Fatalf("DerivedDomain(claude): %v", err)
	}
	if claude == generic {
		t.Fatal("claude profile derived the verbatim label — derivation did not run")
	}
	if !strings.HasSuffix(claude, ".claudemcpcontent.com") {
		t.Fatalf("claude profile derived %q, want a *.claudemcpcontent.com origin", claude)
	}

	// The selection is observable end to end: two Apps with the same label but
	// different host profiles serve different _meta.ui.domain values over the
	// same real resources/read handler.
	s := newWave4AppsServer(t)
	if err := apps.Register(s, apps.App{
		URI: "ui://wave4-generic/main", Name: "wave4-generic", HTML: []byte("<html>g</html>"),
		Domain: label,
	}); err != nil {
		t.Fatalf("Register(generic App): %v", err)
	}
	if err := apps.Register(s, apps.App{
		URI: "ui://wave4-claude/main", Name: "wave4-claude", HTML: []byte("<html>c</html>"),
		Domain: label, HostProfile: "claude", ServerURL: serverURL,
	}); err != nil {
		t.Fatalf("Register(claude App): %v", err)
	}

	session := connect(t, s)
	domainOf := func(uri string) string {
		t.Helper()
		read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("ReadResource %q: %v", uri, err)
		}
		ui, _ := read.Contents[0].Meta["ui"].(map[string]any)
		d, _ := ui["domain"].(string)
		return d
	}
	if got := domainOf("ui://wave4-generic/main"); got != generic {
		t.Fatalf("generic App _meta.ui.domain = %q, want %q", got, generic)
	}
	if got := domainOf("ui://wave4-claude/main"); got != claude {
		t.Fatalf("claude App _meta.ui.domain = %q, want %q", got, claude)
	}
}

// ---- 5. concurrency stress under -race + goroutine-leak gate ----------------

// TestWave4ConcurrencyStress drives the shared Wave 4 reusable artifacts — one
// runtime/server serving many sessions, the process-wide host-profile registry,
// and the resources/read _meta.ui choke point — concurrently from N>=10
// goroutines. The -race detector does the race assertion; the test asserts no
// goroutine leak after teardown (AGENTS.md §5, §14).
func TestWave4ConcurrencyStress(t *testing.T) {
	baseline := stableGoroutineCount()

	// One shared server with one App + linked tool, registered up front. Every
	// worker reads the same resource and calls the same tool concurrently.
	srv := newWave4AppsServer(t)
	const uri = "ui://wave4-stress/main"
	const html = "<html><body>stress</body></html>"
	if err := apps.Register(srv, apps.App{
		URI: uri, Name: "wave4-stress", HTML: []byte(html),
		Domain: "stress-origin", HostProfile: "claude",
		ServerURL: "https://wave4.example.com/mcp",
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}
	registerReportTool(t, srv, uri)

	// The signed origin the App was registered with — every concurrent read
	// must return exactly this.
	wantDomain, err := apps.DerivedDomain("claude", "stress-origin", "https://wave4.example.com/mcp")
	if err != nil {
		t.Fatalf("DerivedDomain: %v", err)
	}

	const workers = 16 // N >= 10
	const iterations = 20

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		go func(w int) {
			defer wg.Done()

			// Each worker gets its own client session against the shared
			// server, and tears it down before returning so the post-wait
			// leak assertion sees a fully unwound wave.
			session, teardown := connectWithTeardown(t, srv)
			defer teardown()

			for range iterations {
				// resources/read: the shared _meta.ui choke point must return
				// identical, un-mutated content under concurrent reads.
				read, err := session.ReadResource(context.Background(),
					&mcpsdk.ReadResourceParams{URI: uri})
				if err != nil {
					t.Errorf("worker %d: ReadResource: %v", w, err)
					return
				}
				if len(read.Contents) != 1 || read.Contents[0].Text != html {
					t.Errorf("worker %d: resource content mismatch", w)
					return
				}
				ui, ok := read.Contents[0].Meta["ui"].(map[string]any)
				if !ok || ui["domain"] != wantDomain {
					t.Errorf("worker %d: _meta.ui.domain = %#v, want %q", w, ui["domain"], wantDomain)
					return
				}

				// The host-profile registry is read on every derivation —
				// exercise it concurrently and directly too.
				if got, derr := apps.DerivedDomain("claude", "stress-origin",
					"https://wave4.example.com/mcp"); derr != nil || got != wantDomain {
					t.Errorf("worker %d: DerivedDomain: got %q err %v", w, got, derr)
					return
				}

				// tools/call: invoke the linked contract-first tool.
				res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
					Name:      "show_report",
					Arguments: reportIn{Account: "w"},
				})
				if err != nil {
					t.Errorf("worker %d: CallTool: %v", w, err)
					return
				}
				if res.IsError {
					t.Errorf("worker %d: CallTool IsError: %+v", w, res.Content)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	assertNoGoroutineLeak(t, baseline)
}

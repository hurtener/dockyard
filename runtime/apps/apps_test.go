package apps_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/server"
)

// decodeStructured re-marshals a CallToolResult's StructuredContent into dst.
func decodeStructured(res *mcpsdk.CallToolResult, dst any) error {
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// echoIn / echoOut are a trivial typed tool contract used across the tests.
type echoIn struct {
	Message string `json:"message"`
}

type echoOut struct {
	Echo string `json:"echo"`
}

func echoHandler(_ context.Context, in echoIn) (echoOut, error) {
	return echoOut{Echo: in.Message}, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// newAppsServer builds a server advertising the Apps extension capability.
func newAppsServer(t *testing.T) *server.Server {
	t.Helper()
	extCap, err := apps.ExtensionCapability()
	if err != nil {
		t.Fatalf("ExtensionCapability: %v", err)
	}
	s, err := server.New(
		server.Info{Name: "apps-test", Version: "1.0.0"},
		&server.Options{Logger: quietLogger(), Extensions: []server.ExtensionCapability{extCap}},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return s
}

// connect serves s over an in-memory transport and returns a connected SDK
// client session — the contract-test backbone (brief 03 §2.3).
func connect(t *testing.T, s *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	serverT, clientT := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srvErr := make(chan error, 1)
	go func() { srvErr <- s.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
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

// uiMeta extracts the _meta.ui object from a wire _meta map, failing the test
// if it is absent or not an object.
func uiMeta(t *testing.T, meta map[string]any) map[string]any {
	t.Helper()
	raw, ok := meta["ui"]
	if !ok {
		t.Fatalf("_meta has no \"ui\" key: %#v", meta)
	}
	ui, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui is not an object: %#v", raw)
	}
	return ui
}

// TestRegisterAndDiscover is the acceptance test for RFC §7.1: a tool↔ui://
// resource pair is registered and an Apps-capable client discovers it — the
// tool carries _meta.ui.resourceUri (nested form) and resources/read returns
// the App HTML with MIME text/html;profile=mcp-app.
func TestRegisterAndDiscover(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)

	const uri = "ui://customer-health/main"
	const html = "<html><body>customer health</body></html>"
	if err := apps.Register(s, apps.App{
		URI:  uri,
		Name: "customer-health",
		HTML: []byte(html),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	toolMeta, err := apps.ToolMetaFor(apps.ToolLink{
		ResourceURI: uri,
		Visibility:  []string{apps.VisibilityModel, apps.VisibilityApp},
	})
	if err != nil {
		t.Fatalf("ToolMetaFor: %v", err)
	}
	if err := server.AddTool(s,
		server.ToolDef{Name: "show_customer_health", Meta: toolMeta},
		echoHandler,
	); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	// The tool is discoverable with its _meta.ui linking it to the resource.
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 {
		t.Fatalf("ListTools = %d tools, want 1", len(list.Tools))
	}
	tui := uiMeta(t, list.Tools[0].Meta)
	if got := tui["resourceUri"]; got != uri {
		t.Fatalf("tool _meta.ui.resourceUri = %v, want %q", got, uri)
	}
	// The nested form only — never the deprecated flat key.
	if _, bad := list.Tools[0].Meta["ui/resourceUri"]; bad {
		t.Fatal("tool _meta carries the deprecated flat ui/resourceUri key")
	}

	// resources/read returns the App HTML with the App MIME type.
	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("ReadResource Contents = %d, want 1", len(read.Contents))
	}
	if read.Contents[0].Text != html {
		t.Fatalf("App HTML = %q, want %q", read.Contents[0].Text, html)
	}
	if read.Contents[0].MIMEType != apps.MIMETypeApp {
		t.Fatalf("App MIME = %q, want %q", read.Contents[0].MIMEType, apps.MIMETypeApp)
	}
}

// TestResourceReadCarriesMeta proves the resources/read response carries
// _meta.ui — the choke point the spec mandates (brief 01 §2.2, RFC §7.1).
func TestResourceReadCarriesMeta(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)

	const uri = "ui://dashboard/main"
	border := true
	if err := apps.Register(s, apps.App{
		URI:           uri,
		Name:          "dashboard",
		HTML:          []byte("<html></html>"),
		CSP:           apps.CSP{Connect: []string{"https://api.example.com"}},
		Permissions:   apps.Permissions{ClipboardWrite: true},
		Domain:        "dashboard-origin",
		PrefersBorder: &border,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	session := connect(t, s)
	read, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	ui := uiMeta(t, read.Contents[0].Meta)

	csp, ok := ui["csp"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui.csp missing or wrong shape: %#v", ui)
	}
	connectDomains, ok := csp["connectDomains"].([]any)
	if !ok || len(connectDomains) != 1 || connectDomains[0] != "https://api.example.com" {
		t.Fatalf("_meta.ui.csp.connectDomains = %#v, want [https://api.example.com]", csp["connectDomains"])
	}
	if ui["domain"] != "dashboard-origin" {
		t.Fatalf("_meta.ui.domain = %v, want dashboard-origin", ui["domain"])
	}
	if ui["prefersBorder"] != true {
		t.Fatalf("_meta.ui.prefersBorder = %v, want true", ui["prefersBorder"])
	}
	perms, ok := ui["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui.permissions missing: %#v", ui)
	}
	if _, ok := perms["clipboardWrite"]; !ok {
		t.Fatalf("_meta.ui.permissions.clipboardWrite missing: %#v", perms)
	}
}

// TestDenyByDefaultCSP is the RFC §7.4 acceptance test: an App that declares no
// CSP gets a deny-by-default policy — no _meta.ui and therefore no external
// origins, which a host reads as the deny-by-default CSP (brief 01 §2.5).
func TestDenyByDefaultCSP(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)

	const uri = "ui://plain/main"
	if err := apps.Register(s, apps.App{
		URI:  uri,
		Name: "plain",
		HTML: []byte("<html></html>"),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	session := connect(t, s)
	read, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	// No declared CSP / permissions / domain ⇒ no _meta.ui object. A host
	// applies its deny-by-default policy: zero external origins (brief 01 §2.5).
	if _, present := read.Contents[0].Meta["ui"]; present {
		t.Fatalf("App with no declared CSP emitted a _meta.ui object: %#v", read.Contents[0].Meta)
	}
}

// TestExtensionCapabilityAdvertised proves the server advertises the
// io.modelcontextprotocol/ui extension during initialize (RFC §7.1, §7.5).
func TestExtensionCapabilityAdvertised(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)
	if err := apps.Register(s, apps.App{
		URI: "ui://x/main", Name: "x", HTML: []byte("<html></html>"),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	session := connect(t, s)
	init := session.InitializeResult()
	if init == nil || init.Capabilities == nil {
		t.Fatal("no server capabilities in InitializeResult")
	}
	ext, ok := init.Capabilities.Extensions[apps.ExtensionID]
	if !ok {
		t.Fatalf("server did not advertise the %q extension; capabilities = %#v",
			apps.ExtensionID, init.Capabilities)
	}
	settings, ok := ext.(map[string]any)
	if !ok {
		t.Fatalf("extension settings not an object: %#v", ext)
	}
	mimes, ok := settings["mimeTypes"].([]any)
	if !ok || len(mimes) != 1 || mimes[0] != apps.MIMETypeApp {
		t.Fatalf("extension mimeTypes = %#v, want [%q]", settings["mimeTypes"], apps.MIMETypeApp)
	}
}

// TestGracefulDegradation proves a plain MCP server — one that advertises no
// Apps extension — still serves the App's tool and resource fully (RFC §7.1,
// §7.5). A non-Apps host simply does not see the extension; the tool's
// _meta.ui is inert metadata it ignores.
func TestGracefulDegradation(t *testing.T) {
	t.Parallel()
	// A server built WITHOUT the Apps extension capability.
	s, err := server.New(
		server.Info{Name: "plain", Version: "1.0.0"},
		&server.Options{Logger: quietLogger()},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	const uri = "ui://w/main"
	if err := apps.Register(s, apps.App{URI: uri, Name: "w", HTML: []byte("<html></html>")}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	toolMeta, err := apps.ToolMetaFor(apps.ToolLink{ResourceURI: uri})
	if err != nil {
		t.Fatalf("ToolMetaFor: %v", err)
	}
	if err := server.AddTool(s,
		server.ToolDef{Name: "echo", Meta: toolMeta}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	// The server advertises no Apps extension...
	init := session.InitializeResult()
	if init.Capabilities != nil {
		if _, ok := init.Capabilities.Extensions[apps.ExtensionID]; ok {
			t.Fatal("plain server unexpectedly advertised the Apps extension")
		}
	}
	// ...yet the tool still works end to end.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "echo",
		Arguments: echoIn{Message: "still works"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var got echoOut
	if err := decodeStructured(res, &got); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if got.Echo != "still works" {
		t.Fatalf("echo = %q, want %q", got.Echo, "still works")
	}
	// ...and the resource still reads back as a plain MCP resource.
	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if read.Contents[0].Text != "<html></html>" {
		t.Fatalf("resource text = %q, want <html></html>", read.Contents[0].Text)
	}
}

// TestRegisterErrors covers the typed-error rejections.
func TestRegisterErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		app  apps.App
	}{
		{"empty URI", apps.App{Name: "x", HTML: []byte("h")}},
		{"non-ui scheme", apps.App{URI: "https://x/y", Name: "x", HTML: []byte("h")}},
		{"bare ui scheme", apps.App{URI: "ui://", Name: "x", HTML: []byte("h")}},
		{"empty name", apps.App{URI: "ui://x/m", HTML: []byte("h")}},
		{"empty HTML", apps.App{URI: "ui://x/m", Name: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newAppsServer(t)
			if err := apps.Register(s, tc.app); err == nil {
				t.Fatalf("Register(%s): want error", tc.name)
			}
		})
	}
	t.Run("nil server", func(t *testing.T) {
		t.Parallel()
		if err := apps.Register(nil, apps.App{URI: "ui://x/m", Name: "x", HTML: []byte("h")}); err == nil {
			t.Fatal("Register(nil): want error")
		}
	})
}

// TestToolMetaForErrors covers the ToolLink validation rejections.
func TestToolMetaForErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		link apps.ToolLink
	}{
		{"empty resourceUri", apps.ToolLink{}},
		{"non-ui scheme", apps.ToolLink{ResourceURI: "http://x"}},
		{"bad visibility", apps.ToolLink{ResourceURI: "ui://x/m", Visibility: []string{"nope"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := apps.ToolMetaFor(tc.link); err == nil {
				t.Fatalf("ToolMetaFor(%s): want error", tc.name)
			}
		})
	}
}

// TestToolMetaForNestedFormOnly proves ToolMetaFor emits the nested form and
// never the deprecated flat _meta["ui/resourceUri"] key (brief 01 §2.3).
func TestToolMetaForNestedFormOnly(t *testing.T) {
	t.Parallel()
	meta, err := apps.ToolMetaFor(apps.ToolLink{ResourceURI: "ui://x/m"})
	if err != nil {
		t.Fatalf("ToolMetaFor: %v", err)
	}
	if _, flat := meta["ui/resourceUri"]; flat {
		t.Fatal("ToolMetaFor emitted the deprecated flat ui/resourceUri key")
	}
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing or wrong shape: %#v", meta)
	}
	if ui["resourceUri"] != "ui://x/m" {
		t.Fatalf("_meta.ui.resourceUri = %v, want ui://x/m", ui["resourceUri"])
	}
}

// TestToolMetaForLegacyOptIn proves the D-177 opt-in: ToolLink with
// EmitLegacyResourceURI set emits BOTH the nested _meta.ui.resourceUri and the
// deprecated flat key, with the flat value equal to the nested resourceUri. The
// default (TestToolMetaForNestedFormOnly above) stays nested-only.
func TestToolMetaForLegacyOptIn(t *testing.T) {
	t.Parallel()
	const uri = "ui://x/m"
	meta, err := apps.ToolMetaFor(apps.ToolLink{
		ResourceURI:           uri,
		EmitLegacyResourceURI: true,
	})
	if err != nil {
		t.Fatalf("ToolMetaFor: %v", err)
	}
	flat, ok := meta["ui/resourceUri"].(string)
	if !ok {
		t.Fatalf("opt-in did not emit the flat key: %#v", meta)
	}
	if flat != uri {
		t.Errorf("flat key = %q, want it equal to the nested resourceUri %q", flat, uri)
	}
	ui, ok := meta["ui"].(map[string]any)
	if !ok || ui["resourceUri"] != uri {
		t.Fatalf("_meta.ui.resourceUri missing or wrong: %#v", meta["ui"])
	}
}

// TestAppVisibilityOnly proves a UI-only action tool (visibility ["app"]) is
// encoded faithfully (brief 01 §2.3).
func TestAppVisibilityOnly(t *testing.T) {
	t.Parallel()
	meta, err := apps.ToolMetaFor(apps.ToolLink{
		ResourceURI: "ui://cart/main",
		Visibility:  []string{apps.VisibilityApp},
	})
	if err != nil {
		t.Fatalf("ToolMetaFor: %v", err)
	}
	ui := meta["ui"].(map[string]any)
	vis, ok := ui["visibility"].([]any)
	if !ok || len(vis) != 1 || vis[0] != apps.VisibilityApp {
		t.Fatalf("_meta.ui.visibility = %#v, want [%q]", ui["visibility"], apps.VisibilityApp)
	}
}

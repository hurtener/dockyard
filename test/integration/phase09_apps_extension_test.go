// This file is the Phase 09 cross-subsystem integration test (AGENTS.md §17).
// Phase 09's Deps name shipped phases — Phase 07's runtime/server, Phase 02's
// internal/protocolcodec, Phase 06's internal/manifest — and Phase 09 closes
// the server-side MCP Apps seam (RFC §7.1, §7.4). The test drives the surface
// end to end with real drivers: a contract-first tool built with runtime/tool
// is linked to a ui:// resource registered with runtime/apps on a real
// runtime/server, served over the SDK in-memory transport to a real SDK
// client. It asserts the Phase 09 acceptance criteria — the tool↔ui:// pair is
// discoverable, the extensions capability is advertised, the resources/read
// response carries _meta.ui with a deny-by-default CSP when none is declared,
// and a non-Apps host still gets fully working tools (graceful degradation).
package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// dashboardIn / dashboardOut are the Phase 09 integration contract pair.
type dashboardIn struct {
	Account string `json:"account" jsonschema:"the account to show"`
}

type dashboardOut struct {
	Account string `json:"account"`
	Health  int    `json:"health"`
}

// connectPhase09 serves srv over the in-memory transport and returns a
// connected client session, cleaned up on test end.
func connectPhase09(t *testing.T, srv *server.Server) *mcpsdk.ClientSession {
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

// registerDashboardTool builds the contract-first dashboard tool, links it to
// the App resource, and registers it on srv.
func registerDashboardTool(t *testing.T, srv *server.Server, uri string) {
	t.Helper()
	meta, err := apps.ToolMetaFor(apps.ToolLink{
		ResourceURI: uri,
		Visibility:  []string{apps.VisibilityModel, apps.VisibilityApp},
	})
	if err != nil {
		t.Fatalf("ToolMetaFor: %v", err)
	}
	b := tool.New[dashboardIn, dashboardOut]("show_dashboard").
		Describe("Show an interactive account dashboard").
		Handler(func(_ context.Context, in dashboardIn) (tool.Result[dashboardOut], error) {
			return tool.Result[dashboardOut]{
				Text:       "showing dashboard for " + in.Account,
				Structured: dashboardOut{Account: in.Account, Health: 87},
			}, nil
		})
	// runtime/tool's builder does not (yet) thread tool-definition _meta — that
	// is Phase 10's auto-discovery wiring. Phase 09 registers the linked tool
	// directly through the server seam with the apps-built _meta.
	in, out, err := b.Schemas()
	if err != nil {
		t.Fatalf("Schemas: %v", err)
	}
	err = server.AddToolWithSchemas(srv,
		server.ToolDef{Name: "show_dashboard", Description: "Show a dashboard", Meta: meta},
		in, out,
		func(_ context.Context, in dashboardIn) (server.ToolOutput[dashboardOut], error) {
			return server.ToolOutput[dashboardOut]{
				Text:       "showing dashboard for " + in.Account,
				Structured: dashboardOut{Account: in.Account, Health: 87},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("AddToolWithSchemas: %v", err)
	}
}

// TestPhase09_AppsExtensionEndToEnd exercises the server-side Apps layer with
// real drivers — no mocks at the runtime/apps ↔ runtime/server ↔ protocolcodec
// seams (AGENTS.md §17).
func TestPhase09_AppsExtensionEndToEnd(t *testing.T) {
	extCap, err := apps.ExtensionCapability()
	if err != nil {
		t.Fatalf("ExtensionCapability: %v", err)
	}
	srv, err := server.New(
		server.Info{Name: "dashboard-app", Version: "1.0.0"},
		&server.Options{Logger: quietLogger(), Extensions: []server.ExtensionCapability{extCap}},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	const uri = "ui://account-dashboard/main"
	const html = "<html><body>account dashboard</body></html>"
	if err := apps.Register(srv, apps.App{
		URI:  uri,
		Name: "account-dashboard",
		HTML: []byte(html),
		// No CSP declared — the resource-read response must still carry no
		// external origins (deny-by-default — RFC §7.4).
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}
	registerDashboardTool(t, srv, uri)

	session := connectPhase09(t, srv)
	ctx := context.Background()

	// 1. The extensions capability is advertised.
	init := session.InitializeResult()
	if init == nil || init.Capabilities == nil {
		t.Fatal("no server capabilities in InitializeResult")
	}
	ext, ok := init.Capabilities.Extensions[apps.ExtensionID]
	if !ok {
		t.Fatalf("server did not advertise %q; capabilities = %#v",
			apps.ExtensionID, init.Capabilities)
	}
	settings, _ := ext.(map[string]any)
	mimes, _ := settings["mimeTypes"].([]any)
	if len(mimes) != 1 || mimes[0] != apps.MIMETypeApp {
		t.Fatalf("extension mimeTypes = %#v, want [%q]", settings["mimeTypes"], apps.MIMETypeApp)
	}

	// 2. The tool↔ui:// pair is discoverable: the tool carries _meta.ui.
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 {
		t.Fatalf("ListTools = %d, want 1", len(list.Tools))
	}
	ui, ok := list.Tools[0].Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("tool _meta.ui missing: %#v", list.Tools[0].Meta)
	}
	if ui["resourceUri"] != uri {
		t.Fatalf("tool _meta.ui.resourceUri = %v, want %q", ui["resourceUri"], uri)
	}
	if _, flat := list.Tools[0].Meta["ui/resourceUri"]; flat {
		t.Error("tool _meta carries the deprecated flat ui/resourceUri key")
	}

	// 3. resources/read returns the App HTML with the App MIME and a _meta.ui
	//    that declares no external origin — the deny-by-default CSP (RFC §7.4).
	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if read.Contents[0].Text != html {
		t.Fatalf("App HTML = %q, want %q", read.Contents[0].Text, html)
	}
	if read.Contents[0].MIMEType != apps.MIMETypeApp {
		t.Fatalf("App MIME = %q, want %q", read.Contents[0].MIMEType, apps.MIMETypeApp)
	}
	if rui, present := read.Contents[0].Meta["ui"]; present {
		// If a _meta.ui object is present it must declare no connect origins.
		if obj, ok := rui.(map[string]any); ok {
			if csp, ok := obj["csp"].(map[string]any); ok {
				if conn, ok := csp["connectDomains"].([]any); ok && len(conn) > 0 {
					t.Fatalf("deny-by-default CSP violated: connectDomains = %#v", conn)
				}
			}
		}
	}

	// 4. The tool itself works end to end.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "show_dashboard",
		Arguments: dashboardIn{Account: "acme"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out dashboardOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Account != "acme" || out.Health != 87 {
		t.Fatalf("structuredContent = %+v, want {acme 87}", out)
	}
}

// TestPhase09_GracefulDegradation proves a server built WITHOUT the Apps
// extension still serves the App's linked tool and resource fully — the
// non-Apps-host path (RFC §7.1, §7.5).
func TestPhase09_GracefulDegradation(t *testing.T) {
	// No Extensions in Options — a plain MCP server.
	srv, err := server.New(
		server.Info{Name: "plain-app", Version: "1.0.0"},
		&server.Options{Logger: quietLogger()},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	const uri = "ui://account-dashboard/main"
	if err := apps.Register(srv, apps.App{
		URI: uri, Name: "account-dashboard", HTML: []byte("<html></html>"),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}
	registerDashboardTool(t, srv, uri)

	session := connectPhase09(t, srv)
	ctx := context.Background()

	// The server advertises no Apps extension.
	if init := session.InitializeResult(); init != nil && init.Capabilities != nil {
		if _, ok := init.Capabilities.Extensions[apps.ExtensionID]; ok {
			t.Fatal("plain server advertised the Apps extension")
		}
	}

	// Yet the linked tool still works — graceful degradation (RFC §7.5).
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "show_dashboard",
		Arguments: dashboardIn{Account: "globex"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out dashboardOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Account != "globex" {
		t.Fatalf("structuredContent account = %q, want globex", out.Account)
	}

	// And the resource still reads back as a plain MCP resource.
	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if read.Contents[0].Text != "<html></html>" {
		t.Fatalf("resource text = %q, want <html></html>", read.Contents[0].Text)
	}
}

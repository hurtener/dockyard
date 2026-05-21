package server_test

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

// TestOptionsExtensions proves a server built with Options.Extensions advertises
// those extension capabilities during the initialize handshake (RFC §5.3).
func TestOptionsExtensions(t *testing.T) {
	t.Parallel()
	s, err := server.New(
		server.Info{Name: "ext", Version: "1.0.0"},
		&server.Options{
			Logger: quietLogger(),
			Extensions: []server.ExtensionCapability{
				{Name: "io.example/feature", Settings: json.RawMessage(`{"level":2}`)},
			},
		},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	session := connect(t, s)
	init := session.InitializeResult()
	if init == nil || init.Capabilities == nil {
		t.Fatal("no server capabilities")
	}
	ext, ok := init.Capabilities.Extensions["io.example/feature"]
	if !ok {
		t.Fatalf("extension not advertised; capabilities = %#v", init.Capabilities)
	}
	settings, ok := ext.(map[string]any)
	if !ok || settings["level"] != float64(2) {
		t.Fatalf("extension settings = %#v, want {level:2}", ext)
	}
	// Inferred capabilities (tools) must survive the explicit extension block.
	if init.Capabilities.Tools == nil {
		t.Fatal("explicit Extensions clobbered the inferred tools capability")
	}
}

// TestOptionsExtensionsErrors covers the typed-error rejections.
func TestOptionsExtensionsErrors(t *testing.T) {
	t.Parallel()
	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		_, err := server.New(server.Info{Name: "x", Version: "1.0.0"}, &server.Options{
			Extensions: []server.ExtensionCapability{{Name: ""}},
		})
		if err == nil {
			t.Fatal("want error for extension with empty Name")
		}
	})
	t.Run("malformed settings", func(t *testing.T) {
		t.Parallel()
		_, err := server.New(server.Info{Name: "x", Version: "1.0.0"}, &server.Options{
			Extensions: []server.ExtensionCapability{
				{Name: "io.example/x", Settings: json.RawMessage(`{not json`)},
			},
		})
		if err == nil {
			t.Fatal("want error for malformed extension settings")
		}
	})
}

// TestToolDefMeta proves ToolDef.Meta surfaces as the tool definition's _meta
// on tools/list (RFC §7.1 — the Apps layer links a tool to its UI here).
func TestToolDefMeta(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	meta := map[string]any{"ui": map[string]any{"resourceUri": "ui://x/main"}}
	if err := server.AddTool(s,
		server.ToolDef{Name: "withmeta", Meta: meta}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	// Mutating the caller's map after registration must not reach the tool.
	meta["ui"] = "mutated"

	session := connect(t, s)
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 {
		t.Fatalf("ListTools = %d, want 1", len(list.Tools))
	}
	ui, ok := list.Tools[0].Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("tool _meta.ui missing or mutated: %#v", list.Tools[0].Meta)
	}
	if ui["resourceUri"] != "ui://x/main" {
		t.Fatalf("tool _meta.ui.resourceUri = %v, want ui://x/main", ui["resourceUri"])
	}
}

// TestResourceContentMeta proves ResourceContent.Meta surfaces as the
// resources/read response _meta — the choke point the Apps spec mandates
// (brief 01 §2.2).
func TestResourceContentMeta(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	readMeta := map[string]any{"ui": map[string]any{"domain": "x-origin"}}
	if err := s.AddResource(
		server.ResourceDef{URI: "ui://x/main", Name: "x", MIMEType: "text/html"},
		func(_ context.Context, _ string) (server.ResourceContent, error) {
			return server.ResourceContent{Text: "<html></html>", Meta: readMeta}, nil
		},
	); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	session := connect(t, s)
	read, err := session.ReadResource(context.Background(),
		&mcpsdk.ReadResourceParams{URI: "ui://x/main"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	ui, ok := read.Contents[0].Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("resource-read _meta.ui missing: %#v", read.Contents[0].Meta)
	}
	if ui["domain"] != "x-origin" {
		t.Fatalf("resource-read _meta.ui.domain = %v, want x-origin", ui["domain"])
	}
}

// TestResourceDefMeta proves ResourceDef.Meta surfaces as the static resource
// declaration's _meta on resources/list.
func TestResourceDefMeta(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	declMeta := map[string]any{"ui": map[string]any{"prefersBorder": true}}
	if err := s.AddResource(
		server.ResourceDef{URI: "ui://x/main", Name: "x", Meta: declMeta},
		staticResource("<html></html>"),
	); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	session := connect(t, s)
	list, err := session.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(list.Resources) != 1 {
		t.Fatalf("ListResources = %d, want 1", len(list.Resources))
	}
	ui, ok := list.Resources[0].Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("resource declaration _meta.ui missing: %#v", list.Resources[0].Meta)
	}
	if ui["prefersBorder"] != true {
		t.Fatalf("resource _meta.ui.prefersBorder = %v, want true", ui["prefersBorder"])
	}
}

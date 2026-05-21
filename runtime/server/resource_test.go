package server_test

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

func staticResource(text string) server.ResourceFunc {
	return func(_ context.Context, _ string) (server.ResourceContent, error) {
		return server.ResourceContent{Text: text}, nil
	}
}

func TestAddResource_Registration(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	def := server.ResourceDef{
		URI:      "ui://app/main.html",
		Name:     "main-ui",
		Title:    "Main UI",
		MIMEType: "text/html",
	}
	if err := s.AddResource(def, staticResource("<html></html>")); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	got := s.Resources()
	if len(got) != 1 || got[0] != "ui://app/main.html" {
		t.Fatalf("Resources() = %v, want [ui://app/main.html]", got)
	}
	// Resources() must return a defensive copy.
	got[0] = "mutated"
	if s.Resources()[0] != "ui://app/main.html" {
		t.Fatal("Resources() leaked its internal slice")
	}
}

func TestAddResource_Errors(t *testing.T) {
	t.Parallel()
	t.Run("nil server", func(t *testing.T) {
		t.Parallel()
		var s *server.Server
		if err := s.AddResource(server.ResourceDef{URI: "ui://x", Name: "x"}, staticResource("")); err == nil {
			t.Fatal("want error for nil server")
		}
	})
	t.Run("empty URI", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		if err := s.AddResource(server.ResourceDef{Name: "x"}, staticResource("")); err == nil {
			t.Fatal("want error for empty URI")
		}
	})
	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		if err := s.AddResource(server.ResourceDef{URI: "ui://x"}, staticResource("")); err == nil {
			t.Fatal("want error for empty name")
		}
	})
	t.Run("nil handler", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		if err := s.AddResource(server.ResourceDef{URI: "ui://x", Name: "x"}, nil); err == nil {
			t.Fatal("want error for nil handler")
		}
	})
	t.Run("duplicate URI", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		def := server.ResourceDef{URI: "ui://dup", Name: "dup"}
		if err := s.AddResource(def, staticResource("a")); err != nil {
			t.Fatalf("first AddResource: %v", err)
		}
		if err := s.AddResource(def, staticResource("b")); err == nil {
			t.Fatal("want error for duplicate resource URI")
		}
	})
	t.Run("non-absolute URI rejected", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		// A scheme-less URI is not absolute; the MCP spec requires an absolute
		// resource URI. The SDK tolerates a scheme-less string, so Dockyard
		// validates the scheme itself and rejects it as an error.
		err := s.AddResource(server.ResourceDef{URI: "not-a-uri", Name: "x"}, staticResource(""))
		if err == nil {
			t.Fatal("want error for non-absolute resource URI")
		}
	})
	t.Run("unparseable URI rejected", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		// A URI that fails to parse must surface as an error, never a panic
		// across the MCP boundary (AGENTS.md §13).
		err := s.AddResource(server.ResourceDef{URI: "%zz", Name: "x"}, staticResource(""))
		if err == nil {
			t.Fatal("want error for unparseable resource URI")
		}
	})
}

// TestResourceReadBack is an acceptance test: a resource registers and reads
// back over a transport (RFC §5.3).
func TestResourceReadBack(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	const body = "<html><body>dockyard</body></html>"
	if err := s.AddResource(server.ResourceDef{
		URI:      "ui://app/index.html",
		Name:     "index",
		MIMEType: "text/html",
	}, staticResource(body)); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	list, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(list.Resources) != 1 || list.Resources[0].URI != "ui://app/index.html" {
		t.Fatalf("ListResources = %+v, want one resource ui://app/index.html", list.Resources)
	}

	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "ui://app/index.html"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("ReadResource Contents = %d, want 1", len(read.Contents))
	}
	if read.Contents[0].Text != body {
		t.Fatalf("resource text = %q, want %q", read.Contents[0].Text, body)
	}
	if read.Contents[0].MIMEType != "text/html" {
		t.Fatalf("resource MIME = %q, want text/html", read.Contents[0].MIMEType)
	}
}

// TestResourceBlob proves a binary resource round-trips its Blob channel.
func TestResourceBlob(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	payload := []byte{0x00, 0x01, 0x02, 0xff}
	if err := s.AddResource(server.ResourceDef{
		URI:      "data://app/blob",
		Name:     "blob",
		MIMEType: "application/octet-stream",
	}, func(_ context.Context, _ string) (server.ResourceContent, error) {
		return server.ResourceContent{Blob: payload}, nil
	}); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	session := connect(t, s)
	read, err := session.ReadResource(context.Background(),
		&mcpsdk.ReadResourceParams{URI: "data://app/blob"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) != 1 || string(read.Contents[0].Blob) != string(payload) {
		t.Fatalf("resource blob = %v, want %v", read.Contents, payload)
	}
}

// TestResourceHandlerError proves a handler error surfaces to the client
// rather than panicking across the MCP boundary (AGENTS.md §13).
func TestResourceHandlerError(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := s.AddResource(server.ResourceDef{URI: "ui://app/broken", Name: "broken"},
		func(_ context.Context, _ string) (server.ResourceContent, error) {
			return server.ResourceContent{}, context.DeadlineExceeded
		}); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	session := connect(t, s)
	if _, err := session.ReadResource(context.Background(),
		&mcpsdk.ReadResourceParams{URI: "ui://app/broken"}); err == nil {
		t.Fatal("ReadResource: want error from failing handler")
	}
}

package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

// TestDefaultHTTPSecurity_AllProtectionsOn asserts the secure default has every
// protection explicitly ON — the explicit-security requirement (RFC §5.2,
// AGENTS.md §7). A Dockyard HTTP deployment never inherits an SDK default.
func TestDefaultHTTPSecurity_AllProtectionsOn(t *testing.T) {
	t.Parallel()
	sec := server.DefaultHTTPSecurity()
	if !sec.DNSRebindingProtection {
		t.Error("DefaultHTTPSecurity: DNSRebindingProtection must be ON")
	}
	if !sec.CrossOriginProtection {
		t.Error("DefaultHTTPSecurity: CrossOriginProtection must be ON")
	}
}

// TestHTTPHandler_NilServer proves the constructor is panic-free on a nil
// receiver (AGENTS.md §13).
func TestHTTPHandler_NilServer(t *testing.T) {
	t.Parallel()
	var s *server.Server
	if _, err := s.HTTPHandler(nil); err == nil {
		t.Fatal("HTTPHandler on nil server: want error")
	}
}

// TestHTTPHandler_NilOptionsUsesSecureDefault proves a nil *HTTPOptions yields
// a usable handler with the secure default posture.
func TestHTTPHandler_NilOptionsUsesSecureDefault(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	h, err := s.HTTPHandler(nil)
	if err != nil {
		t.Fatalf("HTTPHandler(nil): %v", err)
	}
	if h == nil {
		t.Fatal("HTTPHandler(nil) returned a nil handler")
	}
}

// TestHTTPHandler_ZeroSecurityUsesSecureDefault proves an HTTPOptions whose
// Security is the zero value is treated as the secure default — an app must
// opt out deliberately, never by forgetting to set the field.
func TestHTTPHandler_ZeroSecurityUsesSecureDefault(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	// Zero-value Security: cross-origin protection must still be applied, so a
	// cross-origin browser POST is rejected.
	h, err := s.HTTPHandler(&server.HTTPOptions{})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodPost, ts.URL, http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	// A cross-origin, non-safe request that CrossOriginProtection must reject.
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403 (cross-origin protection not applied)", resp.StatusCode)
	}
}

// TestHTTPHandler_BadTrustedOrigin proves a malformed trusted origin is a
// constructor error, not a silent misconfiguration.
func TestHTTPHandler_BadTrustedOrigin(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	_, err := s.HTTPHandler(&server.HTTPOptions{
		Security: server.HTTPSecurity{
			CrossOriginProtection: true,
			TrustedOrigins:        []string{"not a valid origin"},
		},
	})
	if err == nil {
		t.Fatal("HTTPHandler with malformed trusted origin: want error")
	}
}

// httpClientSession connects an SDK client to a Dockyard server over the real
// streamable-HTTP transport via an httptest.Server.
func httpClientSession(t *testing.T, h http.Handler) *mcpsdk.ClientSession {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "http-test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("client connect over HTTP: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestServeStreamableHTTP is an acceptance test: a server serves over the
// streamable-HTTP transport, and a client lists + calls a tool and reads a
// resource end to end (RFC §5.2).
func TestServeStreamableHTTP(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo", Description: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	if err := s.AddResource(server.ResourceDef{
		URI: "ui://app/page", Name: "page", MIMEType: "text/html",
	}, staticResource("<html>http</html>")); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	// Trusted origin so the SDK HTTP client's requests are not CSRF-rejected;
	// security stays explicitly ON, just with the test origin trusted.
	h, err := s.HTTPHandler(&server.HTTPOptions{
		Security: server.DefaultHTTPSecurity(),
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	session := httpClientSession(t, h)
	ctx := context.Background()

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools over HTTP: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "echo" {
		t.Fatalf("ListTools = %+v, want [echo]", list.Tools)
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "echo",
		Arguments: echoIn{Message: "over http"},
	})
	if err != nil {
		t.Fatalf("CallTool over HTTP: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool over HTTP returned IsError: %+v", res.Content)
	}

	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "ui://app/page"})
	if err != nil {
		t.Fatalf("ReadResource over HTTP: %v", err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text != "<html>http</html>" {
		t.Fatalf("ReadResource = %+v, want the registered page body", read.Contents)
	}
}

// TestHTTPHandler_GetServerSeam exercises the getServer per-request seam
// (RFC §5.2): ServerForRequest is invoked once per HTTP request to select the
// serving Server.
func TestHTTPHandler_GetServerSeam(t *testing.T) {
	t.Parallel()
	base := newTestServer(t)

	// The per-request server: a distinct Server with its own tool.
	perReq := newTestServer(t)
	if err := server.AddTool(perReq, server.ToolDef{Name: "scoped"}, echoHandler); err != nil {
		t.Fatalf("AddTool on per-request server: %v", err)
	}

	var calls atomic.Int64
	h, err := base.HTTPHandler(&server.HTTPOptions{
		Security: server.DefaultHTTPSecurity(),
		ServerForRequest: func(_ *http.Request) *server.Server {
			calls.Add(1)
			return perReq
		},
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}

	session := httpClientSession(t, h)
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	// The seam routed to perReq, so its "scoped" tool is what the client sees.
	if len(list.Tools) != 1 || list.Tools[0].Name != "scoped" {
		t.Fatalf("ListTools = %+v, want the per-request server's [scoped] tool", list.Tools)
	}
	if calls.Load() == 0 {
		t.Fatal("ServerForRequest seam was never invoked")
	}
}

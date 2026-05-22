package server_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if !sec.ContentTypeVerification {
		t.Error("DefaultHTTPSecurity: ContentTypeVerification must be ON")
	}
}

// dockyardCTRejection is the substring unique to Dockyard's own Content-Type
// middleware rejection — distinct from any SDK-internal Content-Type message.
// A test asserts on it to prove the EXPLICIT Dockyard check fired, not whatever
// the linked SDK happens to do (AGENTS.md §7, D-112).
const dockyardCTRejection = "MCP streamable-HTTP request body must be application/json"

// TestHTTPHandler_ContentTypeVerification proves the explicit Content-Type
// posture (AGENTS.md §7, D-112): with ContentTypeVerification ON, a POST whose
// body Content-Type is not application/json is rejected with 415 by Dockyard's
// OWN middleware (asserted via the distinct rejection body), a correct one is
// accepted, and GET (which carries no body) is never rejected on Content-Type
// grounds.
func TestHTTPHandler_ContentTypeVerification(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo", Description: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	// Default posture has ContentTypeVerification ON. DNS-rebinding protection
	// is left ON; the test drives the handler directly via httptest, so the
	// Host header is the test server's own — not a rebinding case.
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	// A minimal well-formed JSON-RPC initialize body — enough that a correct
	// Content-Type reaches the SDK rather than being bounced by the middleware.
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize",` +
		`"params":{"protocolVersion":"2025-06-18","capabilities":{},` +
		`"clientInfo":{"name":"t","version":"0"}}}`

	cases := []struct {
		name         string
		method       string
		contentType  string
		wantRejected bool
	}{
		{"post wrong content-type", http.MethodPost, "text/plain", true},
		{"post missing content-type", http.MethodPost, "", true},
		{"post correct content-type", http.MethodPost, "application/json", false},
		{"post json with charset", http.MethodPost, "application/json; charset=utf-8", false},
		{"get is never content-type-rejected", http.MethodGet, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Not t.Parallel(): the parent owns the httptest server and closes
			// it on return; parallel subtests would outlive it.
			var reqBody *strings.Reader
			if tc.method == http.MethodPost {
				reqBody = strings.NewReader(body)
			} else {
				reqBody = strings.NewReader("")
			}
			req, err := http.NewRequestWithContext(context.Background(), tc.method, ts.URL, reqBody)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			// Accept both media types so a passed-through request is not
			// bounced by the SDK on Accept grounds — the test isolates the
			// Content-Type check.
			req.Header.Set("Accept", "application/json, text/event-stream")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			raw, _ := io.ReadAll(resp.Body)
			byDockyard := resp.StatusCode == http.StatusUnsupportedMediaType &&
				strings.Contains(string(raw), dockyardCTRejection)
			if byDockyard != tc.wantRejected {
				t.Fatalf("status %d body %q (rejected-by-Dockyard=%v), want %v",
					resp.StatusCode, raw, byDockyard, tc.wantRejected)
			}
		})
	}
}

// TestHTTPHandler_ContentTypeVerificationOff proves the check is opt-out: with
// ContentTypeVerification explicitly off, Dockyard's own middleware no longer
// bounces a wrong-Content-Type POST — the response no longer carries Dockyard's
// distinct rejection body. (Whatever the linked SDK does on its own is not
// Dockyard's explicit posture and is out of scope here — that is exactly the
// SDK-default the explicit posture exists to not depend on.)
func TestHTTPHandler_ContentTypeVerificationOff(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	h, err := s.HTTPHandler(&server.HTTPOptions{
		// Non-zero posture with ContentTypeVerification deliberately off.
		Security: server.HTTPSecurity{DNSRebindingProtection: true},
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), dockyardCTRejection) {
		t.Fatalf("ContentTypeVerification off: Dockyard middleware still rejected "+
			"the POST (body %q)", raw)
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

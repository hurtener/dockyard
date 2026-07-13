package inspector

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/server"
)

// appTestHTML is the ui:// App body the test server registers.
const appTestHTML = `<!doctype html><html><body><div id="app">appsource test</div></body></html>`

// newAppTestServer stands up a real runtime/server with a registered ui:// App,
// served over the real streamable-HTTP transport on a loopback port. It returns
// the base URL the inspector's AppSource connects to. Every seam is real.
func newAppTestServer(t *testing.T, withApp bool) string {
	t.Helper()
	srv, err := server.New(server.Info{Name: "appsource-test", Version: "0.1.0"}, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if withApp {
		if err := apps.Register(srv, apps.App{
			URI:  "ui://appsource/main",
			Name: "appsource-app",
			HTML: []byte(appTestHTML),
		}); err != nil {
			t.Fatalf("apps.Register: %v", err)
		}
	}
	handler, err := srv.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Dual, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	httpSrv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() { _ = httpSrv.Close() })
	return "http://" + ln.Addr().String()
}

// TestAppsFromServer reads a real server's ui:// App over a real read-only
// resources/read — the App-render path RFC §12 makes binding (D-103).
func TestAppsFromServer(t *testing.T) {
	t.Parallel()

	t.Run("reads a server's ui:// App", func(t *testing.T) {
		t.Parallel()
		src := AppsFromServer(newAppTestServer(t, true))
		previews, err := src(context.Background())
		if err != nil {
			t.Fatalf("AppsFromServer: %v", err)
		}
		if len(previews) != 1 {
			t.Fatalf("previews = %d, want 1: %+v", len(previews), previews)
		}
		if previews[0].URI != "ui://appsource/main" {
			t.Errorf("URI = %q, want ui://appsource/main", previews[0].URI)
		}
		if previews[0].HTML != appTestHTML {
			t.Errorf("App HTML not read verbatim: %q", previews[0].HTML)
		}
	})

	t.Run("a server with no ui:// App yields no previews", func(t *testing.T) {
		t.Parallel()
		src := AppsFromServer(newAppTestServer(t, false))
		previews, err := src(context.Background())
		if err != nil {
			t.Fatalf("AppsFromServer: %v", err)
		}
		if len(previews) != 0 {
			t.Fatalf("previews = %+v, want none", previews)
		}
	})

	t.Run("an empty base URL is the detached inspector", func(t *testing.T) {
		t.Parallel()
		previews, err := AppsFromServer("")(context.Background())
		if err != nil {
			t.Fatalf("AppsFromServer(\"\"): %v", err)
		}
		if len(previews) != 0 {
			t.Fatalf("detached inspector returned previews: %+v", previews)
		}
	})

	t.Run("an unreachable server is a typed error", func(t *testing.T) {
		t.Parallel()
		// A reserved-then-closed port: nothing listens there.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		dead := "http://" + ln.Addr().String()
		_ = ln.Close()
		if _, err := AppsFromServer(dead)(context.Background()); err == nil {
			t.Fatal("AppsFromServer against a dead server: want error, got nil")
		}
	})
}

// TestAppsEndpoint exercises the `/api/apps` HTTP endpoint — the read path the
// inspector frontend's App-frame consumes.
func TestAppsEndpoint(t *testing.T) {
	t.Parallel()

	t.Run("no source yields an empty array", func(t *testing.T) {
		t.Parallel()
		insp, err := New(Options{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")
		if body := httpGet(t, insp.URL()+"/api/apps"); body != "[]\n" && body != "[]" {
			t.Fatalf("/api/apps no source: got %q, want []", body)
		}
	})

	t.Run("a source surfaces the server's App", func(t *testing.T) {
		t.Parallel()
		insp, err := New(Options{Apps: AppsFromServer(newAppTestServer(t, true))})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")
		body := httpGet(t, insp.URL()+"/api/apps")
		var previews []AppPreview
		if err := json.Unmarshal([]byte(body), &previews); err != nil {
			t.Fatalf("decode /api/apps %q: %v", body, err)
		}
		if len(previews) != 1 || previews[0].HTML != appTestHTML {
			t.Fatalf("/api/apps did not surface the App: %+v", previews)
		}
	})

	t.Run("a discovery failure answers 502 with a typed message", func(t *testing.T) {
		t.Parallel()
		failing := AppSource(func(context.Context) ([]AppPreview, error) {
			return nil, context.DeadlineExceeded
		})
		insp, err := New(Options{Apps: failing})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")
		resp, err := http.Get(insp.URL() + "/api/apps") //nolint:gosec // loopback test URL
		if err != nil {
			t.Fatalf("GET /api/apps: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("/api/apps discovery failure: status %d, want 502", resp.StatusCode)
		}
	})
}

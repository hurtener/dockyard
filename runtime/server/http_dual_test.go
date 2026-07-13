package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
)

func TestHTTPHandlerDualLifecycle(t *testing.T) {
	t.Parallel()

	ring := obs.NewRingBuffer(32)
	s, err := server.New(server.Info{Name: "dual-http", Version: "0.0.1"}, &server.Options{Obs: ring})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var mu sync.Mutex
	var meta map[string]any
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, func(ctx context.Context, in echoIn) (echoOut, error) {
		mu.Lock()
		meta = server.RequestMeta(ctx)
		mu.Unlock()
		if err := s.LogBridge().Log(ctx, server.LogRecord{Level: server.LogDebug, Message: "modern request"}); err != nil {
			return echoOut{}, err
		}
		return echoOut{Echo: in.Message}, nil
	}); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Dual, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	// The current SDK probes server/discover and then uses the stateless path.
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "dual-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("modern client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "echo", Arguments: echoIn{Message: "hello"},
	}); err != nil {
		t.Fatalf("modern CallTool: %v", err)
	}

	mu.Lock()
	gotMeta := meta
	mu.Unlock()
	if gotMeta[mcpsdk.MetaKeyProtocolVersion] != "2026-07-28" ||
		gotMeta[mcpsdk.MetaKeyClientInfo] == nil || gotMeta[mcpsdk.MetaKeyClientCapabilities] == nil {
		t.Fatalf("modern handler metadata = %#v, want protocol, client info, and capabilities", gotMeta)
	}
	for _, event := range ring.Recent(0) {
		if event.SessionID != "" {
			t.Fatalf("stateless obs event %s has fabricated session ID %q", event.Kind, event.SessionID)
		}
	}

	// A legacy initialize still reaches the same root-mounted handler without a
	// protocol-version header and creates a real session for the 2025 lifecycle.
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy","version":"1"}}}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL, body)
	if err != nil {
		t.Fatalf("legacy request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("legacy initialize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("legacy initialize status = %d, body = %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("Mcp-Session-Id") == "" {
		t.Fatal("legacy initialize did not create an Mcp-Session-Id")
	}
}

func TestHTTPHandlerStatelessVersionValidationPrecedesDecode(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "Mcp-Protocol-Version") {
		t.Fatalf("missing version status/body = %d/%q, want clear pre-decode 400", res.Code, res.Body.String())
	}
}

func TestHTTPHandlerDualRejectsEveryUnsupportedVersion(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Dual, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	for _, version := range []string{"2027-01-01", "2025-06-18", "2025-03-26", "2024-11-05", "2026-01-01", "garbage"} {
		t.Run(version, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader("{"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Mcp-Protocol-Version", version)
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "unsupported MCP protocol version") {
				t.Fatalf("version %q status/body = %d/%q, want clear no-downgrade rejection", version, res.Code, res.Body.String())
			}
		})
	}
}

func TestHTTPHandlerDualAcceptsExplicitLegacyVersion(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Dual, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy","version":"1"}}}`)
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Header().Get("Mcp-Session-Id") == "" {
		t.Fatalf("explicit legacy version status/session = %d/%q", res.Code, res.Header().Get("Mcp-Session-Id"))
	}
}

func TestHTTPHandlerDualValidatesModernRoutingHeaders(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddTool(s, server.ToolDef{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Dual, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hi"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"modern","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "resources/read") // Deliberately disagrees with the JSON-RPC method.
	req.Header.Set("Mcp-Name", "echo")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "Mcp-Method") {
		t.Fatalf("mismatched routing header status/body = %d/%q, want 400 mismatch", res.Code, res.Body.String())
	}
}

func TestServerForRequestRoutesLegacyTasksToSelectedServer(t *testing.T) {
	newEngine := func(id string) (*tasks.Engine, tasks.TaskStore) {
		store := tasks.NewInMemoryStore()
		engine, err := tasks.NewEngine(store, &tasks.Options{GenerateID: func() (string, error) { return id, nil }})
		if err != nil {
			t.Fatal(err)
		}
		return engine, store
	}
	engineA, storeA := newEngine("shared-id")
	engineB, storeB := newEngine("shared-id")
	serverA, err := server.New(server.Info{Name: "tenant-a", Version: "1"}, &server.Options{
		Tasks: engineA, TasksAuthContext: func(*http.Request) string { return "auth-a" },
	})
	if err != nil {
		t.Fatal(err)
	}
	serverB, err := server.New(server.Info{Name: "tenant-b", Version: "1"}, &server.Options{
		Tasks: engineB, TasksAuthContext: func(*http.Request) string { return "auth-b" },
	})
	if err != nil {
		t.Fatal(err)
	}
	releaseA := make(chan struct{})
	defer close(releaseA)
	if _, err := engineA.CreateToolTask(context.Background(), tasks.CreateToolCallParams{
		ToolName: "a", AuthContext: "auth-a",
		Run: func(context.Context) (json.RawMessage, error) {
			<-releaseA
			return json.RawMessage(`{}`), nil
		},
	}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := engineB.CreateToolTask(context.Background(), tasks.CreateToolCallParams{
		ToolName: "b", AuthContext: "auth-b",
		Run: func(ctx context.Context) (json.RawMessage, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}, true); err != nil {
		t.Fatal(err)
	}
	h, err := serverA.HTTPHandler(&server.HTTPOptions{
		ProtocolMode: server.Dual,
		Security:     server.DefaultHTTPSecurity(),
		ServerForRequest: func(r *http.Request) *server.Server {
			if r.Header.Get("X-Tenant") == "b" {
				return serverB
			}
			return serverA
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tasks/cancel","params":{"taskId":"shared-id"}}`)
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant", "b")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "error") {
		t.Fatalf("selected legacy cancel status/body = %d/%s", res.Code, res.Body.String())
	}
	recA, err := storeA.Get(context.Background(), "shared-id")
	if err != nil || recA.Status != protocolcodec.TaskWorking {
		t.Fatalf("tenant A task was affected: %#v, %v", recA, err)
	}
	recB, err := storeB.Get(context.Background(), "shared-id")
	if err != nil || recB.Status != protocolcodec.TaskCancelled {
		t.Fatalf("tenant B task was not cancelled: %#v, %v", recB, err)
	}
}

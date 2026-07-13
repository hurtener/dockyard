// This file is the Phase 07 cross-subsystem integration test (AGENTS.md §17).
// Phase 07's Deps name shipped phases (Phase 01 runtime/server, Phase 02
// protocolcodec) and Phase 07 opens a public interface later phases build on:
// the streamable-HTTP transport, typed resource registration, and the
// getServer per-request seam (RFC §5.2). This test drives that surface end to
// end with real components and no mocks at the seam: a contract-first tool
// (runtime/tool) and a resource are registered on a real runtime/server, the
// server is served over the real SDK streamable-HTTP handler behind an
// httptest.Server, and a real SDK client discovers and exercises both. It
// covers the explicit-security posture, the getServer seam, and one failure
// mode (a CSRF-rejected cross-origin request), and runs a concurrency stress
// under -race with a goroutine-leak assertion.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

type pingInput struct {
	Name string `json:"name"`
}

type pingOutput struct {
	Greeting string `json:"greeting"`
}

func pingHandler(_ context.Context, in pingInput) (tool.Result[pingOutput], error) {
	return tool.Result[pingOutput]{
		Text:       "pong",
		Structured: pingOutput{Greeting: "hello " + in.Name},
	}, nil
}

// buildServer registers a contract-first tool and a resource on a real server.
func buildServer(t *testing.T) *server.Server {
	t.Helper()
	s, err := server.New(server.Info{Name: "phase07-app", Version: "0.1.0"},
		&server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	b := tool.New[pingInput, pingOutput]("ping").
		Describe("ping the server").
		Handler(pingHandler)
	if err := b.Register(s); err != nil {
		t.Fatalf("tool Register: %v", err)
	}
	if err := s.AddResource(server.ResourceDef{
		URI: "ui://phase07/page", Name: "page", MIMEType: "text/html",
	}, func(_ context.Context, _ string) (server.ResourceContent, error) {
		return server.ResourceContent{Text: "<html>phase07</html>"}, nil
	}); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	return s
}

// connectHTTP serves h over an httptest.Server and returns a connected SDK
// client session over the real streamable-HTTP transport.
func connectHTTP(t *testing.T, h http.Handler) *mcpsdk.ClientSession {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "phase07-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("client connect over streamable-HTTP: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestPhase07_StreamableHTTPEndToEnd proves the full tool+resource surface is
// reachable over the streamable-HTTP transport with explicit security.
func TestPhase07_StreamableHTTPEndToEnd(t *testing.T) {
	s := buildServer(t)
	h, err := s.HTTPHandler(&server.HTTPOptions{
		Security:  server.DefaultHTTPSecurity(),
		Stateless: true,
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	session := connectHTTP(t, h)
	ctx := context.Background()

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "ping",
		Arguments: pingInput{Name: "dockyard"},
	})
	if err != nil {
		t.Fatalf("CallTool over HTTP: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool over HTTP returned IsError: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got pingOutput
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if got.Greeting != "hello dockyard" {
		t.Fatalf("greeting = %q, want %q", got.Greeting, "hello dockyard")
	}

	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "ui://phase07/page"})
	if err != nil {
		t.Fatalf("ReadResource over HTTP: %v", err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text != "<html>phase07</html>" {
		t.Fatalf("ReadResource = %+v, want the registered page", read.Contents)
	}
}

// TestPhase07_CrossOriginRejected proves the explicit cross-origin protection
// is genuinely enforced — a cross-site browser POST is rejected (the failure
// mode required by AGENTS.md §17).
func TestPhase07_CrossOriginRejected(t *testing.T) {
	s := buildServer(t)
	h, err := s.HTTPHandler(&server.HTTPOptions{
		Security:  server.DefaultHTTPSecurity(),
		Stateless: true,
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodPost, ts.URL, http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403", resp.StatusCode)
	}
}

// TestPhase07_GetServerSeam proves the getServer per-request seam routes each
// HTTP request to the Server selected by ServerForRequest.
func TestPhase07_GetServerSeam(t *testing.T) {
	base := buildServer(t)
	scoped := buildServer(t)

	h, err := base.HTTPHandler(&server.HTTPOptions{
		Security:         server.DefaultHTTPSecurity(),
		ServerForRequest: func(*http.Request) *server.Server { return scoped },
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	session := connectHTTP(t, h)
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "ping" {
		t.Fatalf("ListTools via getServer seam = %+v, want [ping]", list.Tools)
	}
}

// TestPhase07_ConcurrentHTTPSessions stresses one server over many concurrent
// streamable-HTTP sessions under -race, and asserts no goroutine leak after
// teardown.
func TestPhase07_ConcurrentHTTPSessions(t *testing.T) {
	baseline := stableGoroutineCount()

	s := buildServer(t)
	h, err := s.HTTPHandler(&server.HTTPOptions{
		ProtocolMode: server.Stateless20260728,
		Security:     server.DefaultHTTPSecurity(),
	})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)

	const sessions = 10
	var wg sync.WaitGroup
	wg.Add(sessions)
	for i := range sessions {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "conc", Version: "0.0.0"}, nil)
			// DisableStandaloneSSE: a request-response-only client whose
			// goroutines unwind promptly on Close, so the post-teardown
			// goroutine-leak assertion is deterministic.
			session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
				Endpoint:             ts.URL,
				DisableStandaloneSSE: true,
				MaxRetries:           -1,
			}, nil)
			if err != nil {
				t.Errorf("session %d connect: %v", i, err)
				return
			}
			res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "ping",
				Arguments: pingInput{Name: "conc"},
			})
			if err != nil {
				t.Errorf("session %d CallTool: %v", i, err)
			} else if res.IsError {
				t.Errorf("session %d returned IsError", i)
			}
			_ = session.Close()
		}()
	}
	wg.Wait()

	// Close the HTTP server before the leak assertion so its listener and
	// per-connection goroutines have fully unwound.
	ts.CloseClientConnections()
	ts.Close()
	assertNoGoroutineLeak(t, baseline)
}

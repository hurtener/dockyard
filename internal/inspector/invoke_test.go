package inspector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/server"
)

// invokeIn / invokeOut are the typed contract of the test tool the operator
// invokes through `/api/tools/invoke`. The runtime/server schema-validates
// `Greeting` is required at the catalog edge (P1); the structured payload
// reflects the input and threads it back as `Greeted`.
type invokeIn struct {
	Greeting string `json:"greeting"`
}

type invokeOut struct {
	Greeted string `json:"greeted"`
}

// newInvokeTestServer stands up a real runtime/server with one registered
// tool, served over the real streamable-HTTP transport on a loopback port.
// Every seam is real: the test exercises the same client/server path a
// production server uses.
func newInvokeTestServer(t *testing.T) string {
	t.Helper()
	srv, err := server.New(server.Info{Name: "invoke-test", Version: "0.1.0"}, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	handler := func(_ context.Context, in invokeIn) (invokeOut, error) {
		if in.Greeting == "boom" {
			return invokeOut{}, errors.New("greeting boom rejected")
		}
		return invokeOut{Greeted: "hello, " + in.Greeting}, nil
	}
	if err := server.AddTool(srv, server.ToolDef{
		Name:        "greet",
		Description: "Greet the supplied name.",
	}, handler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	httpHandler, err := srv.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	httpSrv := &http.Server{Handler: httpHandler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() { _ = httpSrv.Close() })
	return "http://" + ln.Addr().String()
}

// TestToolsFromServer drives a real tools/call through [ToolsFromServer]
// against a real runtime/server tool — the operator-initiated path D-131
// makes binding.
func TestToolsFromServer(t *testing.T) {
	t.Parallel()

	t.Run("invokes a real tool and returns structured content", func(t *testing.T) {
		t.Parallel()
		invoker := ToolsFromServer(newInvokeTestServer(t))
		resp, err := invoker(context.Background(), InvokeRequest{
			Tool:      "greet",
			Arguments: json.RawMessage(`{"greeting":"world"}`),
		})
		if err != nil {
			t.Fatalf("invoker: %v", err)
		}
		if resp == nil {
			t.Fatal("invoker returned nil response")
		}
		if resp.IsError {
			t.Errorf("IsError = true on a successful call: %+v", resp)
		}
		if len(resp.StructuredContent) == 0 {
			t.Fatalf("StructuredContent empty: %+v", resp)
		}
		var structured invokeOut
		if err := json.Unmarshal(resp.StructuredContent, &structured); err != nil {
			t.Fatalf("decode structured: %v (raw %s)", err, resp.StructuredContent)
		}
		if structured.Greeted != "hello, world" {
			t.Errorf("Greeted = %q, want %q", structured.Greeted, "hello, world")
		}
	})

	t.Run("a tool-level error sets IsError but is a successful RPC", func(t *testing.T) {
		t.Parallel()
		invoker := ToolsFromServer(newInvokeTestServer(t))
		resp, err := invoker(context.Background(), InvokeRequest{
			Tool:      "greet",
			Arguments: json.RawMessage(`{"greeting":"boom"}`),
		})
		if err != nil {
			t.Fatalf("invoker (tool-level error): %v", err)
		}
		if !resp.IsError {
			t.Errorf("IsError = false on a tool-level error: %+v", resp)
		}
	})

	t.Run("an unknown tool is a typed transport-level error", func(t *testing.T) {
		t.Parallel()
		invoker := ToolsFromServer(newInvokeTestServer(t))
		if _, err := invoker(context.Background(), InvokeRequest{
			Tool:      "no-such-tool",
			Arguments: json.RawMessage(`{}`),
		}); err == nil {
			t.Fatal("invoker against an unknown tool: want error, got nil")
		}
	})

	t.Run("a detached inspector returns a typed error", func(t *testing.T) {
		t.Parallel()
		if _, err := ToolsFromServer("")(context.Background(), InvokeRequest{
			Tool:      "greet",
			Arguments: json.RawMessage(`{}`),
		}); err == nil {
			t.Fatal("ToolsFromServer(\"\"): want error, got nil")
		}
	})

	t.Run("an unreachable server is a typed error", func(t *testing.T) {
		t.Parallel()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		dead := "http://" + ln.Addr().String()
		_ = ln.Close()
		if _, err := ToolsFromServer(dead)(context.Background(), InvokeRequest{
			Tool:      "greet",
			Arguments: json.RawMessage(`{}`),
		}); err == nil {
			t.Fatal("ToolsFromServer against a dead server: want error, got nil")
		}
	})
}

// TestInvokeEndpoint exercises `POST /api/tools/invoke` — the operator-driven
// surface the inspector frontend POSTs to.
func TestInvokeEndpoint(t *testing.T) {
	t.Parallel()

	t.Run("no invoker yields 503 with a typed message", func(t *testing.T) {
		t.Parallel()
		insp, err := New(Options{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, body := httpPost(t, insp.URL()+"/api/tools/invoke",
			`{"tool":"greet","arguments":{}}`)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503; body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, "detached") {
			t.Errorf("body %q did not mention detached", body)
		}
	})

	t.Run("a malformed body yields 400", func(t *testing.T) {
		t.Parallel()
		insp, err := New(Options{Invoker: ToolsFromServer(newInvokeTestServer(t))})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, _ := httpPost(t, insp.URL()+"/api/tools/invoke", `{not json`)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("an empty tool name yields 400", func(t *testing.T) {
		t.Parallel()
		insp, err := New(Options{Invoker: ToolsFromServer(newInvokeTestServer(t))})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, _ := httpPost(t, insp.URL()+"/api/tools/invoke",
			`{"tool":"","arguments":{}}`)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("a real invoke returns structured content end-to-end", func(t *testing.T) {
		t.Parallel()
		invoker := ToolsFromServer(newInvokeTestServer(t))
		insp, err := New(Options{Invoker: invoker})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, body := httpPost(t, insp.URL()+"/api/tools/invoke",
			`{"tool":"greet","arguments":{"greeting":"operator"}}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
		}
		var got InvokeResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("decode response: %v (body=%s)", err, body)
		}
		if got.IsError {
			t.Errorf("IsError = true on a successful call: %+v", got)
		}
		var structured invokeOut
		if err := json.Unmarshal(got.StructuredContent, &structured); err != nil {
			t.Fatalf("decode structured %s: %v", got.StructuredContent, err)
		}
		if structured.Greeted != "hello, operator" {
			t.Errorf("Greeted = %q, want %q", structured.Greeted, "hello, operator")
		}
	})

	t.Run("a transport failure yields 502 with a typed message", func(t *testing.T) {
		t.Parallel()
		failing := ToolInvoker(func(context.Context, InvokeRequest) (*InvokeResponse, error) {
			return nil, errors.New("simulated transport failure")
		})
		insp, err := New(Options{Invoker: failing})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")

		resp, body := httpPost(t, insp.URL()+"/api/tools/invoke",
			`{"tool":"greet","arguments":{}}`)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", resp.StatusCode)
		}
		if !strings.Contains(body, "simulated transport failure") {
			t.Errorf("body %q did not carry the typed error", body)
		}
	})
}

// httpPost POSTs a JSON body to url and returns the response + body. Reused by
// the invoke tests; mirrors [httpGet]'s loopback-only contract.
func httpPost(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", //nolint:gosec // loopback test URL
		bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	read, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp, string(read)
}

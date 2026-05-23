package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
)

// This file proves the R5 depth-audit remediation:
//
//   S2 — obs/v1 Event.SessionID is populated from the in-flight MCP session
//        once a transport (or the handler edge) calls obs.WithSession on the
//        handler context (D-120). The fix wires it on the tool-handler edge
//        in withRequestSession (req.Session.ID()); this test drives a real
//        tools/call over the streamable-HTTP transport — the SDK transport
//        whose connection actually mints a session id — and asserts the
//        emitted tool.call events carry SessionID != "".
//
//   N1 — a resource handler's obs.SpanContext is threaded onto its handler
//        context via obs.WithSpan (D-121, mirroring D-079's tool-handler
//        seam). An obs/v1 event minted *inside* the read — e.g. an
//        `app.load` produced by runtime/apps's read handler via
//        obs.ChildOrNewTrace — correlates as a CHILD of the read's span
//        rather than minting an unrelated fresh trace.

type r5In struct {
	Message string `json:"message"`
}

type r5Out struct {
	Echo string `json:"echo"`
}

// r5httpServer wires up a Dockyard server with the given obs emitter over a
// real streamable-HTTP transport via httptest.Server and returns a connected
// SDK client session. Mirrors the http_test.go pattern; the difference is the
// obs emitter is passed in so the test can observe what the handler edge stamps.
func r5httpServer(t *testing.T, emitter obs.Emitter, configure func(s *server.Server)) *mcpsdk.ClientSession {
	t.Helper()
	s, err := server.New(server.Info{Name: "r5-obs-test", Version: "0.0.1"}, &server.Options{
		Obs: emitter,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	configure(s)
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "r5-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connect over HTTP: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestR5_S2_ToolCallEventCarriesSessionID drives a real tools/call over the
// streamable-HTTP transport. The R5 wiring stamps obs.WithSession from
// req.Session.ID() onto the handler context, so the obs/v1 tool.call events
// the recorder emits must carry SessionID != "".
func TestR5_S2_ToolCallEventCarriesSessionID(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(64)

	handler := func(_ context.Context, in r5In) (r5Out, error) {
		return r5Out{Echo: in.Message}, nil
	}
	session := r5httpServer(t, ring, func(s *server.Server) {
		if err := server.AddTool(s, server.ToolDef{Name: "s2probe"}, handler); err != nil {
			t.Fatalf("AddTool: %v", err)
		}
	})

	ctx := context.Background()
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "s2probe",
		Arguments: r5In{Message: "hi"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var sawToolCall bool
	for _, ev := range ring.Recent(0) {
		if ev.Kind != obs.KindToolCall {
			continue
		}
		sawToolCall = true
		if ev.SessionID == "" {
			t.Errorf("tool.call (phase=%s) emitted with SessionID=\"\" — R5/S2 regression: "+
				"the in-flight MCP session id was not threaded onto the handler context",
				ev.Phase)
		}
	}
	if !sawToolCall {
		t.Fatal("no obs/v1 tool.call event emitted from the HTTP tools/call")
	}
}

// TestR5_N1_ResourceReadEventCarriesSessionID drives a real resources/read
// over HTTP and asserts the resource.read events carry SessionID — the
// resource edge mirror of the tool edge wiring (withResourceRequestSession).
func TestR5_N1_ResourceReadEventCarriesSessionID(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(64)
	session := r5httpServer(t, ring, func(s *server.Server) {
		read := func(_ context.Context, _ string) (server.ResourceContent, error) {
			return server.ResourceContent{MIMEType: "text/plain", Text: "hello"}, nil
		}
		if err := s.AddResource(server.ResourceDef{URI: "ui://r5/page", Name: "page"}, read); err != nil {
			t.Fatalf("AddResource: %v", err)
		}
	})

	if _, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{
		URI: "ui://r5/page",
	}); err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	var sawResRead bool
	for _, ev := range ring.Recent(0) {
		if ev.Kind != obs.KindResourceRead {
			continue
		}
		sawResRead = true
		if ev.SessionID == "" {
			t.Errorf("resource.read (phase=%s) emitted with SessionID=\"\" — "+
				"R5/S2 regression on the resource edge", ev.Phase)
		}
	}
	if !sawResRead {
		t.Fatal("no obs/v1 resource.read event emitted from the HTTP resources/read")
	}
}

// TestR5_N2_ToolCallInheritsInboundTraceparent proves the N2 fix end to end:
// a tools/call POST carrying a W3C `traceparent` header makes the obs/v1
// tool.call event inherit the caller's trace id and child under the inbound
// span id (D-122). This is the cross-process trace inheritance the OTel doc
// claim had been making in advance of the wiring — now real.
//
// The test bypasses the SDK client (which does not let us set arbitrary HTTP
// headers per request) and POSTs the JSON-RPC initialize + tools/call frames
// directly, so the traceparent header is on the inbound HTTP request the
// transport middleware sees.
func TestR5_N2_ToolCallInheritsInboundTraceparent(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(64)

	s, err := server.New(server.Info{Name: "r5-n2-test", Version: "0.0.1"}, &server.Options{
		Obs: ring,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handler := func(_ context.Context, in r5In) (r5Out, error) {
		return r5Out{Echo: in.Message}, nil
	}
	if err := server.AddTool(s, server.ToolDef{Name: "n2probe"}, handler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	const inboundTrace = "0af7651916cd43dd8448eb211c80319c"
	const inboundSpan = "b7ad6b7169203331"
	const traceparent = "00-" + inboundTrace + "-" + inboundSpan + "-01"

	// Use the SDK client; configure its HTTP client to inject the traceparent
	// on every outbound request — the streamable HTTP transport accepts a
	// custom *http.Client (see mcp.StreamableClientTransport.HTTPClient).
	httpClient := &http.Client{
		Transport: &traceparentInjector{
			rt:          http.DefaultTransport,
			traceparent: traceparent,
		},
		Timeout: 5 * time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "n2-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "n2probe",
		Arguments: r5In{Message: "hi"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var sawToolCall bool
	for _, ev := range ring.Recent(0) {
		if ev.Kind != obs.KindToolCall {
			continue
		}
		sawToolCall = true
		if ev.TraceID != inboundTrace {
			t.Errorf("tool.call (phase=%s) TraceID = %q, want %q — R5/N2 regression: "+
				"the W3C traceparent was not inherited at the handler edge",
				ev.Phase, ev.TraceID, inboundTrace)
		}
		if ev.ParentSpanID != inboundSpan {
			t.Errorf("tool.call (phase=%s) ParentSpanID = %q, want %q (the inbound span id) — "+
				"R5/N2 regression", ev.Phase, ev.ParentSpanID, inboundSpan)
		}
		if ev.SpanID == inboundSpan {
			t.Error("tool.call SpanID == inbound span id; the local unit of work must mint its own")
		}
	}
	if !sawToolCall {
		t.Fatal("no obs/v1 tool.call event emitted")
	}
}

// traceparentInjector is an http.RoundTripper that adds a fixed `Traceparent`
// header to every outgoing request — the simplest harness for driving an
// end-to-end W3C-propagation test without standing up an OTel client.
type traceparentInjector struct {
	rt          http.RoundTripper
	traceparent string
}

func (i *traceparentInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request so we never mutate the caller's headers.
	clone := req.Clone(req.Context())
	clone.Header.Set("Traceparent", i.traceparent)
	return i.rt.RoundTrip(clone)
}

// TestR5_N1_ResourceReadSpanCorrelatesToChildEmits proves the N1 fix end to
// end. The resource handler is registered to emit an extra obs/v1 event from
// INSIDE its handler context (mirroring what runtime/apps's read handler does
// when it emits app.load via obs.ChildOrNewTrace). Without the R5 fix that
// nested event would mint an unrelated fresh trace; with the fix it is a
// child of the resource.read span — same trace id, parent span id = the
// read's span id (the pattern D-079 closed for tool.call/log).
func TestR5_N1_ResourceReadSpanCorrelatesToChildEmits(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(64)

	configure := func(s *server.Server) {
		rec := s.Recorder()
		read := func(ctx context.Context, _ string) (server.ResourceContent, error) {
			// Emit an event from inside the read handler the same way
			// runtime/apps does (ChildOrNewTrace). The R5/N1 fix threads the
			// read's span onto ctx via obs.WithSpan, so this event becomes
			// a child of it.
			rec.AppLoad(ctx, obs.ChildOrNewTrace(ctx), obs.AppLoadPayload{
				AppID:       "n1probe",
				ResourceURI: "ui://r5/n1",
				MIME:        "text/html",
				Bytes:       3,
			})
			return server.ResourceContent{MIMEType: "text/html", Text: "<p/>"}, nil
		}
		if err := s.AddResource(server.ResourceDef{URI: "ui://r5/n1", Name: "n1"}, read); err != nil {
			t.Fatalf("AddResource: %v", err)
		}
	}
	session := r5httpServer(t, ring, configure)

	if _, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{
		URI: "ui://r5/n1",
	}); err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	var readTrace, readSpan, appTrace, appParent string
	var sawRead, sawApp bool
	for _, ev := range ring.Recent(0) {
		switch ev.Kind {
		case obs.KindResourceRead:
			// Start and end share the span; either carries it.
			readTrace, readSpan = ev.TraceID, ev.SpanID
			sawRead = true
		case obs.KindAppLoad:
			appTrace, appParent = ev.TraceID, ev.ParentSpanID
			sawApp = true
		}
	}
	if !sawRead {
		t.Fatal("no obs/v1 resource.read event emitted")
	}
	if !sawApp {
		t.Fatal("no obs/v1 app.load event emitted from the read handler")
	}
	if appTrace != readTrace {
		t.Errorf("app.load trace id = %q, want %q (the resource.read's trace) — N1 regression",
			appTrace, readTrace)
	}
	if appParent != readSpan {
		t.Errorf("app.load parent span id = %q, want %q (the resource.read span id) — N1 regression",
			appParent, readSpan)
	}
	if appParent == "" {
		t.Error("app.load has no parent span — it was minted as an unrelated trace; N1 regression")
	}
}

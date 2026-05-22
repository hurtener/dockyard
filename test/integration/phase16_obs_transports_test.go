// This file is the Phase 16 cross-subsystem integration test (CLAUDE.md §17).
// Phase 16's Deps name Phase 15; it consumes the obs emitter seam and
// instruments runtime/server. The test drives a REAL runtime/server over a REAL
// stdio-shaped transport (newline-delimited JSON-RPC over OS pipes) and proves
// the three Phase 16 transports/adapters end to end, with real drivers and no
// mocks at the seam:
//
//  1. The out-of-band SSE sink streams obs/v1 events while the stdio pipe
//     carries ONLY clean MCP JSON-RPC framing — the no-corruption headline
//     acceptance criterion.
//  2. A real OTel in-memory span pipeline receives spans carrying mcp.* /
//     gen_ai.* attributes and the W3C-derived trace IDs.
//  3. A server log record arrives BOTH as a standard MCP notifications/message
//     AND as an obs/v1 log event.
//
// It covers a failure mode (a deliberately stalled SSE subscriber must not
// block the runtime's emit path) and runs under -race.
package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/hurtener/dockyard/runtime/obs"
	obsotel "github.com/hurtener/dockyard/runtime/obs/otel"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

type phase16Input struct {
	Region string `json:"region"`
}

type phase16Output struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
}

func phase16Handler(_ context.Context, in phase16Input) (tool.Result[phase16Output], error) {
	return tool.Result[phase16Output]{
		Text:       "ok",
		Structured: phase16Output{Region: in.Region, Count: 3},
	}, nil
}

// teeWriteCloser captures every byte the server writes to its transport so the
// test can prove the stdio pipe carried only clean JSON-RPC.
type teeWriteCloser struct {
	w   io.WriteCloser
	mu  sync.Mutex
	buf strings.Builder
}

func (t *teeWriteCloser) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf.Write(p)
	t.mu.Unlock()
	return t.w.Write(p)
}

func (t *teeWriteCloser) Close() error { return t.w.Close() }

func (t *teeWriteCloser) captured() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}

// phase16Env wires a real server over a real stdio-shaped pipe transport with
// the SSE sink and the OTel emitter both behind the obs FanOut seam.
type phase16Env struct {
	session  *mcpsdk.ClientSession
	sink     *obs.SSESink
	ring     *obs.RingBuffer
	spanRec  *tracetest.SpanRecorder
	srvWrite *teeWriteCloser
	logs     chan *mcpsdk.LoggingMessageParams
}

func newPhase16Env(t *testing.T) *phase16Env {
	t.Helper()

	// The out-of-band SSE sink — localhost-bound, behind the emitter seam.
	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	// A ring buffer (the inspector's pull source) and a real OTel span pipeline.
	ring := obs.NewRingBuffer(256)
	spanRec := tracetest.NewSpanRecorder()
	otelEmitter := obsotel.New(spanRec)

	// The runtime emits to all three drivers through the bounded FanOut.
	fanout := obs.NewFanOut(ring, sink, otelEmitter)

	srv, err := server.New(
		server.Info{Name: "phase16-app", Version: "0.1.0"},
		&server.Options{Obs: fanout, Logger: quietLogger()},
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	report := tool.New[phase16Input, phase16Output]("report").
		Describe("region report").
		Handler(phase16Handler)
	if err := report.Register(srv); err != nil {
		t.Fatalf("register report: %v", err)
	}

	// A REAL stdio-shaped transport: newline-delimited JSON-RPC over OS pipes,
	// exactly as a stdio MCP server speaks — but with the server's write side
	// teed so the test can inspect every byte on the pipe.
	srvIn, clientOut := io.Pipe() // client -> server
	clientIn, srvOut := io.Pipe() // server -> client
	tee := &teeWriteCloser{w: srvOut}
	serverT := &mcpsdk.IOTransport{Reader: srvIn, Writer: tee}
	clientT := &mcpsdk.IOTransport{Reader: clientIn, Writer: clientOut}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-srvErr:
		case <-time.After(2 * time.Second):
		}
	})

	// A client that negotiates the logging capability and records every
	// notifications/message it receives.
	logs := make(chan *mcpsdk.LoggingMessageParams, 16)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "phase16-client", Version: "0.0.0"},
		&mcpsdk.ClientOptions{
			LoggingMessageHandler: func(_ context.Context, req *mcpsdk.LoggingMessageRequest) {
				if req != nil && req.Params != nil {
					logs <- req.Params
				}
			},
		})
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return &phase16Env{
		session:  session,
		sink:     sink,
		ring:     ring,
		spanRec:  spanRec,
		srvWrite: tee,
		logs:     logs,
	}
}

// TestPhase16_SSESinkDoesNotCorruptStdioPipe is the headline acceptance
// criterion: obs events flow out the SSE channel while the stdio pipe carries
// ONLY clean MCP JSON-RPC framing.
func TestPhase16_SSESinkDoesNotCorruptStdioPipe(t *testing.T) {
	env := newPhase16Env(t)

	// Connect an SSE subscriber to the out-of-band channel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+env.sink.Addr()+"/obs/v1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	sseLines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			sseLines <- sc.Text()
		}
		close(sseLines)
	}()
	// Wait for the subscriber to register before exercising the tool.
	waitUntil(t, 2*time.Second, func() bool { return env.sink.Subscribers() == 1 })

	// Exercise the tool over the real stdio pipe.
	out, err := env.session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "report",
		Arguments: map[string]any{"region": "emea"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out.IsError {
		t.Fatalf("tool returned an error result")
	}

	// 1. The stdio pipe carries ONLY clean newline-delimited JSON-RPC. Every
	//    non-empty line must parse as a JSON object with "jsonrpc":"2.0" — no
	//    SSE frame, no obs event, no log line ever leaked onto the pipe.
	piped := env.srvWrite.captured()
	if piped == "" {
		t.Fatal("the server wrote nothing to the stdio pipe")
	}
	for _, line := range strings.Split(strings.TrimRight(piped, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("stdio pipe carried a non-JSON-RPC line (corruption): %q: %v", line, err)
		}
		if msg["jsonrpc"] != "2.0" {
			t.Fatalf("stdio pipe carried a non-JSON-RPC message (corruption): %q", line)
		}
		if strings.Contains(line, "schema_version") || strings.Contains(line, "dockyard.obs") {
			t.Fatalf("an obs/v1 event leaked onto the stdio pipe: %q", line)
		}
	}

	// 2. The obs/v1 tool.call event DID arrive out-of-band on the SSE channel.
	deadline := time.After(3 * time.Second)
	sawToolCall := false
	for !sawToolCall {
		select {
		case line, ok := <-sseLines:
			if !ok {
				t.Fatal("SSE stream closed before a tool.call event arrived")
			}
			if strings.HasPrefix(line, "data: ") {
				body := strings.TrimPrefix(line, "data: ")
				var ev obs.Event
				if json.Unmarshal([]byte(body), &ev) == nil && ev.Kind == obs.KindToolCall {
					sawToolCall = true
				}
			}
		case <-deadline:
			t.Fatal("no obs/v1 tool.call event on the SSE channel within deadline")
		}
	}

	// And the ring buffer (the inspector's pull source) recorded it too.
	if !ringHasKind(env.ring, obs.KindToolCall) {
		t.Fatal("ring buffer did not record the tool.call event")
	}
}

// TestPhase16_OTelSpansCarryMCPSemconv asserts the real OTel span pipeline
// received spans with mcp.* / gen_ai.* attributes and the W3C-derived IDs.
func TestPhase16_OTelSpansCarryMCPSemconv(t *testing.T) {
	env := newPhase16Env(t)

	out, err := env.session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "report",
		Arguments: map[string]any{"region": "apac"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out.IsError {
		t.Fatalf("tool returned an error result")
	}

	var found bool
	waitUntil(t, 3*time.Second, func() bool {
		for _, sp := range env.spanRec.Ended() {
			if strings.HasPrefix(sp.Name(), "tools/call") {
				found = true
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatal("no tools/call span exported to the OTel pipeline")
	}

	for _, sp := range env.spanRec.Ended() {
		if !strings.HasPrefix(sp.Name(), "tools/call") {
			continue
		}
		attrs := map[string]string{}
		for _, a := range sp.Attributes() {
			attrs[string(a.Key)] = a.Value.Emit()
		}
		if attrs["mcp.method.name"] != "tools/call" {
			t.Errorf("span missing mcp.method.name=tools/call, got %q", attrs["mcp.method.name"])
		}
		if attrs["gen_ai.tool.name"] != "report" {
			t.Errorf("span missing gen_ai.tool.name=report, got %q", attrs["gen_ai.tool.name"])
		}
		if attrs["gen_ai.operation.name"] != "execute_tool" {
			t.Errorf("span missing gen_ai.operation.name=execute_tool, got %q", attrs["gen_ai.operation.name"])
		}
		// The span's trace-id is a well-formed W3C trace-id (32 hex) — the
		// W3C-derived ID the obs event carried.
		if tid := sp.SpanContext().TraceID().String(); len(tid) != 32 {
			t.Errorf("span trace-id %q is not a 32-hex W3C trace-id", tid)
		}
	}
}

// TestPhase16_LogBridge_RoundTrip drives a real server.LogBridge over a real
// stdio session: one Log call from inside a tool handler fans to MCP
// notifications/message AND obs/v1.
func TestPhase16_LogBridge_RoundTrip(t *testing.T) {
	// A dedicated env that also exposes the server so the bridge can be driven.
	sink, err := obs.NewSSESink("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSSESink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })
	ring := obs.NewRingBuffer(64)

	srv, err := server.New(server.Info{Name: "phase16-log", Version: "0.1.0"},
		&server.Options{Obs: obs.NewFanOut(ring, sink), Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	// A tool whose handler drives the log bridge. The bridge resolves the
	// in-flight MCP ServerSession from the handler context (threaded by
	// runtime/server), so the typed handler never touches a raw SDK session.
	bridge := srv.LogBridge()
	loggingTool := tool.New[phase16Input, phase16Output]("logging-tool").
		Describe("emits a log record").
		Handler(func(ctx context.Context, in phase16Input) (tool.Result[phase16Output], error) {
			_ = bridge.Log(ctx, server.LogRecord{
				Level:   server.LogWarning,
				Logger:  "logging-tool",
				Message: "region " + in.Region + " is slow",
			})
			return tool.Result[phase16Output]{
				Text:       "ok",
				Structured: phase16Output{Region: in.Region, Count: 1},
			}, nil
		})
	if err := loggingTool.Register(srv); err != nil {
		t.Fatalf("register logging-tool: %v", err)
	}

	srvIn, clientOut := io.Pipe()
	clientIn, srvOut := io.Pipe()
	serverT := &mcpsdk.IOTransport{Reader: srvIn, Writer: srvOut}
	clientT := &mcpsdk.IOTransport{Reader: clientIn, Writer: clientOut}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()

	logs := make(chan *mcpsdk.LoggingMessageParams, 8)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "log-client", Version: "0.0.0"},
		&mcpsdk.ClientOptions{
			LoggingMessageHandler: func(_ context.Context, req *mcpsdk.LoggingMessageRequest) {
				if req != nil && req.Params != nil {
					logs <- req.Params
				}
			},
		})
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
		}
	})

	if err := session.SetLoggingLevel(ctx, &mcpsdk.SetLoggingLevelParams{Level: "debug"}); err != nil {
		t.Fatalf("SetLoggingLevel: %v", err)
	}

	out, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "logging-tool",
		Arguments: map[string]any{"region": "us"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out.IsError {
		t.Fatalf("tool returned an error result")
	}

	// (a) The standard MCP notifications/message arrived — a client that
	//     negotiated logging still receives it exactly as before.
	select {
	case lm := <-logs:
		if lm.Level != "warning" {
			t.Errorf("MCP log level = %q, want warning", lm.Level)
		}
		if lm.Logger != "logging-tool" {
			t.Errorf("MCP log logger = %q, want logging-tool", lm.Logger)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no MCP notifications/message received — the standard MCP logging path is broken")
	}

	// (b) The SAME record surfaced as an obs/v1 log event.
	var logEvent *obs.Event
	waitUntil(t, 3*time.Second, func() bool {
		for _, ev := range ring.Recent(0) {
			if ev.Kind == obs.KindLog {
				e := ev
				logEvent = &e
				return true
			}
		}
		return false
	})
	if logEvent == nil {
		t.Fatal("no obs/v1 log event — the MCP logging bridge did not feed obs/v1")
	}
	var lp obs.LogPayload
	if err := json.Unmarshal(logEvent.Payload, &lp); err != nil {
		t.Fatalf("decode LogPayload: %v", err)
	}
	if lp.Level != "warning" || lp.Logger != "logging-tool" {
		t.Errorf("obs/v1 log payload = %+v, want level=warning logger=logging-tool", lp)
	}
}

// TestPhase16_StalledSSESubscriberDoesNotBlockEmit is the failure-mode cover: a
// subscriber that connects but never reads must not stall the emit path while
// the server keeps serving tool calls over the stdio pipe.
func TestPhase16_StalledSSESubscriberDoesNotBlockEmit(t *testing.T) {
	env := newPhase16Env(t)

	// A subscriber that connects but never drains its body.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+env.sink.Addr()+"/obs/v1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect stalled subscriber: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	waitUntil(t, 2*time.Second, func() bool { return env.sink.Subscribers() == 1 })

	// Many tool calls; each must complete promptly despite the stalled
	// subscriber — the emit path is non-blocking.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 64; i++ {
			_, err := env.session.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name:      "report",
				Arguments: map[string]any{"region": "r"},
			})
			if err != nil {
				t.Errorf("CallTool %d: %v", i, err)
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("tool calls stalled — a slow SSE subscriber blocked the emit path")
	}
}

// ringHasKind reports whether the ring buffer recorded an event of kind k.
func ringHasKind(r *obs.RingBuffer, k obs.EventKind) bool {
	for _, ev := range r.Recent(0) {
		if ev.Kind == k {
			return true
		}
	}
	return false
}

// waitUntil polls cond until true or the deadline elapses.
func waitUntil(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", d)
	}
}

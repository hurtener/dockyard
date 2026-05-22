package server

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
)

// This file proves the Wave 6 checkpoint item S1 fix: an obs/v1 `log` event
// emitted from inside a tool handler is trace-correlated to its enclosing
// `tool.call` span — same trace id, the log event's parent span id set to the
// tool.call span id — rather than carrying an unrelated fresh trace (D-079).

type s1In struct {
	Message string `json:"message"`
}

type s1Out struct {
	Echo string `json:"echo"`
}

// TestLogBridge_HandlerLogCorrelatesToToolCall registers a tool whose handler
// emits a log record through the server's LogBridge, drives a real tools/call
// over the in-memory transport, then asserts the obs/v1 log event shares the
// tool.call's trace id and nests under its span.
func TestLogBridge_HandlerLogCorrelatesToToolCall(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(64)
	s := newLogTestServer(t, ring)

	// The handler emits a log record on its own handler context — the same
	// path a real tool handler uses. The S1 fix threads the tool.call span
	// onto that context so the bridge can correlate.
	handler := func(ctx context.Context, in s1In) (s1Out, error) {
		if err := s.LogBridge().Log(ctx, LogRecord{
			Level:   LogInfo,
			Logger:  "tool.s1probe",
			Message: "inside the handler",
		}); err != nil {
			return s1Out{}, err
		}
		return s1Out{Echo: in.Message}, nil
	}
	if err := AddTool(s, ToolDef{Name: "s1probe"}, handler); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clientT := s.ServeInMemory(ctx)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "s1-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "s1probe",
		Arguments: s1In{Message: "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool returned IsError: %+v", res.Content)
	}

	// Collect the tool.call span and the handler-emitted log event.
	var toolCallTrace, toolCallSpan, logTrace, logParent string
	var sawToolCall, sawLog bool
	for _, ev := range ring.Recent(0) {
		switch ev.Kind {
		case obs.KindToolCall:
			// The start and end events share the span; either carries it.
			toolCallTrace, toolCallSpan = ev.TraceID, ev.SpanID
			sawToolCall = true
		case obs.KindLog:
			logTrace, logParent = ev.TraceID, ev.ParentSpanID
			sawLog = true
		}
	}
	if !sawToolCall {
		t.Fatal("no obs/v1 tool.call event emitted")
	}
	if !sawLog {
		t.Fatal("no obs/v1 log event emitted from the handler")
	}

	// The S1 assertion: same trace, log nests under the tool.call span.
	if logTrace != toolCallTrace {
		t.Errorf("handler log event trace id = %q, want %q (the tool.call's trace) — S1 regression",
			logTrace, toolCallTrace)
	}
	if logParent != toolCallSpan {
		t.Errorf("handler log event parent span id = %q, want %q (the tool.call span id) — S1 regression",
			logParent, toolCallSpan)
	}
	if logParent == "" {
		t.Error("handler log event has no parent span — it is not correlated to the tool.call")
	}
}

// TestLogBridge_OutOfRequestLogStartsFreshTrace proves the other half of the
// S1 fix: a log record emitted outside any tool handler (no enclosing span on
// the context) still gets a well-formed fresh root trace, not an empty one.
func TestLogBridge_OutOfRequestLogStartsFreshTrace(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(8)
	s := newLogTestServer(t, ring)

	if err := s.LogBridge().Log(context.Background(), LogRecord{
		Level:   LogNotice,
		Message: "out of request",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	events := ring.Recent(0)
	if len(events) != 1 || events[0].Kind != obs.KindLog {
		t.Fatalf("want exactly one log event, got %d", len(events))
	}
	ev := events[0]
	if ev.TraceID == "" || ev.SpanID == "" {
		t.Error("an out-of-request log event must still carry a fresh trace identity")
	}
	if ev.ParentSpanID != "" {
		t.Errorf("an out-of-request log event must be a root span, got parent %q", ev.ParentSpanID)
	}
}

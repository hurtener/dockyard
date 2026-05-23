package server

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
)

// This file implements the MCP logging → obs/v1 log-event bridge (RFC §11.3,
// Phase 16). A Dockyard server still speaks STANDARD MCP logging to any client:
// a log record is sent as an MCP notifications/message exactly as the spec and
// the go-sdk define it. The bridge ADDITIONALLY surfaces the same record as an
// obs/v1 log event through the server's obs.Recorder — obs/v1 is a one-way
// event stream, never a back channel (P2, CLAUDE.md §6, §8). The bridge is an
// event SOURCE; it does not replace or intercept MCP logging.

// LogLevel is the RFC 5424 severity of a server log record — the level set
// shared by MCP logging and the obs/v1 log event. It is a Dockyard-owned type
// so the server-facing API never exposes a raw SDK type (RFC §5.4, P3).
type LogLevel string

// The RFC 5424 severities MCP logging uses (see the MCP logging capability and
// the go-sdk LoggingLevel set).
const (
	LogDebug     LogLevel = "debug"
	LogInfo      LogLevel = "info"
	LogNotice    LogLevel = "notice"
	LogWarning   LogLevel = "warning"
	LogError     LogLevel = "error"
	LogCritical  LogLevel = "critical"
	LogAlert     LogLevel = "alert"
	LogEmergency LogLevel = "emergency"
)

// validLogLevel reports whether l is a known RFC 5424 severity. An unknown
// level is normalised to LogInfo by the bridge rather than rejected —
// observability never fails a request (P2).
func (l LogLevel) valid() bool {
	switch l {
	case LogDebug, LogInfo, LogNotice, LogWarning, LogError,
		LogCritical, LogAlert, LogEmergency:
		return true
	default:
		return false
	}
}

// LogRecord is one server log record. It is the bridge's input: the same
// record is delivered to a client as an MCP notifications/message AND emitted
// as an obs/v1 log event.
type LogRecord struct {
	// Level is the RFC 5424 severity. An empty or unknown level is treated as
	// LogInfo.
	Level LogLevel
	// Logger is the optional logger name carried on both the MCP record and the
	// obs/v1 log event.
	Logger string
	// Message is the log message.
	Message string
	// Data is optional structured detail attached to the MCP
	// notifications/message `data` field. The obs/v1 log event carries the
	// message and level only (LogPayload) — structured data capture stays
	// shape-bounded and is out of obs/v1 log-event scope.
	Data any
}

// sessionKey is the context key under which a tool handler's in-flight MCP
// ServerSession is threaded so [LogBridge.Log] can reach it without the typed
// handler signature exposing a raw SDK session (P3, RFC §5.4).
type sessionKey struct{}

// withRequestSession returns a copy of ctx carrying the request's MCP
// ServerSession AND its obs/v1 session id. runtime/server calls it at the
// tool-handler edge so the logging bridge can reach the raw SDK session, and
// so every obs/v1 event emitted from inside the handler carries the in-flight
// SessionID (R5 — depth-audit remediation; D-120). The two threadings share
// one helper so the tool and resource edges cannot drift.
func withRequestSession(ctx context.Context, req *mcpsdk.CallToolRequest) context.Context {
	if req == nil || req.Session == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, sessionKey{}, req.Session)
	return obs.WithSession(ctx, req.Session.ID())
}

// withResourceRequestSession is the resources/read counterpart of
// [withRequestSession]: it stamps obs.WithSession from the read request's
// session id so an obs/v1 resource.read (or an app.load minted inside it)
// carries SessionID. The ServerSession itself is not threaded onto ctx for a
// resource request — there is no resource-side logging bridge yet — only the
// session id needed by the obs emit sites.
func withResourceRequestSession(ctx context.Context, req *mcpsdk.ReadResourceRequest) context.Context {
	if req == nil || req.Session == nil {
		return ctx
	}
	return obs.WithSession(ctx, req.Session.ID())
}

// sessionFromContext extracts the in-flight MCP ServerSession threaded by
// [withRequestSession], or nil when ctx is not a tool-handler context.
func sessionFromContext(ctx context.Context) *mcpsdk.ServerSession {
	s, _ := ctx.Value(sessionKey{}).(*mcpsdk.ServerSession)
	return s
}

// LogBridge bridges the MCP logging capability into obs/v1. A Dockyard server
// owns exactly one; obtain it via [Server.LogBridge]. It is a reusable
// concurrent artifact — Log is safe from many goroutines (the obs.Recorder and
// the SDK ServerSession both are).
type LogBridge struct {
	rec *obs.Recorder
}

// LogBridge returns the server's MCP-logging → obs/v1 bridge. It is never nil;
// a server constructed without an obs emitter still returns a usable bridge
// whose obs side discards events (the Recorder is a NopEmitter) while the MCP
// notifications/message side is unaffected.
func (s *Server) LogBridge() *LogBridge {
	return &LogBridge{rec: s.rec}
}

// Log delivers rec to a client as a standard MCP notifications/message AND
// emits it as an obs/v1 log event. It is the bridge's primary entry point: a
// tool handler calls it with its handler context, and the bridge resolves the
// in-flight MCP ServerSession from that context (threaded by runtime/server) —
// the typed handler never touches a raw SDK session (P3).
//
// When ctx carries no session (a record logged outside a request) the MCP
// notifications/message delivery is skipped but the obs/v1 log event is still
// emitted, so an out-of-request record is still observable.
//
// The standard MCP path is unchanged: ServerSession.Log applies the client's
// negotiated minimum level exactly as before — a client that called SetLevel
// receives notifications/message per spec, a client that did not receives
// nothing on the MCP channel. The obs/v1 log event is independent of the
// client's MCP log level: the inspector sees every server log record.
//
// Any MCP delivery error is returned so a caller can react, but it does NOT
// suppress the obs/v1 log event — observability never fails a request (P2).
func (b *LogBridge) Log(ctx context.Context, rec LogRecord) error {
	return b.LogTo(ctx, sessionFromContext(ctx), rec)
}

// LogTo is [LogBridge.Log] with an explicitly supplied MCP ServerSession,
// rather than one resolved from the context. It is the lower-level entry point
// for a caller that already holds a session (e.g. a non-tool subsystem). A nil
// sess skips the MCP notifications/message delivery; the obs/v1 log event is
// still emitted.
func (b *LogBridge) LogTo(ctx context.Context, sess *mcpsdk.ServerSession, rec LogRecord) error {
	level := rec.Level
	if !level.valid() {
		level = LogInfo
	}

	// 1. obs/v1 log event — the new event source. Independent of the client's
	//    MCP log level so the inspector observes every record.
	//
	//    The log event correlates to its enclosing unit of work: when ctx
	//    carries an in-flight span (a tool handler's tool.call span, threaded
	//    by runtime/server via obs.WithSpan), the log event is emitted as a
	//    CHILD of that span — same trace id, parent span id set — so a
	//    handler-emitted log record is trace-correlated to its tool.call
	//    (Wave 6 checkpoint S1; D-079). Outside a request — a record logged
	//    with no enclosing span — ChildOrNewTrace begins a fresh root trace.
	b.rec.Log(ctx, obs.ChildOrNewTrace(ctx), obs.LogPayload{
		Level:   string(level),
		Logger:  rec.Logger,
		Message: rec.Message,
	})

	// 2. Standard MCP notifications/message — unchanged spec behaviour. Skipped
	//    only when there is no client session to notify.
	if sess == nil {
		return nil
	}
	data := rec.Data
	if data == nil {
		data = rec.Message
	}
	return sess.Log(ctx, &mcpsdk.LoggingMessageParams{
		Level:  mcpsdk.LoggingLevel(level),
		Logger: rec.Logger,
		Data:   data,
	})
}

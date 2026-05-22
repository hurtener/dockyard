// Package otel is the optional OpenTelemetry export adapter for obs/v1 — the
// OTelEmitter (RFC §11.3, brief 05 §3.4).
//
// It is a driver behind the obs emitter seam (CLAUDE.md §4.4): it registers
// itself under the driver name "otel" via obs.RegisterDriver, exactly like the
// ring-buffer and SSE drivers. It is OFF BY DEFAULT — local observation (the
// ring buffer + the SSE sink) works with zero OTel configuration; OTel is an
// interoperability option, never a prerequisite to observe locally (CLAUDE.md
// §8). The adapter is constructed only when a caller explicitly opens the
// driver or calls New.
//
// # Why a separate package
//
// OTel is an EXPORT ADAPTER behind obs/v1, never the internal model (brief 05
// §4 risk 1). obs/v1 is Dockyard's stable, versioned contract; the OTel
// dependency and the still-"Development" MCP semantic conventions are contained
// here so an attribute-name shift is a localized edit. runtime/obs itself takes
// no OTel dependency.
//
// # The mapping
//
// An obs.Event lowers onto an OpenTelemetry span carrying MCP semantic-
// convention attributes (mcp.* / gen_ai.*): a tool.call becomes a span
// "tools/call {tool}" with mcp.method.name, gen_ai.tool.name,
// gen_ai.operation.name=execute_tool, mcp.session.id, network.transport, and —
// on failure — error.type. The W3C Trace Context IDs obs/v1 already puts on
// every event (obs.SpanContext) become the OTel span's trace-id and span-id, so
// a Dockyard span nests natively under a calling Harbor agent's execute_tool
// span. A log event is exported as a span event on the correlated span
// (D-076).
package otel

import (
	"context"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/hurtener/dockyard/runtime/obs"
)

// driverName is the registered obs-emitter driver name of the OTel adapter.
const driverName = "otel"

// MCP semantic-convention attribute keys (OpenTelemetry semconv — the MCP page,
// brief 05 §3.4). Centralised so a semconv revision is a one-file edit.
const (
	attrMCPMethodName         = "mcp.method.name"
	attrMCPSessionID          = "mcp.session.id"
	attrMCPResourceURI        = "mcp.resource.uri"
	attrGenAIToolName         = "gen_ai.tool.name"
	attrGenAIOperationName    = "gen_ai.operation.name"
	attrNetworkTransport      = "network.transport"
	attrErrorType             = "error.type"
	attrDockyardEventKind     = "dockyard.obs.event.kind"
	attrDockyardServerID      = "dockyard.obs.server.id"
	attrDockyardSchemaVer     = "dockyard.obs.schema_version"
	attrDockyardContractOK    = "dockyard.obs.contract_ok"
	attrDockyardResourceMIME  = "dockyard.obs.resource.mime"
	attrDockyardResourceBytes = "dockyard.obs.resource.bytes"
)

// genAIOperationExecuteTool is the OTel gen_ai.operation.name value for an MCP
// tool call — a Dockyard tool.call span merges into a parent GenAI execute_tool
// span when an outer agent is already tracing (brief 05 §3.4).
const genAIOperationExecuteTool = "execute_tool"

// instrumentationName is the OTel instrumentation scope of the adapter.
const instrumentationName = "github.com/hurtener/dockyard/runtime/obs/otel"

// init registers the OTel adapter behind the obs emitter seam. The driver is
// inert until Open is called: the factory returns a NopEmitter when no tracer
// provider is wired (the config is empty), so merely importing this package —
// or having it registered — never starts OTel. OTel is off by default
// (CLAUDE.md §8). To activate it a caller passes a configured TracerProvider to
// [New] (the CLI knob that does so is Wave 7 scope).
func init() {
	obs.RegisterDriver(driverName, func(_ string) (obs.Emitter, error) {
		// The seam's string config cannot carry a live TracerProvider. Opening
		// the driver by name therefore yields the explicitly-off NopEmitter;
		// activation is via New with a caller-supplied provider. This keeps
		// "off by default" true even when the driver is registered.
		return obs.NopEmitter{}, nil
	})
}

// OTelEmitter is the optional OpenTelemetry export adapter for obs/v1. It is an
// obs.Emitter: the runtime emits obs.Events to it through the same seam as any
// other driver, and it lowers each event onto an OTel span.
//
// OTelEmitter is a reusable concurrent artifact — Emit is safe from many
// goroutines, because the underlying OTel Tracer is. Emit is non-blocking from
// the runtime's point of view: span export is handed to the span processor,
// which the OTel SDK runs asynchronously (a batching processor) — the runtime
// is never stalled on an OTel exporter (CLAUDE.md §8).
//
// The emitter owns an internal TracerProvider built over the caller-supplied
// span processor(s) and a context-keyed IDGenerator (idGen). The IDGenerator is
// what makes a Dockyard span carry the obs/v1 event's OWN W3C trace-id and
// span-id rather than fresh OTel-generated IDs: a Dockyard span therefore nests
// natively under a calling Harbor agent's execute_tool span (RFC §11.2).
//
// The otel.OTelEmitter "stutter" is intentional: OTelEmitter is the RFC
// §11.3-binding name for this adapter — it appears verbatim in the RFC and the
// master plan — so it is the intended public vocabulary, not an accident.
//
//nolint:revive // OTelEmitter is the RFC §11.3-binding public name (see above).
type OTelEmitter struct {
	tracer oteltrace.Tracer
	idGen  *idGenerator
}

// New constructs an OTelEmitter that exports spans to the caller-supplied
// OpenTelemetry span processors (e.g. a batching processor over an OTLP
// exporter, or an in-memory recorder under test). With NO processors the
// emitter discards every event — OTel stays off unless a real export pipeline
// is supplied (CLAUDE.md §8), so a caller can wire New unconditionally and only
// a configured pipeline activates export.
func New(processors ...sdktrace.SpanProcessor) *OTelEmitter {
	live := false
	for _, p := range processors {
		if p != nil {
			live = true
		}
	}
	if !live {
		return &OTelEmitter{}
	}
	idGen := newIDGenerator()
	opts := []sdktrace.TracerProviderOption{sdktrace.WithIDGenerator(idGen)}
	for _, p := range processors {
		if p != nil {
			opts = append(opts, sdktrace.WithSpanProcessor(p))
		}
	}
	tp := sdktrace.NewTracerProvider(opts...)
	return &OTelEmitter{
		tracer: tp.Tracer(instrumentationName),
		idGen:  idGen,
	}
}

// Emit lowers e onto an OpenTelemetry span. A nil tracer (OTel off) discards
// the event. The span carries the MCP semantic-convention attributes for e's
// kind and the W3C Trace Context IDs obs/v1 already assigned, so the exported
// span shares e's trace and span identity.
func (o *OTelEmitter) Emit(ctx context.Context, e obs.Event) {
	if o == nil || o.tracer == nil {
		return
	}
	// A log event has no lifecycle of its own — it is exported as a span event
	// on a correlated span (D-076).
	if e.Kind == obs.KindLog {
		o.emitLogEvent(ctx, e)
		return
	}
	// Only an end/emit event closes a unit of work with a known duration; a
	// start event is the open half of a pair. Exporting on the end event keeps
	// one OTel span per obs unit of work, with the correct duration.
	if e.Phase == obs.PhaseStart {
		return
	}

	ids, ok := obsIDs(e)
	if !ok {
		return
	}
	startCtx := o.idGen.withIDs(ctx, ids)
	_, span := o.tracer.Start(startCtx, spanName(e),
		oteltrace.WithSpanKind(oteltrace.SpanKindServer),
	)
	span.SetAttributes(attributesFor(e)...)
	if e.Error != nil {
		span.SetStatus(codes.Error, e.Error.Message)
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// emitLogEvent exports an obs/v1 log event as a span event on a one-shot span
// correlated to the log record's trace.
func (o *OTelEmitter) emitLogEvent(ctx context.Context, e obs.Event) {
	ids, ok := obsIDs(e)
	if !ok {
		return
	}
	startCtx := o.idGen.withIDs(ctx, ids)
	_, span := o.tracer.Start(startCtx, spanName(e),
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
	)
	attrs := []attribute.KeyValue{
		attribute.String(attrDockyardEventKind, string(e.Kind)),
		attribute.String(attrDockyardServerID, e.ServerID),
	}
	if lp, perr := decodeLogPayload(e); perr == nil {
		attrs = append(attrs,
			attribute.String("log.level", lp.Level),
			attribute.String("log.message", lp.Message),
		)
		if lp.Logger != "" {
			attrs = append(attrs, attribute.String("log.logger", lp.Logger))
		}
	}
	span.AddEvent("log", oteltrace.WithAttributes(attrs...))
	span.End()
}

// obsIDs parses an obs.Event's W3C trace-id and span-id into OTel ID types so
// the exported span carries the event's own identity. It reports false if the
// event's IDs are not well-formed W3C IDs.
func obsIDs(e obs.Event) (otelIDs, bool) {
	tid, err := oteltrace.TraceIDFromHex(e.TraceID)
	if err != nil {
		return otelIDs{}, false
	}
	sid, err := oteltrace.SpanIDFromHex(e.SpanID)
	if err != nil {
		return otelIDs{}, false
	}
	return otelIDs{trace: tid, span: sid}, true
}

// spanName is the OTel span name for an event — "{mcp.method.name} {target}"
// per the MCP semconv span-naming rule (brief 05 §3.4), or the event kind when
// there is no method/target.
func spanName(e obs.Event) string {
	switch e.Kind {
	case obs.KindToolCall:
		if p, err := decodeToolPayload(e); err == nil && p.Tool != "" {
			return "tools/call " + p.Tool
		}
		return "tools/call"
	case obs.KindResourceRead:
		if p, err := decodeResourcePayload(e); err == nil && p.URI != "" {
			return "resources/read " + p.URI
		}
		return "resources/read"
	case obs.KindPromptGet:
		return "prompts/get"
	case obs.KindLog:
		return "log"
	default:
		return string(e.Kind)
	}
}

// attributesFor builds the MCP semantic-convention attribute set for e.
func attributesFor(e obs.Event) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(attrDockyardSchemaVer, e.SchemaVersion),
		attribute.String(attrDockyardEventKind, string(e.Kind)),
		attribute.String(attrDockyardServerID, e.ServerID),
	}
	if e.SessionID != "" {
		attrs = append(attrs, attribute.String(attrMCPSessionID, e.SessionID))
	}
	if e.Error != nil && e.Error.Type != "" {
		attrs = append(attrs, attribute.String(attrErrorType, e.Error.Type))
	}

	switch e.Kind {
	case obs.KindToolCall:
		attrs = append(attrs,
			attribute.String(attrMCPMethodName, "tools/call"),
			attribute.String(attrGenAIOperationName, genAIOperationExecuteTool),
		)
		if p, err := decodeToolPayload(e); err == nil {
			if p.Tool != "" {
				attrs = append(attrs, attribute.String(attrGenAIToolName, p.Tool))
			}
			if p.Transport != "" {
				attrs = append(attrs, attribute.String(attrNetworkTransport, p.Transport))
			}
			if p.ContractOK != nil {
				attrs = append(attrs, attribute.Bool(attrDockyardContractOK, *p.ContractOK))
			}
		}
	case obs.KindResourceRead:
		attrs = append(attrs, attribute.String(attrMCPMethodName, "resources/read"))
		if p, err := decodeResourcePayload(e); err == nil {
			if p.URI != "" {
				attrs = append(attrs, attribute.String(attrMCPResourceURI, p.URI))
			}
			if p.MIME != "" {
				attrs = append(attrs, attribute.String(attrDockyardResourceMIME, p.MIME))
			}
			attrs = append(attrs, attribute.Int(attrDockyardResourceBytes, p.Bytes))
		}
	case obs.KindPromptGet:
		attrs = append(attrs, attribute.String(attrMCPMethodName, "prompts/get"))
	}

	if e.DurationMS != nil {
		attrs = append(attrs, attribute.String("dockyard.obs.duration_ms",
			strconv.FormatInt(*e.DurationMS, 10)))
	}
	return attrs
}

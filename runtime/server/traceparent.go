package server

import (
	"net/http"
	"strings"

	"github.com/hurtener/dockyard/runtime/obs"
)

// This file implements the W3C TraceContext extractor on the streamable-HTTP
// transport boundary (R5 — depth-audit remediation; D-122). A request whose
// `traceparent` header is well-formed has its trace identity threaded onto
// the request context via obs.WithInboundTrace, so a handler-edge call site
// using obs.NewTraceFromContext (in place of obs.NewTrace) inherits the
// caller's trace — a Dockyard server's spans nest natively under a calling
// Harbor agent's `execute_tool` span, satisfying the RFC §11.2 claim that
// the OTel doc comment had been making in advance of the wiring.
//
// W3C TraceContext propagation
// (https://www.w3.org/TR/trace-context/) is a textual header set: this code
// parses `traceparent` only — version `00`, format
// `00-{32 hex trace-id}-{16 hex span-id}-{2 hex flags}`. `tracestate` is the
// W3C vendor-specific extension carrier; the obs/v1 SpanContext does not
// carry a tracestate field, so the V1 propagator preserves traceparent
// linkage only — the parent's TraceID and SpanID. A future obs/v1 schema
// revision that adds tracestate is a versioned change (CLAUDE.md §8).
//
// The implementation does NOT depend on go.opentelemetry.io/otel — keeping
// `runtime/server` OTel-free is part of the §6 / §10 isolation: an OTel
// dependency at the transport boundary would leak the optional adapter into
// the always-on server core. The format is small enough to parse directly,
// and `runtime/obs/otel`'s `obsIDs` does the inverse for the OTel export
// half.

// traceparentHeader is the canonical W3C TraceContext request header. HTTP
// header lookup is case-insensitive, so this is the only spelling we need.
const traceparentHeader = "Traceparent"

// traceparentMiddleware extracts a W3C `traceparent` from an incoming HTTP
// request and stamps the parsed SpanContext onto the request context via
// obs.WithInboundTrace. A request that carries no traceparent — or an
// unparseable one — passes through untouched; observability never fails a
// request (P2).
//
// The middleware sits outside the SDK handler chain so the parent context
// reaches every handler the SDK dispatches into. It is wired by
// HTTPHandler and runs once per HTTP request, before the optional Tasks
// mount and before the CrossOriginProtection / Content-Type checks have
// produced their verdicts — extraction is purely read-only.
func traceparentMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hdr := r.Header.Get(traceparentHeader); hdr != "" {
			if sc, ok := parseTraceparent(hdr); ok {
				r = r.WithContext(obs.WithInboundTrace(r.Context(), sc))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// parseTraceparent parses a W3C TraceContext `traceparent` header value into
// an obs.SpanContext. Only version `00` is recognised; a future version (the
// W3C spec allows forward compatibility) is treated as no parent rather than
// guessed — version-misinterpretation would be worse than no inheritance.
// Returns ok=false on any parse failure.
func parseTraceparent(v string) (obs.SpanContext, bool) {
	v = strings.TrimSpace(v)
	// Expected: 2 hex (version) + "-" + 32 hex (trace) + "-" + 16 hex (span)
	// + "-" + 2 hex (flags) = 55 chars. A version-`00` traceparent has no
	// optional fields after flags.
	parts := strings.Split(v, "-")
	if len(parts) != 4 {
		return obs.SpanContext{}, false
	}
	version, traceID, spanID, flags := parts[0], parts[1], parts[2], parts[3]
	if version != "00" {
		// Forward versions reserved; do not guess.
		return obs.SpanContext{}, false
	}
	if !isLowerHex(traceID, 32) || !isLowerHex(spanID, 16) || !isLowerHex(flags, 2) {
		return obs.SpanContext{}, false
	}
	// All-zero trace-id and all-zero span-id are invalid per the W3C spec.
	if isAllZero(traceID) || isAllZero(spanID) {
		return obs.SpanContext{}, false
	}
	// The inbound carrier: TraceID + SpanID. The handler's child span is
	// minted by NewTraceFromContext.Child, which sets ParentID = spanID and
	// mints a fresh own SpanID.
	return obs.SpanContext{TraceID: traceID, SpanID: spanID}, true
}

// isLowerHex reports whether s is exactly n lowercase-hex characters.
func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// isAllZero reports whether s is composed entirely of '0' characters.
func isAllZero(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

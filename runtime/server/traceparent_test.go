package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hurtener/dockyard/runtime/obs"
)

// TestParseTraceparent_Valid covers the parser's accept path: a well-formed
// W3C TraceContext version-`00` traceparent header parses into a SpanContext
// carrying the inbound trace-id and span-id (R5/N2 — D-122).
func TestParseTraceparent_Valid(t *testing.T) {
	t.Parallel()
	// W3C TR/trace-context: an example sampled traceparent.
	hdr := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	sc, ok := parseTraceparent(hdr)
	if !ok {
		t.Fatalf("parseTraceparent(%q) = ok=false, want a parsed SpanContext", hdr)
	}
	if sc.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("TraceID = %q, want 0af7651916cd43dd8448eb211c80319c", sc.TraceID)
	}
	if sc.SpanID != "b7ad6b7169203331" {
		t.Errorf("SpanID = %q, want b7ad6b7169203331", sc.SpanID)
	}
	if sc.ParentID != "" {
		t.Errorf("ParentID = %q, want empty (the inbound carrier is parent, "+
			"not the local unit of work)", sc.ParentID)
	}
}

// TestParseTraceparent_Invalid covers the parser's reject path: an unparseable
// or version-non-`00` traceparent is rejected with ok=false rather than guessed.
// Observability never fails a request (P2); a rejected traceparent yields the
// "no parent" fallback.
func TestParseTraceparent_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		hdr  string
	}{
		{"empty", ""},
		{"missing fields", "00-aabbccdd"},
		{"unknown version", "01-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		{"future version", "ff-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		{"short trace-id", "00-0af7651916cd43dd-b7ad6b7169203331-01"},
		{"short span-id", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b716-01"},
		{"uppercase hex", "00-0AF7651916CD43DD8448EB211C80319C-B7AD6B7169203331-01"},
		{"non-hex char", "00-0af7651916cd43dd8448eb211c80319z-b7ad6b7169203331-01"},
		{"all-zero trace", "00-00000000000000000000000000000000-b7ad6b7169203331-01"},
		{"all-zero span", "00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseTraceparent(tc.hdr); ok {
				t.Errorf("parseTraceparent(%q) = ok=true, want false", tc.hdr)
			}
		})
	}
}

// TestTraceparentMiddleware_StampsInboundTrace proves the HTTP middleware
// stamps the parsed parent SpanContext onto the request context via
// obs.WithInboundTrace, so a handler downstream of the middleware can
// inherit the calling agent's trace via obs.NewTraceFromContext (R5; D-122).
func TestTraceparentMiddleware_StampsInboundTrace(t *testing.T) {
	t.Parallel()
	var captured obs.SpanContext
	var capturedOK bool
	var derived obs.SpanContext
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// The middleware wraps r with a new context; only r.Context() inside
		// the inner handler reflects the stamped value.
		captured, capturedOK = obs.InboundTraceFromContext(r.Context())
		derived = obs.NewTraceFromContext(r.Context())
	})
	h := traceparentMiddleware(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://localhost/", nil)
	req.Header.Set("Traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	h.ServeHTTP(rec, req)

	if !capturedOK {
		t.Fatal("InboundTraceFromContext: ok=false — traceparentMiddleware did not stamp the parent")
	}
	if captured.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("captured TraceID = %q, want 0af7651916cd43dd8448eb211c80319c", captured.TraceID)
	}
	if captured.SpanID != "b7ad6b7169203331" {
		t.Errorf("captured SpanID = %q, want b7ad6b7169203331", captured.SpanID)
	}

	if derived.TraceID != captured.TraceID {
		t.Errorf("NewTraceFromContext.TraceID = %q, want %q (the inbound parent's)",
			derived.TraceID, captured.TraceID)
	}
	if derived.ParentID != captured.SpanID {
		t.Errorf("NewTraceFromContext.ParentID = %q, want %q (the inbound span id)",
			derived.ParentID, captured.SpanID)
	}
	if derived.SpanID == captured.SpanID {
		t.Error("NewTraceFromContext.SpanID == parent SpanID — must be a fresh own span id")
	}
}

// TestTraceparentMiddleware_NoHeaderPassesThrough proves a request with no
// `traceparent` header reaches the inner handler with no inbound trace —
// observability never fails a request (P2). The handler then mints a fresh
// root via NewTraceFromContext's fallback.
func TestTraceparentMiddleware_NoHeaderPassesThrough(t *testing.T) {
	t.Parallel()
	var capturedOK bool
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, capturedOK = obs.InboundTraceFromContext(r.Context())
	})
	h := traceparentMiddleware(inner)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://localhost/", nil)
	h.ServeHTTP(rec, req)

	if capturedOK {
		t.Error("InboundTraceFromContext: ok=true on a request with no Traceparent header")
	}
	// And the fallback yields a fresh root span.
	sc := obs.NewTraceFromContext(req.Context())
	if sc.TraceID == "" || sc.SpanID == "" {
		t.Error("NewTraceFromContext: empty trace identity on a no-traceparent request")
	}
	if sc.ParentID != "" {
		t.Errorf("NewTraceFromContext.ParentID = %q, want empty (a root span)", sc.ParentID)
	}
}

// TestTraceparentMiddleware_MalformedPassesThrough proves a malformed
// traceparent is rejected as "no parent" rather than dropped or guessed — the
// inner handler still runs.
func TestTraceparentMiddleware_MalformedPassesThrough(t *testing.T) {
	t.Parallel()
	called := false
	var capturedOK bool
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		_, capturedOK = obs.InboundTraceFromContext(r.Context())
	})
	h := traceparentMiddleware(inner)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://localhost/", nil)
	req.Header.Set("Traceparent", "not a valid traceparent")
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("inner handler not called — middleware dropped a malformed-traceparent request")
	}
	if capturedOK {
		t.Error("InboundTraceFromContext: ok=true on a malformed traceparent")
	}
}

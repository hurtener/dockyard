package inspector

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// This file holds the Phase 27 fuzz targets for the inspector's HTTP endpoints
// — the routes the inspector exposes on its loopback listener. The inspector
// is dev-mode-gated and localhost-only, but an operator could still feed it
// adversarial bytes (e.g. via a misconfigured curl, or an in-process driver
// in a test); every endpoint must reject malformed input cleanly without
// panicking.
//
// The invariant under fuzz is uniform: a request through the inspector's mux
// NEVER causes a panic regardless of method, path, headers, or body content.
// A typed status (200 / 400 / 404 / 405 / 502 / 503) is acceptable; only a
// panic is a fuzz failure.
//
// CI runs the seed corpus as ordinary tests. For a longer local session:
//
//	go test ./internal/inspector -run '^$' -fuzz FuzzInspectorMux -fuzztime 60s

// FuzzInspectorMux fuzzes the inspector HTTP mux against every endpoint.
// The fuzz input is the raw HTTP request body (and the mux is exercised
// against POST /api/tools/invoke and POST /api/tasks/elicitation, the two
// body-consuming endpoints; the read-only GETs are exercised with a fixed
// path enumeration).
func FuzzInspectorMux(f *testing.F) {
	// Seed corpus for the body shape — valid + adversarial JSON for the
	// two mutating endpoints.
	f.Add([]byte(`{"tool":"echo","arguments":{"in":"x"}}`))
	f.Add([]byte(`{"taskId":"task-x","data":{"y":1}}`))
	f.Add([]byte(`{"taskId":"task-x","declined":true}`))
	f.Add([]byte(`{"tool":""}`))
	f.Add([]byte(`{"taskId":""}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"tool":"echo","arguments":{"in":"x"},"unknown":42}`)) // DisallowUnknownFields trips
	f.Add([]byte(`{"taskId":"task-x","unknown":42}`))                    // DisallowUnknownFields trips
	// Hostile-large body (1 MB) — the mux must reject cleanly without
	// allocating without bound.
	big := bytes.Repeat([]byte(`{"a":1},`), 32768)
	big = append(big, '{', '}')
	f.Add(big)
	// Embedded NUL + binary noise.
	f.Add(append([]byte{0x00, 0x01, 0x02}, []byte(`{"tool":"x"}`)...))

	// The mutating endpoints — Invoker and Elicitor are stubbed to return
	// success so the path through the handler is exercised, not blocked at
	// the 503-detached gate. The fuzz invariant is panic-freedom; whether
	// the stub returns success or error is incidental.
	opts := Options{
		Logger: slog.New(slog.DiscardHandler),
		Invoker: func(_ context.Context, _ InvokeRequest) (*InvokeResponse, error) {
			return &InvokeResponse{}, nil
		},
		Elicitor: func(_ context.Context, _ ElicitationRequest) (*ElicitationResponse, error) {
			return &ElicitationResponse{Delivered: true}, nil
		},
	}
	mux := newMux(opts, slog.New(slog.DiscardHandler))

	endpoints := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/info"},
		{http.MethodGet, "/api/verdicts"},
		{http.MethodGet, "/api/contracts"},
		{http.MethodGet, "/api/apps"},
		{http.MethodGet, "/api/fixtures"},
		{http.MethodGet, "/api/obs/stream"},
		{http.MethodGet, "/api/rpc/log"},
		{http.MethodPost, "/api/tools/invoke"},
		{http.MethodPost, "/api/tasks/elicitation"},
		// Unknown route — must hit the SPA fallback or 404 cleanly.
		{http.MethodGet, "/api/unknown"},
		{http.MethodPost, "/api/unknown"},
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		for _, ep := range endpoints {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("inspector mux panic on %s %s body=%q: %v",
							ep.method, ep.path, truncateBody(body, 64), r)
					}
				}()
				req := httptest.NewRequest(ep.method, ep.path, bytes.NewReader(body))
				req.Host = "127.0.0.1"
				if ep.method == http.MethodPost {
					req.Header.Set("Content-Type", "application/json")
				}
				rec := httptest.NewRecorder()
				// Bound the obs/rpc SSE handlers — they block until the client
				// disconnects. The recorder drains synchronously, but the SSE
				// handler watches the request context; a cancelled context
				// returns promptly. We achieve that by cancelling immediately.
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
				mux.ServeHTTP(rec, req)
				// Discard the body — the invariant is panic-freedom, not a
				// specific response shape.
				_, _ = io.Copy(io.Discard, rec.Result().Body)
				_ = rec.Result().Body.Close()
			}()
		}
	})
}

// truncateBody clips a fuzz-input body for the error message — avoids
// dumping a multi-kB body into the test log on failure.
func truncateBody(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

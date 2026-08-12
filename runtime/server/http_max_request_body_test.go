package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/server"
)

// bigInitialize returns a syntactically valid JSON-RPC initialize request whose
// serialized length is at least size bytes. The padding rides in
// clientInfo.name — a field the SDK decodes into a plain string — so the body
// remains a valid initialize even at multi-MiB sizes.
func bigInitialize(t *testing.T, size int) string {
	t.Helper()
	const prefix = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"`
	const suffix = `","version":"1"}}}`
	pad := size - len(prefix) - len(suffix)
	if pad < 0 {
		pad = 0
	}
	return prefix + strings.Repeat("a", pad) + suffix
}

// bigDiscover returns a syntactically valid modern 2026-07-28 server/discover
// request of at least size bytes, padded via the _meta clientInfo name.
func bigDiscover(t *testing.T, size int) string {
	t.Helper()
	const prefix = `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"`
	const suffix = `","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	pad := size - len(prefix) - len(suffix)
	if pad < 0 {
		pad = 0
	}
	return prefix + strings.Repeat("a", pad) + suffix
}

// TestHTTPHandlerMaxRequestBodyBytesZeroPreservesDefaultReject proves the zero
// option keeps the pre-existing 4 MiB bound (D-204): a >4 MiB body that is a
// VALID JSON-RPC initialize is rejected with 413 in every lifecycle mode, and
// the rejection still carries the pre-existing "exceeds 4 MiB" message.
func TestHTTPHandlerMaxRequestBodyBytesZeroPreservesDefaultReject(t *testing.T) {
	t.Parallel()
	body := bigInitialize(t, (4<<20)+1)
	for _, tc := range []struct {
		name    string
		mode    server.ProtocolMode
		chunked bool
	}{
		{"legacy-content-length", server.Legacy, false},
		{"stateless-content-length", server.Stateless20260728, false},
		{"stateless-chunked", server.Stateless20260728, true},
		{"dual-content-length", server.Dual, false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			h, err := s.HTTPHandler(&server.HTTPOptions{
				ProtocolMode: tc.mode,
				Security:     server.DefaultHTTPSecurity(),
				// MaxRequestBodyBytes left zero: the SDK default 4 MiB applies.
			})
			if err != nil {
				t.Fatalf("HTTPHandler: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
			if tc.chunked {
				req.ContentLength = -1
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tc.mode != server.Legacy {
				req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
			}
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413 (body %q)", res.Code, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), "exceeds 4 MiB") {
				t.Fatalf("413 body = %q, want the 4 MiB default message", res.Body.String())
			}
		})
	}
}

// TestHTTPHandlerMaxRequestBodyBytesConfiguredAdmitsOversized proves a positive
// override raises the bound: the same >4 MiB body the default rejects is
// admitted when MaxRequestBodyBytes is 5 MiB and reaches the SDK — a legacy
// initialize answers 200 with a session and a modern server/discover answers
// 200 — through both lifecycle handler stacks the shared newSDKHandler builds
// (D-204).
func TestHTTPHandlerMaxRequestBodyBytesConfiguredAdmitsOversized(t *testing.T) {
	t.Parallel()
	const limit = int64(5 << 20)

	t.Run("legacy-initialize", func(t *testing.T) {
		s := newTestServer(t)
		h, err := s.HTTPHandler(&server.HTTPOptions{
			Security:            server.DefaultHTTPSecurity(),
			MaxRequestBodyBytes: limit,
		})
		if err != nil {
			t.Fatalf("HTTPHandler: %v", err)
		}
		ts := httptest.NewServer(h)
		t.Cleanup(ts.Close)

		body := bigInitialize(t, (4<<20)+1)
		req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			t.Fatalf("5 MiB limit rejected a %d-byte valid initialize with 413: %s", len(body), raw)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body reached decode; %s)", resp.StatusCode, raw)
		}
		if resp.Header.Get("Mcp-Session-Id") == "" {
			t.Fatal("valid legacy initialize did not create an Mcp-Session-Id")
		}
	})

	t.Run("stateless-discover", func(t *testing.T) {
		s := newTestServer(t)
		h, err := s.HTTPHandler(&server.HTTPOptions{
			ProtocolMode:        server.Stateless20260728,
			Security:            server.DefaultHTTPSecurity(),
			MaxRequestBodyBytes: limit,
		})
		if err != nil {
			t.Fatalf("HTTPHandler: %v", err)
		}
		ts := httptest.NewServer(h)
		t.Cleanup(ts.Close)

		body := bigDiscover(t, (4<<20)+1)
		req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", "server/discover")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			t.Fatalf("5 MiB limit rejected a %d-byte valid server/discover with 413: %s", len(body), raw)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body reached decode; %s)", resp.StatusCode, raw)
		}
	})
}

// TestHTTPHandlerMaxRequestBodyBytesConfiguredRejectsOverLimit proves the
// configured bound is enforced for both a fixed Content-Length POST and a
// chunked POST (ContentLength -1, the MaxBytesReader path), and does so in the
// Legacy, Stateless20260728, and Dual handler stacks alike — the shared
// newSDKHandler forwards the same bound to each (D-204).
func TestHTTPHandlerMaxRequestBodyBytesConfiguredRejectsOverLimit(t *testing.T) {
	t.Parallel()
	const limit = int64(5 << 20)
	body := bigInitialize(t, int(limit)+1)
	for _, tc := range []struct {
		name    string
		mode    server.ProtocolMode
		chunked bool
	}{
		{"legacy-content-length", server.Legacy, false},
		{"legacy-chunked", server.Legacy, true},
		{"stateless-content-length", server.Stateless20260728, false},
		{"stateless-chunked", server.Stateless20260728, true},
		{"dual-content-length", server.Dual, false},
		{"dual-chunked", server.Dual, true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			h, err := s.HTTPHandler(&server.HTTPOptions{
				ProtocolMode:        tc.mode,
				Security:            server.DefaultHTTPSecurity(),
				MaxRequestBodyBytes: limit,
			})
			if err != nil {
				t.Fatalf("HTTPHandler: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
			if tc.chunked {
				req.ContentLength = -1
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tc.mode != server.Legacy {
				req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
			}
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413 (body %q)", res.Code, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), "exceeds 5 MiB") {
				t.Fatalf("413 body = %q, want the 5 MiB limit message", res.Body.String())
			}
		})
	}
}

// TestHTTPHandlerMaxRequestBodyBytesNegativeIsError proves a negative option is
// refused at construction: the go-sdk would treat it as "no limit", and
// Dockyard never builds a handler that silently disables the DoS bound on a
// transport exposed to untrusted clients (D-204). The zero value, by contrast,
// still constructs — it preserves the default rather than erroring.
func TestHTTPHandlerMaxRequestBodyBytesNegativeIsError(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	for _, limit := range []int64{-1, -(4 << 20), -1 << 40} {
		if _, err := s.HTTPHandler(&server.HTTPOptions{
			Security:            server.DefaultHTTPSecurity(),
			MaxRequestBodyBytes: limit,
		}); err == nil {
			t.Fatalf("HTTPHandler with MaxRequestBodyBytes=%d: want constructor error", limit)
		}
	}
	if _, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()}); err != nil {
		t.Fatalf("HTTPHandler with zero MaxRequestBodyBytes: %v", err)
	}
}

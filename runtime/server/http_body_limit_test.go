package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/server"
)

func TestHTTPHandlerRejectsOversizedPostsAcrossLifecycles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		mode    server.ProtocolMode
		chunked bool
	}{
		{"legacy-content-length", server.Legacy, false},
		{"stateless-chunked", server.Stateless20260728, true},
		{"dual-content-length", server.Dual, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: tc.mode, Security: server.DefaultHTTPSecurity()})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp",
				strings.NewReader(strings.Repeat("x", (4<<20)+1)))
			if tc.chunked {
				req.ContentLength = -1
			}
			req.Header.Set("Content-Type", "application/json")
			if tc.mode != server.Legacy {
				req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
			}
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != http.StatusRequestEntityTooLarge || !strings.Contains(res.Body.String(), "exceeds 4 MiB") {
				t.Fatalf("status/body = %d/%q, want 413", res.Code, res.Body.String())
			}
		})
	}
}

package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hurtener/dockyard/runtime/server"
)

// TestSDK2026LifecycleModesAreTransportDistinct proves the SDK's lifecycle
// switch is transport configuration, not a JSON-RPC-body discriminator. Phase
// 32 can therefore dispatch to one of these handlers from declared version.
func TestSDK2026LifecycleModesAreTransportDistinct(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	legacy, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("legacy HTTPHandler: %v", err)
	}
	modern, err := s.HTTPHandler(&server.HTTPOptions{
		Security:  server.DefaultHTTPSecurity(),
		Stateless: true,
	})
	if err != nil {
		t.Fatalf("stateless HTTPHandler: %v", err)
	}

	legacyResponse := httptest.NewRecorder()
	legacy.ServeHTTP(legacyResponse, httptest.NewRequest(http.MethodGet, "http://example.test/mcp", nil))
	modernResponse := httptest.NewRecorder()
	modern.ServeHTTP(modernResponse, httptest.NewRequest(http.MethodGet, "http://example.test/mcp", nil))

	if modernResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("stateless GET status = %d, want %d", modernResponse.Code, http.StatusMethodNotAllowed)
	}
	if got := modernResponse.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("stateless GET Allow = %q, want %q", got, http.MethodPost)
	}
	if legacyResponse.Code == modernResponse.Code {
		t.Fatalf("legacy and stateless GET statuses both %d; SDK modes are not distinguishable", modernResponse.Code)
	}
}

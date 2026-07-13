package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hurtener/dockyard/runtime/authz"
	"github.com/hurtener/dockyard/runtime/tasks"
)

const serverAuthTestDriver = "server-auth-test"

var serverAuthFactoryCalls atomic.Int64
var serverAuthValidatorCalls atomic.Int64

func init() {
	authz.RegisterDriver(serverAuthTestDriver, func(context.Context, authz.Config) (authz.Validator, error) {
		serverAuthFactoryCalls.Add(1)
		return serverAuthValidator{}, nil
	})
}

type serverAuthValidator struct{}

func (serverAuthValidator) Validate(_ context.Context, token string) (authz.Principal, error) {
	serverAuthValidatorCalls.Add(1)
	switch token {
	case "alice":
		return authz.Principal{Issuer: "https://issuer.example", Subject: "alice", Resource: "https://resource.example/mcp", Scopes: []string{"read", "write"}}, nil
	case "narrow":
		return authz.Principal{Issuer: "https://issuer.example", Subject: "alice", Resource: "https://resource.example/mcp", Scopes: []string{"read"}}, nil
	default:
		return authz.Principal{}, authz.ErrInvalidToken
	}
}

func authHTTPOptions() *HTTPOptions {
	return &HTTPOptions{Authorization: &authz.Config{Driver: serverAuthTestDriver, Resource: "https://resource.example/mcp", Issuer: "https://issuer.example", Scopes: []string{"read", "write", "admin"}, RequiredScopes: []string{"read", "write"}, ContinuationKey: []byte("0123456789abcdef0123456789abcdef")}}
}

func TestHTTPTransportChecksPrecedeTokenValidation(t *testing.T) {
	s, _ := New(Info{Name: "auth-order", Version: "1"}, nil)
	h, err := s.HTTPHandler(authHTTPOptions())
	if err != nil {
		t.Fatal(err)
	}
	before := serverAuthValidatorCalls.Load()

	crossOrigin := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp", strings.NewReader(`{}`))
	crossOrigin.Header.Set("Authorization", "Bearer alice")
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "https://attacker.example")
	h.ServeHTTP(httptest.NewRecorder(), crossOrigin)

	wrongContent := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp", strings.NewReader(`{}`))
	wrongContent.Header.Set("Authorization", "Bearer alice")
	wrongContent.Header.Set("Content-Type", "text/plain")
	h.ServeHTTP(httptest.NewRecorder(), wrongContent)
	if got := serverAuthValidatorCalls.Load(); got != before {
		t.Fatalf("validator calls = %d, want %d", got, before)
	}
}

func TestHTTPAuthorizationMetadataChallengesAndPrincipal(t *testing.T) {
	s, err := New(Info{Name: "auth", Version: "1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got authz.Principal
	opts := authHTTPOptions()
	opts.ServerForRequest = func(r *http.Request) *Server { got, _ = authz.PrincipalFromContext(r.Context()); return s }
	before := serverAuthFactoryCalls.Load()
	h, err := s.HTTPHandler(opts)
	if err != nil {
		t.Fatal(err)
	}
	if serverAuthFactoryCalls.Load() != before+1 {
		t.Fatal("validator was not constructed exactly once")
	}

	metadata := httptest.NewRecorder()
	h.ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, "https://resource.example/.well-known/oauth-protected-resource/mcp", nil))
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"resource":"https://resource.example/mcp"`) {
		t.Fatalf("metadata = %d %s", metadata.Code, metadata.Body.String())
	}
	method := httptest.NewRecorder()
	h.ServeHTTP(method, httptest.NewRequest(http.MethodPost, "https://resource.example/.well-known/oauth-protected-resource/mcp", nil))
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("metadata method = %d", method.Code)
	}

	for _, header := range []string{"", "Basic abc", "Bearer invalid"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "https://resource.example/mcp", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Header().Get("WWW-Authenticate"), "resource_metadata=") {
			t.Errorf("header %q = %d %q", header, rr.Code, rr.Header().Get("WWW-Authenticate"))
		}
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://resource.example/mcp", nil)
	req.Header.Set("Authorization", "Bearer narrow")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Header().Get("WWW-Authenticate"), `scope="read write"`) {
		t.Fatalf("scope = %d %q", rr.Code, rr.Header().Get("WWW-Authenticate"))
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "https://resource.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer alice")
	h.ServeHTTP(rr, req)
	if got.Subject != "alice" {
		t.Fatalf("principal = %#v", got)
	}
}

func TestAuthenticatedContinuationRejectsCrossPrincipalAndTampering(t *testing.T) {
	p := newContinuationProtector([]byte("0123456789abcdef0123456789abcdef"))
	alice := authz.Principal{Issuer: "i", Subject: "alice", Resource: "r"}
	bob := authz.Principal{Issuer: "i", Subject: "bob", Resource: "r"}
	state, err := p.seal(alice, "tool", []byte(`{"x":1}`), "application-state")
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := p.open(alice, "tool", []byte(`{"x":1}`), state); err != nil || opened != "application-state" {
		t.Fatalf("open = %q, %v", opened, err)
	}
	for _, tc := range []struct {
		principal authz.Principal
		tool      string
		args      []byte
		state     RequestState
	}{{bob, "tool", []byte(`{"x":1}`), state}, {alice, "other", []byte(`{"x":1}`), state}, {alice, "tool", []byte(`{"x":2}`), state}, {alice, "tool", []byte(`{"x":1}`), state + "x"}} {
		if _, err := p.open(tc.principal, tc.tool, tc.args, tc.state); err == nil {
			t.Fatal("accepted invalid continuation")
		}
	}
}

func TestAuthorizationHandlerConcurrentReuse(t *testing.T) {
	s, _ := New(Info{Name: "auth", Version: "1"}, nil)
	h, err := s.HTTPHandler(authHTTPOptions())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "https://resource.example/mcp", nil)
			req.Header.Set("Authorization", "Bearer alice")
			h.ServeHTTP(rr, req)
			if rr.Code == http.StatusUnauthorized {
				t.Error(errors.New("valid token rejected"))
			}
		}()
	}
	wg.Wait()
}

func TestVerifiedTaskIdentityOverridesLegacyCallback(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = tasks.RequestAuthContext(r.Context()) })
	h := taskAuthMiddleware(next, func(*http.Request) string { return "spoofed-callback" })
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req = req.WithContext(tasks.WithRequestAuthContext(req.Context(), "verified-principal"))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "verified-principal" {
		t.Fatalf("task identity = %q", got)
	}
}

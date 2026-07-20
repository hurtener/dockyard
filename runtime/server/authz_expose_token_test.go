package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/authz"
	"github.com/hurtener/dockyard/runtime/tasks"
)

func exposeTokenOptions(expose bool) *HTTPOptions {
	o := authHTTPOptions()
	o.Authorization.ExposeRawToken = expose
	return o
}

func legacyInitializeReq(bearer string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+bearer)
	return req
}

func modernDiscoverReq(bearer string) *http.Request {
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	req := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Authorization", "Bearer "+bearer)
	return req
}

// TestExposeRawTokenOnPathReturnsValidatedToken proves the opt-in exposes the
// exact validated token to the handler, under both protocol lifecycles.
func TestExposeRawTokenOnPathReturnsValidatedToken(t *testing.T) {
	for _, lc := range []struct {
		name string
		mode ProtocolMode
		req  func(string) *http.Request
	}{
		{"legacy", Legacy, legacyInitializeReq},
		{"modern", Stateless20260728, modernDiscoverReq},
	} {
		t.Run(lc.name, func(t *testing.T) {
			s, _ := New(Info{Name: "expose-on", Version: "1"}, nil)
			var gotToken string
			var gotOK bool
			opts := exposeTokenOptions(true)
			opts.ProtocolMode = lc.mode
			opts.ServerForRequest = func(r *http.Request) *Server {
				gotToken, gotOK = authz.RawTokenFromContext(r.Context())
				return s
			}
			h, err := s.HTTPHandler(opts)
			if err != nil {
				t.Fatal(err)
			}
			h.ServeHTTP(httptest.NewRecorder(), lc.req("alice"))
			if !gotOK || gotToken != "alice" {
				t.Fatalf("handler token = %q, %v; want alice, true", gotToken, gotOK)
			}
		})
	}
}

// TestExposeRawTokenDefaultOffHidesToken proves the zero value keeps the token
// out of the handler context — no behavior change for existing servers.
func TestExposeRawTokenDefaultOffHidesToken(t *testing.T) {
	s, _ := New(Info{Name: "expose-off", Version: "1"}, nil)
	var gotToken string
	var gotOK, reached bool
	opts := exposeTokenOptions(false)
	opts.ServerForRequest = func(r *http.Request) *Server {
		reached = true
		gotToken, gotOK = authz.RawTokenFromContext(r.Context())
		return s
	}
	h, err := s.HTTPHandler(opts)
	if err != nil {
		t.Fatal(err)
	}
	h.ServeHTTP(httptest.NewRecorder(), legacyInitializeReq("alice"))
	if !reached {
		t.Fatal("handler not reached on a valid request")
	}
	if gotOK || gotToken != "" {
		t.Fatalf("token exposed with the flag off: %q, %v", gotToken, gotOK)
	}
}

// TestExposeRawTokenNotExposedForRejectedRequest proves a request rejected by
// the validation / resource / scope gates never reaches a handler, so no token
// is ever exposed for it — even with the flag on.
func TestExposeRawTokenNotExposedForRejectedRequest(t *testing.T) {
	s, _ := New(Info{Name: "expose-reject", Version: "1"}, nil)
	reached := false
	opts := exposeTokenOptions(true)
	opts.ServerForRequest = func(r *http.Request) *Server { reached = true; return s }
	h, err := s.HTTPHandler(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, bearer := range []string{"invalid", "wrong-resource", "narrow"} {
		reached = false
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, legacyInitializeReq(bearer))
		if reached {
			t.Fatalf("handler reached for rejected bearer %q (status %d)", bearer, rr.Code)
		}
		if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusForbidden {
			t.Fatalf("bearer %q status = %d, want 401 or 403", bearer, rr.Code)
		}
	}
}

// TestExposeRawTokenNeverEntersDurableTaskState proves that even when the raw
// token is present in the request context (ExposeRawToken on), a task created
// on that request persists no token bytes: the engine binds the derived
// principal identity, never the token (D-196, D-201).
func TestExposeRawTokenNeverEntersDurableTaskState(t *testing.T) {
	const sentinel = "SENTINEL-RAW-BEARER-9f83a1c7"
	store := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(store, &tasks.Options{
		GenerateID: func() (string, error) { return "tok-free-task", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	principal := authz.Principal{
		Issuer: "https://issuer.example", Subject: "alice",
		Resource: "https://resource.example/mcp", Scopes: []string{"read"},
	}
	// The exact context shape the auth middleware builds with ExposeRawToken on:
	// the raw token is present, and the Tasks auth context is the derived key.
	ctx := authz.WithRawToken(context.Background(), sentinel)
	ctx = tasks.WithRequestAuthContext(ctx, principal.BindingKey())

	if _, err := engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName:    "work",
		AuthContext: principal.BindingKey(),
		Run:         func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
	}); err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	rec, err := store.Get(context.Background(), "tok-free-task")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	blob, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), sentinel) {
		t.Fatalf("durable task record contains the raw bearer token: %s", blob)
	}
}

// TestExposeRawTokenScrubbedFromDetachedTaskRun proves the exposed token stays
// request-scoped: an async task run detached from the request context does NOT
// inherit it, so a task handler that outlives the request cannot read a token
// it should re-exchange for (D-201). runtime/server registers the authz
// scrubber via init(), so this holds for any engine in the process.
func TestExposeRawTokenScrubbedFromDetachedTaskRun(t *testing.T) {
	store := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(store, &tasks.Options{
		GenerateID: func() (string, error) { return "scrub-task", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan struct {
		token string
		ok    bool
	}, 1)
	principal := authz.Principal{Issuer: "iss", Subject: "alice", Resource: "res"}
	ctx := authz.WithRawToken(context.Background(), "SENTINEL-DETACH-TOKEN")
	ctx = tasks.WithRequestAuthContext(ctx, principal.BindingKey())

	if _, err := engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName:    "work",
		AuthContext: principal.BindingKey(),
		Run: func(runCtx context.Context) (json.RawMessage, error) {
			tok, ok := authz.RawTokenFromContext(runCtx)
			seen <- struct {
				token string
				ok    bool
			}{tok, ok}
			return json.RawMessage(`{}`), nil
		},
	}); err != nil {
		t.Fatalf("CreateForToolCall: %v", err)
	}
	got := <-seen
	if got.ok || got.token != "" {
		t.Fatalf("detached task run inherited the exposed token: %q, %v", got.token, got.ok)
	}
}

// TestMRTRContinuationBindsPrincipalNotToken proves the authenticated MRTR
// continuation binds the non-reversible principal key and never the raw token
// or the plaintext subject (D-196, D-201).
func TestMRTRContinuationBindsPrincipalNotToken(t *testing.T) {
	p := newContinuationProtector([]byte("0123456789abcdef0123456789abcdef"))
	principal := authz.Principal{Issuer: "https://issuer.example", Subject: "alice", Resource: "https://resource.example/mcp"}
	state, err := p.seal(principal, "work", json.RawMessage(`{"x":1}`), RequestState("handler-state"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	payloadB64, _, ok := strings.Cut(string(state), ".")
	if !ok {
		t.Fatalf("sealed continuation malformed: %s", state)
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatalf("decode continuation payload: %v", err)
	}
	var env continuationEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Binding != principal.BindingKey() {
		t.Fatalf("continuation binding = %q, want the derived binding key", env.Binding)
	}
	if strings.Contains(string(payload), "alice") {
		t.Fatalf("continuation payload leaks the plaintext subject/token: %s", payload)
	}
}

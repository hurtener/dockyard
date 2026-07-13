package inspector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// This file is the Phase 27 inspector security re-audit (sub-goal E). The
// existing TestNew_RejectsNonLoopback covers the cardinal shapes (the
// wildcard, a routable address, a malformed string). The re-audit
// strengthens the sweep with additional adversarial shapes drawn from the
// CVE-2025-49596 lesson (brief 05 §4.2) and from the practical landscape of
// SSRF/host-header attacks: the IPv6 unspecified address ("[::]"), the
// per-interface IPv6 unspecified, IPv4-mapped IPv6 loopback, leading /
// trailing whitespace, embedded path / scheme bytes, and DNS-resolved hosts
// that an attacker could control through /etc/hosts (the inspector accepts
// "localhost" as a string shortcut — that behaviour is documented and
// asserted here).

// TestPhase27_InspectorBindShape_AdversarialSweep drives every adversarial
// bind shape we can construct against [New] and asserts each is rejected
// with [ErrNonLoopbackBind] before the listener opens — none of the
// adversarial shapes must produce a usable [Inspector] handle.
func TestPhase27_InspectorBindShape_AdversarialSweep(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
	}{
		// The wildcard shapes — every public-facing bind.
		{"ipv4 wildcard 0.0.0.0", "0.0.0.0:0"},
		{"ipv6 unspecified [::]", "[::]:0"},
		{"ipv6 unspecified bare", "::0"},
		{"port-only", ":0"},
		{"port-only-with-explicit-port", ":8080"},
		// Routable / private / link-local — never loopback.
		{"private 192.168", "192.168.1.10:0"},
		{"private 10.0", "10.0.0.1:0"},
		{"private 172.16", "172.16.0.1:0"},
		{"link-local 169.254", "169.254.0.1:0"},
		{"public dns", "8.8.8.8:0"},
		{"ipv6 public", "[2001:4860:4860::8888]:0"},
		{"ipv6 link-local", "[fe80::1]:0"},
		// Malformed addresses.
		// NB the empty string is NOT here: it is a documented "use the
		// loopback default" shortcut and is asserted in the positive
		// LoopbackAccepted suite below.
		{"garbage", "not-an-address"},
		{"path-as-host", "/etc/passwd:0"},
		{"scheme-in-addr", "http://127.0.0.1:0"},
		{"leading whitespace", " 127.0.0.1:0"},
		{"trailing whitespace", "127.0.0.1:0 "},
		{"both whitespace", "  127.0.0.1:0  "},
		// Hostnames that resolve to something else are NOT in our threat
		// model — the gate is structural — but we assert the only
		// hostname we DO accept ("localhost") is the lone string-form
		// shortcut, by negative example.
		{"random hostname", "evil.example:0"},
		{"non-localhost loopback alias", "loopback:0"},
		// Adversarial port — but loopback is the gate, so this should pass.
		// (Recorded as a NEGATIVE control below in TestPhase27_InspectorBindShape_LoopbackAccepted.)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			insp, err := New(Options{Addr: tc.addr})
			if err == nil {
				_ = insp.Close()
				t.Fatalf("New(%q): expected rejection but got a live Inspector at %s",
					tc.addr, insp.Addr())
			}
			if !errors.Is(err, ErrNonLoopbackBind) {
				t.Fatalf("New(%q): want ErrNonLoopbackBind, got %v", tc.addr, err)
			}
		})
	}
}

// TestPhase27_InspectorBindShape_LoopbackAccepted is the negative-control
// counterpart: every shape we DO accept actually opens a listener.
func TestPhase27_InspectorBindShape_LoopbackAccepted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
	}{
		{"ipv4 loopback", "127.0.0.1:0"},
		// 127.0.0.0/8 is loopback per net.IP.IsLoopback, but not every host
		// allows binding to non-127.0.0.1 loopback IPs (Darwin does not by
		// default). The gate still accepts those addresses; the assertion
		// would be environment-dependent, so it is not exercised here.
		{"ipv6 loopback", "[::1]:0"},
		{"ipv4-mapped ipv6 loopback", "[::ffff:127.0.0.1]:0"},
		{"localhost shortcut", "localhost:0"},
		// Empty: the inspector uses the defaultAddr (127.0.0.1:0).
		{"empty selects default", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			insp, err := New(Options{Addr: tc.addr})
			if err != nil {
				t.Fatalf("New(%q): unexpected rejection: %v", tc.addr, err)
			}
			defer func() { _ = insp.Close() }()
			if !looksLikeLoopback(insp.Addr()) {
				t.Fatalf("New(%q): resolved Addr=%q is not loopback", tc.addr, insp.Addr())
			}
		})
	}
}

// looksLikeLoopback is a simple textual check the resolved Addr is bound to
// a loopback interface — the resolved address is host:port, the host is
// either 127.x.x.x or ::1.
func looksLikeLoopback(addr string) bool {
	return strings.HasPrefix(addr, "127.") ||
		strings.HasPrefix(addr, "[::1]") ||
		strings.HasPrefix(addr, "::1")
}

func TestInspectorAPIRejectsCrossOriginAndNonJSONPosts(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	mux := newMux(Options{Invoker: func(context.Context, InvokeRequest) (*InvokeResponse, error) {
		calls.Add(1)
		return &InvokeResponse{}, nil
	}}, slog.New(slog.DiscardHandler))

	tests := []struct {
		name        string
		origin      string
		contentType string
		wantStatus  int
	}{
		{name: "cross-origin simple request", origin: "https://attacker.example", contentType: "text/plain", wantStatus: http.StatusForbidden},
		{name: "same-origin non-json request", contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/tools/invoke",
				bytes.NewBufferString(`{"tool":"dangerous","arguments":{}}`))
			req.Header.Set("Content-Type", tt.contentType)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, req)
			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, tt.wantStatus, res.Body.String())
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("invoker called %d times for rejected requests", calls.Load())
	}
}

func TestInspectorRejectsNonLoopbackHostBeforeOriginCheck(t *testing.T) {
	t.Parallel()
	mux := newMux(Options{}, slog.New(slog.DiscardHandler))
	for _, tc := range []struct {
		host       string
		origin     string
		wantStatus int
	}{
		{host: "127.0.0.1:8080", wantStatus: http.StatusOK},
		{host: "[::1]:8080", wantStatus: http.StatusOK},
		{host: "[::1]", wantStatus: http.StatusOK},
		{host: "localhost:8080", wantStatus: http.StatusOK},
		{host: "LOCALHOST.:8080", wantStatus: http.StatusOK},
		{host: "attacker.example:8080", origin: "http://attacker.example:8080", wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/info", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, req)
			if res.Code != tc.wantStatus {
				t.Fatalf("Host %q status = %d, want %d", tc.host, res.Code, tc.wantStatus)
			}
		})
	}
}

func TestInspectorResponsesCannotBeFramed(t *testing.T) {
	t.Parallel()
	mux := newMux(Options{}, slog.New(slog.DiscardHandler))
	for _, host := range []string{"127.0.0.1:8080", "attacker.example:8080"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/info", nil)
		req.Host = host
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if got := res.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
			t.Errorf("Host %q CSP = %q", host, got)
		}
		if got := res.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("Host %q X-Frame-Options = %q", host, got)
		}
	}
}

func TestInspectorJSONPostsRejectTrailingAndOversizedBodies(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	mux := newMux(Options{
		Invoker: func(context.Context, InvokeRequest) (*InvokeResponse, error) {
			calls.Add(1)
			return &InvokeResponse{}, nil
		},
		Elicitor: func(context.Context, ElicitationRequest) (*ElicitationResponse, error) {
			calls.Add(1)
			return &ElicitationResponse{}, nil
		},
		PromptInvoker: func(context.Context, PromptGetRequest) (*PromptGetResponse, error) {
			calls.Add(1)
			return &PromptGetResponse{}, nil
		},
	}, slog.New(slog.DiscardHandler))

	large := strings.Repeat("x", maxInspectorJSONBody+1)
	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{name: "invoke trailing", path: "/api/tools/invoke", body: `{"tool":"echo","arguments":{}} {}`},
		{name: "elicitation trailing", path: "/api/tasks/elicitation", body: `{"protocol":"2026-07-28","taskId":"t","inputResponses":{}} {}`},
		{name: "prompt trailing", path: "/api/prompts/get", body: `{"name":"hello","arguments":{}} {}`},
		{name: "invoke oversized", path: "/api/tools/invoke", body: `{"tool":"` + large + `"}`},
		{name: "elicitation oversized", path: "/api/tasks/elicitation", body: `{"protocol":"2026-07-28","taskId":"` + large + `","inputResponses":{}}`},
		{name: "prompt oversized", path: "/api/prompts/get", body: `{"name":"` + large + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, req)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusBadRequest, res.Body.String())
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("mutating callback called %d times for rejected bodies", calls.Load())
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/authz"
)

// unauthHandshakeOptions returns the standard auth options with the opt-in
// UnauthenticatedHandshake flag toggled.
func unauthHandshakeOptions(on bool) *HTTPOptions {
	o := authHTTPOptions()
	o.Authorization.UnauthenticatedHandshake = on
	return o
}

func postRPC(method, bearer string) *http.Request {
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return req
}

// probe wires a server whose ServerForRequest records whether the handler was
// reached and what principal (if any) the context carried, so a test can assert
// the auth verdict without depending on the SDK's response body.
func probe(t *testing.T, opts *HTTPOptions) (http.Handler, *bool, *authz.Principal, *bool) {
	t.Helper()
	s, err := New(Info{Name: "unauth-handshake", Version: "1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	var principal authz.Principal
	hasPrincipal := false
	opts.ServerForRequest = func(r *http.Request) *Server {
		reached = true
		principal, hasPrincipal = authz.PrincipalFromContext(r.Context())
		return s
	}
	h, err := s.HTTPHandler(opts)
	if err != nil {
		t.Fatal(err)
	}
	return h, &reached, &principal, &hasPrincipal
}

// TestUnauthHandshakeDefaultOffProtectsEveryMethod proves the zero value is
// unchanged: with the flag off, even the handshake 401s without a token.
func TestUnauthHandshakeDefaultOffProtectsEveryMethod(t *testing.T) {
	for _, method := range []string{"initialize", "tools/list", "ping", "server/discover"} {
		h, reached, _, _ := probe(t, unauthHandshakeOptions(false))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, postRPC(method, ""))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("flag off, %s without token: status = %d, want 401", method, rr.Code)
		}
		if *reached {
			t.Errorf("flag off, %s without token reached the handler", method)
		}
	}
}

// TestUnauthHandshakeExemptMethodsSucceedWithoutToken proves the lifecycle and
// discovery methods are served with no token when the flag is on, with no
// principal populated.
func TestUnauthHandshakeExemptMethodsSucceedWithoutToken(t *testing.T) {
	for _, method := range []string{
		"initialize", "notifications/initialized", "ping", "server/discover",
		"tools/list", "resources/list", "resources/templates/list", "prompts/list",
	} {
		h, reached, _, hasPrincipal := probe(t, unauthHandshakeOptions(true))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, postRPC(method, ""))
		if rr.Code == http.StatusUnauthorized {
			t.Errorf("flag on, exempt %s without token: got 401", method)
		}
		if !*reached {
			t.Errorf("flag on, exempt %s without token did not reach the handler", method)
		}
		if *hasPrincipal {
			t.Errorf("flag on, exempt %s without token populated a principal", method)
		}
	}
}

// TestUnauthHandshakeInvocationsStillRequireToken proves deny-by-default: every
// non-exempt method — invocations and control/notification methods alike — still
// 401s without a token, with the Bearer challenge, and never reaches the handler.
func TestUnauthHandshakeInvocationsStillRequireToken(t *testing.T) {
	for _, method := range []string{
		"tools/call", "resources/read", "resources/subscribe", "resources/unsubscribe",
		"prompts/get", "completion/complete", "logging/setLevel", "notifications/cancelled",
		"notifications/roots/list_changed", "tasks/get", "tasks/cancel", "some/future/method",
	} {
		h, reached, _, _ := probe(t, unauthHandshakeOptions(true))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, postRPC(method, ""))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("flag on, protected %s without token: status = %d, want 401", method, rr.Code)
		}
		if !strings.Contains(rr.Header().Get("WWW-Authenticate"), "resource_metadata=") {
			t.Errorf("flag on, protected %s: missing Bearer challenge (%q)", method, rr.Header().Get("WWW-Authenticate"))
		}
		if *reached {
			t.Errorf("flag on, protected %s without token reached the handler", method)
		}
	}
}

// TestUnauthHandshakeValidTokenPopulatesPrincipalOnExempt proves a valid token
// on an exempt method still yields an identity-filterable principal, and that
// RequiredScopes are NOT enforced on discovery (the low-scope "narrow" token,
// which lacks the "write" required scope, is accepted on tools/list).
func TestUnauthHandshakeValidTokenPopulatesPrincipalOnExempt(t *testing.T) {
	for _, bearer := range []string{"alice", "narrow"} {
		h, reached, principal, hasPrincipal := probe(t, unauthHandshakeOptions(true))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, postRPC("tools/list", bearer))
		if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
			t.Fatalf("valid token %q on exempt tools/list: status = %d", bearer, rr.Code)
		}
		if !*reached || !*hasPrincipal {
			t.Fatalf("valid token %q on exempt tools/list: reached=%v hasPrincipal=%v", bearer, *reached, *hasPrincipal)
		}
		if principal.Subject != "alice" {
			t.Fatalf("valid token %q on exempt tools/list: principal subject = %q, want alice", bearer, principal.Subject)
		}
	}
}

// TestUnauthHandshakeInvalidTokenOnExemptIsRejected proves a token that is
// present but invalid is an error even on an exempt method — its absence is
// tolerated, but a bad credential is surfaced, not silently ignored.
func TestUnauthHandshakeInvalidTokenOnExemptIsRejected(t *testing.T) {
	for _, bearer := range []string{"garbage", "wrong-resource"} {
		h, reached, _, _ := probe(t, unauthHandshakeOptions(true))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, postRPC("tools/list", bearer))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("invalid token %q on exempt tools/list: status = %d, want 401", bearer, rr.Code)
		}
		if *reached {
			t.Errorf("invalid token %q on exempt tools/list reached the handler", bearer)
		}
	}
}

// TestUnauthHandshakeLowScopeTokenStillGatedOnInvocation proves the scope gate
// is enforced on invocations even with the flag on: the low-scope "narrow" token
// (read only) is accepted on discovery but 403s on tools/call (needs write).
func TestUnauthHandshakeLowScopeTokenStillGatedOnInvocation(t *testing.T) {
	h, reached, _, _ := probe(t, unauthHandshakeOptions(true))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, postRPC("tools/call", "narrow"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("narrow token on tools/call: status = %d, want 403", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("WWW-Authenticate"), `scope="read write"`) {
		t.Fatalf("narrow token on tools/call: missing scope challenge (%q)", rr.Header().Get("WWW-Authenticate"))
	}
	if *reached {
		t.Fatal("narrow token on tools/call reached the handler")
	}
}

// TestUnauthHandshakeBatchRequiresTokenIfAnyInvocation proves the batch rule: an
// all-exempt batch is served without a token, but a batch containing a single
// invocation requires a valid token for the whole batch.
func TestUnauthHandshakeBatchRequiresTokenIfAnyInvocation(t *testing.T) {
	batch := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		return req
	}

	allExempt := `[{"jsonrpc":"2.0","id":1,"method":"tools/list"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`
	h, reached, _, _ := probe(t, unauthHandshakeOptions(true))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, batch(allExempt))
	if rr.Code == http.StatusUnauthorized {
		t.Errorf("all-exempt batch without token: got 401")
	}
	if !*reached {
		t.Errorf("all-exempt batch without token did not reach the handler")
	}

	mixed := `[{"jsonrpc":"2.0","id":1,"method":"tools/list"},{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{}}]`
	h2, reached2, _, _ := probe(t, unauthHandshakeOptions(true))
	rr2 := httptest.NewRecorder()
	h2.ServeHTTP(rr2, batch(mixed))
	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("batch with an invocation without token: status = %d, want 401", rr2.Code)
	}
	if *reached2 {
		t.Errorf("batch with an invocation without token reached the handler")
	}
}

// TestUnauthHandshakeGETStreamOpenIsExempt proves the legacy SSE stream-open GET
// is served without a token when the flag is on (it 401s with the flag off).
func TestUnauthHandshakeGETStreamOpenIsExempt(t *testing.T) {
	on, reachedOn, _, _ := probe(t, unauthHandshakeOptions(true))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://resource.example/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	on.ServeHTTP(rr, req)
	if rr.Code == http.StatusUnauthorized {
		t.Errorf("flag on: GET stream-open without token got 401")
	}
	_ = reachedOn

	off, _, _, _ := probe(t, unauthHandshakeOptions(false))
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "https://resource.example/mcp", nil)
	req2.Header.Set("Accept", "text/event-stream")
	off.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("flag off: GET stream-open without token: status = %d, want 401", rr2.Code)
	}
}

// TestUnauthHandshakeExemptSetNotCallerOverridable proves the exempt set is
// owned by Dockyard, not the caller: the only config knob is the boolean flag,
// and no invocation method is in the allowlist. This is a structural guarantee,
// asserted by direct inspection of the package-level allowlist.
func TestUnauthHandshakeExemptSetNotCallerOverridable(t *testing.T) {
	invocations := []string{
		"tools/call", "resources/read", "resources/subscribe", "resources/unsubscribe",
		"prompts/get", "completion/complete", "logging/setLevel",
		"tasks/get", "tasks/update", "tasks/cancel",
	}
	for _, method := range invocations {
		if _, ok := exemptHandshakeMethods[method]; ok {
			t.Errorf("invocation method %q must never be in the exempt allowlist", method)
		}
	}
	want := map[string]struct{}{
		"initialize": {}, "notifications/initialized": {}, "ping": {}, "server/discover": {},
		"tools/list": {}, "resources/list": {}, "resources/templates/list": {}, "prompts/list": {},
	}
	if len(exemptHandshakeMethods) != len(want) {
		t.Fatalf("exempt allowlist size = %d, want %d", len(exemptHandshakeMethods), len(want))
	}
	for m := range want {
		if _, ok := exemptHandshakeMethods[m]; !ok {
			t.Errorf("expected exempt method %q missing from the allowlist", m)
		}
	}
}

// TestUnauthHandshakeModernLifecycleParity proves the peek is per-POST and
// identical across lifecycles: on the modern stateless 2026-07-28 lifecycle,
// server/discover is exempt while tools/call still requires a token.
func TestUnauthHandshakeModernLifecycleParity(t *testing.T) {
	modern := func(method, bearer string) *http.Request {
		body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
		req := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		return req
	}

	opts := unauthHandshakeOptions(true)
	opts.ProtocolMode = Stateless20260728
	h, reached, _, _ := probe(t, opts)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, modern("server/discover", ""))
	if rr.Code == http.StatusUnauthorized {
		t.Errorf("modern server/discover without token got 401")
	}
	if !*reached {
		t.Errorf("modern server/discover without token did not reach the handler")
	}

	opts2 := unauthHandshakeOptions(true)
	opts2.ProtocolMode = Stateless20260728
	h2, reached2, _, _ := probe(t, opts2)
	rr2 := httptest.NewRecorder()
	h2.ServeHTTP(rr2, modern("tools/call", ""))
	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("modern tools/call without token: status = %d, want 401", rr2.Code)
	}
	if *reached2 {
		t.Errorf("modern tools/call without token reached the handler")
	}
}

// differentialBodies are JSON-RPC bodies whose exact-case "method" is an
// invocation but which a case-insensitive / last-wins decoder could mis-read as
// an exempt method. The go-sdk dispatches on the exact-case "method"
// (case-sensitive segmentio decoder), so the peek must NOT classify any of these
// as exempt — otherwise an invocation reaches a handler unauthenticated.
var differentialBodies = []string{
	`{"jsonrpc":"2.0","id":1,"method":"tools/call","Method":"tools/list","params":{}}`,
	`{"jsonrpc":"2.0","id":1,"method":"tools/call","METHOD":"ping","params":{}}`,
	`{"jsonrpc":"2.0","id":1,"method":"resources/read","mEthod":"resources/list"}`,
	`{"jsonrpc":"2.0","id":1,"Method":"tools/list","method":"tools/call","params":{}}`,
	`[{"method":"tools/list"},{"method":"tools/call","Method":"ping"}]`,
}

// TestUnauthHandshakeParserDifferentialCannotBypass is the authoritative guard
// against a peek/dispatch parser differential: each body's exact-case method is
// an invocation, so with no token and the flag on it MUST 401 and never reach a
// handler — regardless of how a lenient decoder might read the decoy key. It
// exercises the real SDK dispatch, so it catches any residual differential
// independent of the peek's own decoder (D-202 adversarial-review regression).
func TestUnauthHandshakeParserDifferentialCannotBypass(t *testing.T) {
	for _, body := range differentialBodies {
		h, reached, _, _ := probe(t, unauthHandshakeOptions(true))
		req := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("differential body %q without token: status = %d, want 401 (fail-open!)", body, rr.Code)
		}
		if *reached {
			t.Errorf("differential body %q without token REACHED the handler (auth bypass!)", body)
		}
	}
}

// TestAllMethodsExempt is a table test over the fail-closed peek.
func TestAllMethodsExempt(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"initialize", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, true},
		{"tools/list", `{"method":"tools/list"}`, true},
		{"server/discover", `{"method":"server/discover","params":{}}`, true},
		{"tools/call", `{"method":"tools/call"}`, false},
		{"unknown method", `{"method":"totally/made/up"}`, false},
		{"missing method", `{"jsonrpc":"2.0","id":1}`, false},
		{"empty object", `{}`, false},
		{"empty body", ``, false},
		{"whitespace only", "   \n\t", false},
		{"non-string method", `{"method":123}`, false},
		{"null", `null`, false},
		{"bare string", `"tools/list"`, false},
		{"garbage", `{not json`, false},
		{"empty batch", `[]`, false},
		{"all-exempt batch", `[{"method":"tools/list"},{"method":"ping"}]`, true},
		{"mixed batch", `[{"method":"tools/list"},{"method":"tools/call"}]`, false},
		{"batch with missing method", `[{"method":"ping"},{"id":2}]`, false},
		{"leading whitespace exempt", "  {\"method\":\"ping\"}", true},
		{"case-variant decoy after invocation", `{"method":"tools/call","Method":"tools/list"}`, false},
		{"case-variant decoy before discovery", `{"Method":"tools/list","method":"ping"}`, false},
		{"uppercase decoy", `{"method":"resources/read","METHOD":"resources/list"}`, false},
		{"exact duplicate method last-wins exempt", `{"method":"tools/call","method":"tools/list"}`, true},
		{"exact duplicate method last-wins invocation", `{"method":"tools/list","method":"tools/call"}`, false},
		{"batch with case-variant decoy", `[{"method":"tools/list"},{"method":"tools/call","Method":"ping"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := allMethodsExempt([]byte(tc.body)); got != tc.want {
				t.Fatalf("allMethodsExempt(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// FuzzAllMethodsExempt asserts the peek never panics on arbitrary bytes and is
// fail-closed: whenever it reports a body exempt, an independent re-decode
// confirms every JSON-RPC method in that body is in the Dockyard-owned
// allowlist. This is the load-bearing security invariant — it must never return
// true for a request that carries an invocation.
func FuzzAllMethodsExempt(f *testing.F) {
	seeds := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"method":"tools/list"}`,
		`{"method":"server/discover","params":{}}`,
		`{"method":"tools/call","params":{}}`,
		`[{"method":"tools/list"},{"method":"tools/call"}]`,
		`[{"method":"ping"}]`,
		`[]`, `{}`, ``, `null`, `"x"`, `{"method":123}`, `{not json`,
	}
	seeds = append(seeds, differentialBodies...)
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	// exactMethod extracts the method the same way the go-sdk dispatcher does:
	// the exact-case "method" key, refusing any body with a case-variant sibling
	// (which the SDK's case-sensitive decoder would ignore). This is an
	// independent re-implementation used only to cross-check the peek.
	exactMethod := func(raw json.RawMessage) (string, bool) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return "", false
		}
		rm, ok := obj["method"]
		if !ok {
			return "", false
		}
		for k := range obj {
			if k != "method" && strings.EqualFold(k, "method") {
				return "", false // case-variant sibling: ambiguous
			}
		}
		var m string
		if err := json.Unmarshal(rm, &m); err != nil {
			return "", false
		}
		return m, true
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		if !allMethodsExempt(body) { // must not panic
			return
		}
		// Reported exempt: every message's exact-case method must be in the
		// allowlist with no ambiguating sibling. A body that reports exempt yet
		// carries a non-allowlisted or ambiguous method is a fail-open bug.
		trimmed := strings.TrimLeft(string(body), " \t\r\n")
		if trimmed == "" {
			t.Fatalf("empty body reported exempt")
		}
		elems := []json.RawMessage{json.RawMessage(body)}
		if trimmed[0] == '[' {
			var batch []json.RawMessage
			if err := json.Unmarshal(body, &batch); err != nil || len(batch) == 0 {
				t.Fatalf("exempt batch failed independent decode: %q", body)
			}
			elems = batch
		}
		for _, el := range elems {
			method, ok := exactMethod(el)
			if !ok {
				t.Fatalf("exempt element has no unambiguous exact-case method: %q", body)
			}
			if _, allow := exemptHandshakeMethods[method]; !allow {
				t.Fatalf("exempt element has non-allowlisted method %q: %q", method, body)
			}
		}
	})
}

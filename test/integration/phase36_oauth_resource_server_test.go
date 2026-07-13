package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hurtener/dockyard/runtime/authz"
	"github.com/hurtener/dockyard/runtime/authz/jwtjwks"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"
)

func TestPhase36OAuthResourceServerEndToEnd(t *testing.T) {
	t.Parallel()
	as := newPhase36AuthorizationServer(t)
	clock := time.Now()
	var clockNanos atomic.Int64
	clockNanos.Store(clock.UnixNano())
	resource := "https://api.example.test/tenant/mcp"
	taskStore := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(taskStore, &tasks.Options{
		GenerateID:            func() (string, error) { return fmt.Sprintf("oauth-task-%d", as.taskIDs.Add(1)), nil },
		RequestorIdentifiable: true,
		AdvertiseList:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(server.Info{Name: "phase36", Version: "1"}, &server.Options{Logger: quietLogger(), Tasks: engine})
	if err != nil {
		t.Fatal(err)
	}
	type input struct {
		Action string `json:"action"`
	}
	type output struct {
		Subject string `json:"subject"`
	}
	releases := make(chan struct{})
	t.Cleanup(func() { close(releases) })
	if err := tool.New[input, output]("protected").ContinuationHandler(func(ctx context.Context, call tool.Call[input]) (tool.Result[output], error) {
		principal, ok := authz.PrincipalFromContext(ctx)
		if !ok {
			return tool.Result[output]{}, fmt.Errorf("verified principal missing")
		}
		switch call.Input.Action {
		case "task":
			created, createErr := engine.CreateToolTask(ctx, tasks.CreateToolCallParams{ToolName: "protected", Run: func(ctx context.Context) (json.RawMessage, error) {
				select {
				case <-releases:
					return json.RawMessage(`{"content":[]}`), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}, true)
			return tool.Result[output]{CreatedTask: &created}, createErr
		case "mrtr":
			if call.RequestState == "" {
				return tool.Result[output]{InputRequests: map[string]tool.InputRequest{"confirm": tool.ElicitationRequest{Message: "confirm"}}, RequestState: "server-state"}, nil
			}
			if call.RequestState != "server-state" {
				return tool.Result[output]{}, fmt.Errorf("unexpected continuation state")
			}
		}
		return tool.Result[output]{Structured: output{Subject: principal.Subject}}, nil
	}).Register(srv); err != nil {
		t.Fatal(err)
	}
	h, err := srv.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Dual, Security: server.DefaultHTTPSecurity(), Authorization: &authz.Config{
		Driver: jwtjwks.DriverName, Resource: resource, Issuer: as.issuer(), Scopes: []string{"mcp:read", "mcp:write", "mcp:admin"}, RequiredScopes: []string{"mcp:read", "mcp:write"},
		ContinuationKey: []byte("phase-36-integration-continuation-key"), DriverConfig: jwtjwks.Config{
			AllowedAlgorithms: []string{"RS256"}, HTTPClient: as.server.Client(), CacheTTL: time.Hour,
			RefreshCooldown: time.Second, Now: func() time.Time { return time.Unix(0, clockNanos.Load()) },
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mcp := httptest.NewServer(h)
	t.Cleanup(mcp.Close)

	metadataURL := mcp.URL + "/.well-known/oauth-protected-resource/tenant/mcp"
	canonicalMetadataURL := "https://api.example.test/.well-known/oauth-protected-resource/tenant/mcp"
	resp := phase36Request(t, mcp.Client(), http.MethodGet, metadataURL, "", "", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata status = %d", resp.StatusCode)
	}
	var metadata authz.ProtectedResourceMetadata
	phase36Decode(t, resp, &metadata)
	if metadata.Resource != resource || len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != as.issuer() || strings.Join(metadata.ScopesSupported, " ") != "mcp:read mcp:write mcp:admin" || strings.Join(metadata.BearerMethodsSupported, " ") != "header" {
		t.Fatalf("protected resource metadata = %#v", metadata)
	}

	validAlice := as.token(t, as.key, "current", "RS256", as.issuer(), resource, "alice", "mcp:read mcp:write", time.Now().Add(time.Hour))
	badKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	cases := map[string]string{
		"missing": "", "malformed": "not-a-jwt",
		"expired":         as.token(t, as.key, "current", "RS256", as.issuer(), resource, "alice", "mcp:read mcp:write", time.Now().Add(-time.Minute)),
		"bad signature":   as.token(t, badKey, "current", "RS256", as.issuer(), resource, "alice", "mcp:read mcp:write", time.Now().Add(time.Hour)),
		"wrong algorithm": as.token(t, as.key, "current", "RS512", as.issuer(), resource, "alice", "mcp:read mcp:write", time.Now().Add(time.Hour)),
		"wrong issuer":    as.token(t, as.key, "current", "RS256", as.server.URL+"/other", resource, "alice", "mcp:read mcp:write", time.Now().Add(time.Hour)),
		"wrong audience":  as.token(t, as.key, "current", "RS256", as.issuer(), "https://other.example/mcp", "alice", "mcp:read mcp:write", time.Now().Add(time.Hour)),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			resp := phase36RPC(t, mcp, token, true, "tools/call", map[string]any{"name": "protected", "arguments": map[string]any{"action": "echo"}}, "")
			if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get("WWW-Authenticate"), `resource_metadata="`+canonicalMetadataURL+`"`) {
				t.Fatalf("status/challenge = %d/%q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
			}
			phase36Close(t, resp)
		})
	}
	narrow := as.token(t, as.key, "current", "RS256", as.issuer(), resource, "alice", "mcp:read", time.Now().Add(time.Hour))
	resp = phase36RPC(t, mcp, narrow, true, "tools/call", map[string]any{"name": "protected", "arguments": map[string]any{"action": "echo"}}, "")
	challenge := resp.Header.Get("WWW-Authenticate")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(challenge, `error="insufficient_scope"`) || !strings.Contains(challenge, `scope="mcp:read mcp:write"`) || !strings.Contains(challenge, `resource_metadata="`+canonicalMetadataURL+`"`) {
		t.Fatalf("scope status/challenge = %d/%q", resp.StatusCode, challenge)
	}
	phase36Close(t, resp)

	// Legacy initialization and a session-bound call use the same per-request authentication.
	legacyInit := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"phase36","version":"1"}}}`
	resp = phase36Request(t, mcp.Client(), http.MethodPost, mcp.URL, validAlice, "", "", legacyInit)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Mcp-Session-Id") == "" {
		t.Fatalf("legacy initialize status/session = %d/%q", resp.StatusCode, resp.Header.Get("Mcp-Session-Id"))
	}
	legacySession := resp.Header.Get("Mcp-Session-Id")
	phase36Close(t, resp)
	legacy := phase36RPC(t, mcp, validAlice, false, "tools/call", map[string]any{"name": "protected", "arguments": map[string]any{"action": "echo"}}, legacySession)
	phase36ExpectSubject(t, legacy, "alice")
	legacyCreated := phase36JSON(t, phase36RPC(t, mcp, validAlice, false, "tools/call", map[string]any{"name": "protected", "arguments": map[string]any{"action": "task"}}, legacySession))
	legacyResult, _ := legacyCreated["result"].(map[string]any)
	legacyTask, _ := legacyResult["task"].(map[string]any)
	legacyTaskID, _ := legacyTask["taskId"].(string)
	if legacyTaskID == "" {
		t.Fatalf("missing legacy task ID: %#v", legacyCreated)
	}
	for _, method := range []string{"tasks/get", "tasks/list", "tasks/cancel", "tasks/result"} {
		params := map[string]any{}
		if method != "tasks/list" {
			params["taskId"] = legacyTaskID
		}
		got := phase36JSON(t, phase36RPC(t, mcp, validAlice, false, method, params, legacySession))
		if method != "tasks/result" && got["error"] != nil {
			t.Fatalf("authenticated legacy %s failed: %#v", method, got)
		}
	}

	modern := phase36RPC(t, mcp, validAlice, true, "tools/call", map[string]any{"name": "protected", "arguments": map[string]any{"action": "echo"}, "_meta": map[string]any{"principal": "mallory"}}, "")
	phase36ExpectSubject(t, modern, "alice")

	// An unknown kid causes one bounded refresh and accepts the rotated real key.
	before := as.jwksRequests.Load()
	rotated := as.rotate(t, "rotated")
	clockNanos.Store(clock.Add(2 * time.Second).UnixNano())
	rotatedToken := as.token(t, rotated, "rotated", "RS256", as.issuer(), resource, "alice", "mcp:read mcp:write", time.Now().Add(time.Hour))
	phase36ExpectSubject(t, phase36RPC(t, mcp, rotatedToken, true, "tools/call", map[string]any{"name": "protected", "arguments": map[string]any{"action": "echo"}}, ""), "alice")
	if delta := as.jwksRequests.Load() - before; delta != 1 {
		t.Fatalf("rotation JWKS fetches = %d, want 1", delta)
	}

	bob := as.token(t, rotated, "rotated", "RS256", as.issuer(), resource, "bob", "mcp:read mcp:write", time.Now().Add(time.Hour))
	created := phase36JSON(t, phase36RPC(t, mcp, rotatedToken, true, "tools/call", map[string]any{"name": "protected", "arguments": map[string]any{"action": "task"}}, ""))
	taskID := phase36ResultString(t, created, "taskId")
	for _, method := range []string{"tasks/get", "tasks/update", "tasks/cancel"} {
		params := map[string]any{"taskId": taskID}
		if method == "tasks/update" {
			params["inputResponses"] = map[string]any{}
		}
		if got := phase36JSON(t, phase36RPC(t, mcp, bob, true, method, params, "")); got["error"] == nil {
			t.Fatalf("bob %s succeeded: %#v", method, got)
		}
	}
	if got := phase36JSON(t, phase36RPC(t, mcp, rotatedToken, true, "tasks/get", map[string]any{"taskId": taskID}, "")); got["error"] != nil {
		t.Fatalf("alice tasks/get failed: %#v", got)
	}
	if err := taskStore.AddInputRequest(context.Background(), taskID, tasks.InputRequest{
		Key: "roots", Method: tasks.InputMethodRoots,
		Payload: json.RawMessage(`{"method":"roots/list","params":{}}`),
	}); err != nil {
		t.Fatalf("add pending task input: %v", err)
	}
	if got := phase36JSON(t, phase36RPC(t, mcp, rotatedToken, true, "tasks/update", map[string]any{"taskId": taskID, "inputResponses": map[string]any{}}, "")); got["error"] != nil {
		t.Fatalf("alice tasks/update failed: %#v", got)
	}
	if got := phase36JSON(t, phase36RPC(t, mcp, rotatedToken, true, "tasks/cancel", map[string]any{"taskId": taskID}, "")); got["error"] != nil {
		t.Fatalf("alice tasks/cancel failed: %#v", got)
	}

	first := phase36JSON(t, phase36RPC(t, mcp, rotatedToken, true, "tools/call", map[string]any{"name": "protected", "arguments": map[string]any{"action": "mrtr"}}, ""))
	state := phase36ResultString(t, first, "requestState")
	retryParams := map[string]any{"name": "protected", "arguments": map[string]any{"action": "mrtr"}, "requestState": state, "inputResponses": map[string]any{"confirm": map[string]any{"action": "accept"}}}
	phase36ExpectSubject(t, phase36RPC(t, mcp, rotatedToken, true, "tools/call", retryParams, ""), "alice")
	if got := phase36JSON(t, phase36RPC(t, mcp, bob, true, "tools/call", retryParams, "")); !phase36ToolError(got) {
		t.Fatalf("bob reused alice MRTR continuation: %#v", got)
	}
}

type phase36AS struct {
	server       *httptest.Server
	mu           sync.RWMutex
	key          *rsa.PrivateKey
	kid          string
	jwksRequests atomic.Int64
	taskIDs      atomic.Int64
}

func newPhase36AuthorizationServer(t *testing.T) *phase36AS {
	t.Helper()
	f := &phase36AS{}
	f.rotate(t, "current")
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server/tenant":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": f.issuer(), "jwks_uri": f.server.URL + "/jwks"})
		case "/jwks":
			f.jwksRequests.Add(1)
			f.mu.RLock()
			key, kid := f.key, f.kid
			f.mu.RUnlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{phase36JWK(kid, &key.PublicKey)}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *phase36AS) issuer() string { return f.server.URL + "/tenant" }
func (f *phase36AS) rotate(t *testing.T, kid string) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.key, f.kid = key, kid
	f.mu.Unlock()
	return key
}
func phase36JWK(kid string, key *rsa.PublicKey) map[string]string {
	return map[string]string{"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}
}
func (f *phase36AS) token(t *testing.T, key *rsa.PrivateKey, kid, alg, issuer, audience, subject, scope string, expiry time.Time) string {
	t.Helper()
	method := jwt.GetSigningMethod(alg)
	token := jwt.NewWithClaims(method, jwt.MapClaims{"iss": issuer, "aud": audience, "sub": subject, "scope": scope, "exp": expiry.Unix(), "iat": time.Now().Add(-time.Second).Unix()})
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func phase36RPC(t *testing.T, ts *httptest.Server, token string, modern bool, method string, params map[string]any, session string) *http.Response {
	t.Helper()
	if modern {
		meta, _ := params["_meta"].(map[string]any)
		if meta == nil {
			meta = map[string]any{}
		}
		meta["io.modelcontextprotocol/protocolVersion"] = "2026-07-28"
		meta["io.modelcontextprotocol/clientInfo"] = map[string]any{"name": "phase36", "version": "1"}
		meta["io.modelcontextprotocol/clientCapabilities"] = map[string]any{"extensions": map[string]any{"io.modelcontextprotocol/tasks": map[string]any{}}}
		params["_meta"] = meta
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	version := ""
	if modern {
		version = "2026-07-28"
	}
	return phase36Request(t, ts.Client(), http.MethodPost, ts.URL, token, version, session, string(body))
}
func phase36Request(t *testing.T, client *http.Client, method, url, token, version, session, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if version != "" {
		req.Header.Set("Mcp-Protocol-Version", version)
		var rpc struct {
			Method string `json:"method"`
			Params struct {
				Name   string `json:"name"`
				TaskID string `json:"taskId"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(body), &rpc) == nil {
			req.Header.Set("Mcp-Method", rpc.Method)
			if rpc.Params.Name != "" {
				req.Header.Set("Mcp-Name", rpc.Params.Name)
			} else if rpc.Params.TaskID != "" {
				req.Header.Set("Mcp-Name", rpc.Params.TaskID)
			}
		}
	}
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
func phase36Decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer phase36Close(t, resp)
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatal(err)
	}
}
func phase36JSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer phase36Close(t, resp)
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "data: ") {
			raw = []byte(strings.TrimPrefix(line, "data: "))
			break
		}
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode response: %v (%s)", err, raw)
	}
	return got
}
func phase36Close(t *testing.T, resp *http.Response) {
	t.Helper()
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}
func phase36ResultString(t *testing.T, got map[string]any, key string) string {
	t.Helper()
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %#v", got)
	}
	value, _ := result[key].(string)
	if value == "" {
		t.Fatalf("missing result.%s: %#v", key, got)
	}
	return value
}
func phase36ExpectSubject(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	got := phase36JSON(t, resp)
	result, _ := got["result"].(map[string]any)
	structured, _ := result["structuredContent"].(map[string]any)
	if structured["subject"] != want {
		t.Fatalf("subject response = %#v, want %q", got, want)
	}
}

func phase36ToolError(got map[string]any) bool {
	result, _ := got["result"].(map[string]any)
	isError, _ := result["isError"].(bool)
	return got["error"] != nil || isError
}

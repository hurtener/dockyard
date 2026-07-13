package jwtjwks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
)

type issuerFixture struct {
	t        testing.TB
	server   *httptest.Server
	mu       sync.RWMutex
	keys     map[string]*rsa.PrivateKey
	extra    []map[string]string
	requests atomic.Int64
	outage   atomic.Bool
}

func newIssuer(t testing.TB) *issuerFixture {
	f := &issuerFixture{t: t, keys: map[string]*rsa.PrivateKey{}}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server/tenant":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": f.server.URL + "/tenant", "jwks_uri": f.server.URL + "/keys"})
		case "/keys":
			if f.outage.Load() {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			f.mu.RLock()
			defer f.mu.RUnlock()
			keys := make([]map[string]string, 0, len(f.keys))
			for kid, key := range f.keys {
				keys = append(keys, rsaJWK(kid, &key.PublicKey))
			}
			keys = append(keys, f.extra...)
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *issuerFixture) rotate(kid string) *rsa.PrivateKey {
	f.t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	f.keys = map[string]*rsa.PrivateKey{kid: key}
	f.mu.Unlock()
	return key
}

func rsaJWK(kid string, key *rsa.PublicKey) map[string]string {
	e := big.NewInt(int64(key.E)).Bytes()
	return map[string]string{"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e)}
}

func sign(t testing.TB, key *rsa.PrivateKey, kid, issuer, audience, subject string, expiry time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{"iss": issuer, "aud": audience, "sub": subject, "exp": expiry.Unix(), "iat": time.Now().Add(-time.Second).Unix(), "scope": "read write"}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newValidator(t testing.TB, f *issuerFixture, cfg Config) *Validator {
	t.Helper()
	cfg.HTTPClient = f.server.Client()
	if cfg.AllowedAlgorithms == nil {
		cfg.AllowedAlgorithms = []string{"RS256"}
	}
	v, err := New(context.Background(), f.server.URL+"/tenant", "https://resource.example/mcp", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidateExactClaimsAndNoTokenLeak(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("one")
	v := newValidator(t, f, Config{})
	valid := sign(t, key, "one", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour))
	p, err := v.Validate(context.Background(), valid)
	if err != nil || p.Subject != "alice" || strings.Join(p.Scopes, ",") != "read,write" {
		t.Fatalf("principal = %#v, %v", p, err)
	}
	tests := []string{
		sign(t, key, "one", f.server.URL+"/other", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour)),
		sign(t, key, "one", f.server.URL+"/tenant", "https://other.example", "alice", time.Now().Add(time.Hour)),
		sign(t, key, "one", f.server.URL+"/tenant", "https://resource.example/mcp", "", time.Now().Add(time.Hour)),
		sign(t, key, "one", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(-time.Hour)),
		strings.Join([]string{"raw", "invalid", "token"}, "."),
	}
	for _, raw := range tests {
		_, err := v.Validate(context.Background(), raw)
		if !errors.Is(err, authz.ErrInvalidToken) {
			t.Errorf("error = %v", err)
		}
		if err != nil && strings.Contains(err.Error(), raw) {
			t.Fatal("error leaked token")
		}
	}
}

func TestRejectsWrongAlgorithm(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("one")
	v := newValidator(t, f, Config{})
	token := jwt.NewWithClaims(jwt.SigningMethodRS512, jwt.MapClaims{"iss": f.server.URL + "/tenant", "aud": "https://resource.example/mcp", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix()})
	token.Header["kid"] = "one"
	raw, _ := token.SignedString(key)
	if _, err := v.Validate(context.Background(), raw); !errors.Is(err, authz.ErrInvalidToken) {
		t.Fatalf("error = %v", err)
	}
}

func TestRejectsJWTAlgorithmThatConflictsWithJWKMetadata(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("one")
	v := newValidator(t, f, Config{AllowedAlgorithms: []string{"RS256", "RS512"}})
	token := jwt.NewWithClaims(jwt.SigningMethodRS512, jwt.MapClaims{"iss": f.server.URL + "/tenant", "aud": "https://resource.example/mcp", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix()})
	token.Header["kid"] = "one"
	raw, _ := token.SignedString(key)
	if _, err := v.Validate(context.Background(), raw); !errors.Is(err, authz.ErrInvalidToken) {
		t.Fatalf("algorithm-confused token error = %v", err)
	}
}

func TestConcurrentKeyRotation(t *testing.T) {
	f := newIssuer(t)
	oldKey := f.rotate("old")
	now := time.Now()
	v := newValidator(t, f, Config{CacheTTL: time.Second, RefreshCooldown: time.Second, Now: func() time.Time { return now }})
	oldToken := sign(t, oldKey, "old", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour))
	if _, err := v.Validate(context.Background(), oldToken); err != nil {
		t.Fatal(err)
	}
	newKey := f.rotate("new")
	now = now.Add(2 * time.Second)
	newToken := sign(t, newKey, "new", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour))
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := v.Validate(context.Background(), newToken); errs <- err }()
	}
	wg.Wait()
	close(errs)
	var success bool
	for err := range errs {
		if err == nil {
			success = true
		}
	}
	if !success {
		t.Fatal("no concurrent validation observed rotated key")
	}
}

func TestExpiredCacheFailsClosedWhenRemovedKeyCannotRefresh(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("removed")
	now := time.Now()
	v := newValidator(t, f, Config{CacheTTL: time.Minute, RefreshCooldown: 5 * time.Minute, Now: func() time.Time { return now }})
	token := sign(t, key, "removed", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour))
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	f.rotate("replacement")
	f.outage.Store(true)
	now = now.Add(6 * time.Minute)
	if _, err := v.Validate(context.Background(), token); !errors.Is(err, authz.ErrInvalidToken) {
		t.Fatalf("expired cached key accepted during outage: %v", err)
	}
}

func TestExpiredCacheRefreshesWhenTTLIsShorterThanCooldown(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("stable")
	now := time.Now()
	v := newValidator(t, f, Config{CacheTTL: time.Second, RefreshCooldown: 5 * time.Minute, Now: func() time.Time { return now }})
	token := sign(t, key, "stable", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))

	before := f.requests.Load()
	now = now.Add(2 * time.Second)
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatalf("valid unchanged-key token rejected after cache expiry: %v", err)
	}
	if got := f.requests.Load() - before; got != 1 {
		t.Fatalf("post-expiry refresh requests = %d, want 1", got)
	}

	// A failed post-expiry refresh is still throttled even though the cache
	// remains expired and its TTL is shorter than the cooldown.
	now = now.Add(2 * time.Second)
	f.outage.Store(true)
	before = f.requests.Load()
	for range 2 {
		if _, err := v.Validate(context.Background(), token); !errors.Is(err, authz.ErrInvalidToken) {
			t.Fatalf("token accepted while expired keys could not refresh: %v", err)
		}
	}
	if got := f.requests.Load() - before; got != 1 {
		t.Fatalf("failed post-expiry refresh requests = %d, want 1 during cooldown", got)
	}
}

func TestDiscoveryAndJWKSBounds(t *testing.T) {
	f := newIssuer(t)
	f.rotate("one")
	if _, err := New(context.Background(), f.server.URL+"/tenant", "https://resource.example", Config{HTTPClient: f.server.Client()}); err == nil {
		t.Fatal("accepted implicit algorithms")
	}
	if _, err := New(context.Background(), f.server.URL+"/tenant", "https://resource.example", Config{HTTPClient: f.server.Client(), AllowedAlgorithms: []string{"none"}}); err == nil {
		t.Fatal("accepted unsafe algorithm")
	}
	if _, err := New(context.Background(), f.server.URL+"/tenant", "https://resource.example", Config{HTTPClient: f.server.Client(), AllowedAlgorithms: []string{"RS256"}, MaxResponseBytes: 10}); err == nil {
		t.Fatal("accepted oversized response")
	}
}

func TestConfigurationBoundsAndCustomClientTimeout(t *testing.T) {
	f := newIssuer(t)
	f.rotate("one")
	v := newValidator(t, f, Config{})
	if v.client.Timeout != defaultFetchTimeout {
		t.Fatalf("custom client timeout = %v, want %v", v.client.Timeout, defaultFetchTimeout)
	}

	tests := []Config{
		{MaxResponseBytes: minResponseBytes - 1},
		{MaxResponseBytes: maxResponseBytes + 1},
		{MaxKeys: maxKeyCount + 1},
		{CacheTTL: minCacheTTL - time.Nanosecond},
		{CacheTTL: maxCacheTTL + time.Nanosecond},
		{RefreshCooldown: minRefreshCooldown - time.Nanosecond},
		{RefreshCooldown: maxRefreshCooldown + time.Nanosecond},
		{ClockSkew: maxClockSkew + time.Nanosecond},
	}
	for _, cfg := range tests {
		cfg.HTTPClient = f.server.Client()
		cfg.AllowedAlgorithms = []string{"RS256"}
		if _, err := New(context.Background(), f.server.URL+"/tenant", "https://resource.example", cfg); err == nil {
			t.Fatalf("accepted out-of-range config %#v", cfg)
		}
	}
}

func TestFailedRefreshAttemptsAreThrottled(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("known")
	now := time.Now()
	v := newValidator(t, f, Config{CacheTTL: time.Minute, RefreshCooldown: time.Second, Now: func() time.Time { return now }})
	now = now.Add(2 * time.Second)
	f.outage.Store(true)
	token := sign(t, key, "unknown", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour))
	before := f.requests.Load()
	for range 2 {
		if _, err := v.Validate(context.Background(), token); !errors.Is(err, authz.ErrInvalidToken) {
			t.Fatalf("unknown-key validation error = %v", err)
		}
	}
	if got := f.requests.Load() - before; got != 1 {
		t.Fatalf("failed refresh requests = %d, want 1 during cooldown", got)
	}
}

func TestSameKeyIDRotationRefreshesAndRetries(t *testing.T) {
	f := newIssuer(t)
	oldKey := f.rotate("stable")
	now := time.Now()
	v := newValidator(t, f, Config{CacheTTL: time.Minute, RefreshCooldown: time.Second, Now: func() time.Time { return now }})
	oldToken := sign(t, oldKey, "stable", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour))
	if _, err := v.Validate(context.Background(), oldToken); err != nil {
		t.Fatal(err)
	}
	newKey := f.rotate("stable")
	now = now.Add(2 * time.Second)
	newToken := sign(t, newKey, "stable", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour))
	if _, err := v.Validate(context.Background(), newToken); err != nil {
		t.Fatalf("same-kid rotated token rejected: %v", err)
	}
}

func TestMixedJWKSFiltersIrrelevantKeys(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("signing")
	signing := rsaJWK("signing", &key.PublicKey)
	delete(signing, "alg")
	f.mu.Lock()
	f.extra = []map[string]string{
		signing,
		{"kty": "oct", "kid": "symmetric", "use": "sig", "alg": "HS256", "k": "AA"},
		{"kty": "RSA", "kid": "encryption", "use": "enc", "alg": "RSA-OAEP"},
	}
	f.keys = map[string]*rsa.PrivateKey{}
	f.mu.Unlock()
	v := newValidator(t, f, Config{})
	token := sign(t, key, "signing", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", time.Now().Add(time.Hour))
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatalf("valid token rejected with mixed JWKS: %v", err)
	}
}

func TestDriverRegistered(t *testing.T) {
	found := false
	for _, name := range authz.Drivers() {
		found = found || name == DriverName
	}
	if !found {
		t.Fatal("driver not registered")
	}
}

func TestDriverFactoryAndConfigDecode(t *testing.T) {
	f := newIssuer(t)
	f.rotate("one")
	v, err := authz.Open(context.Background(), authz.Config{Driver: DriverName, Resource: "https://resource.example/mcp", Issuer: f.server.URL + "/tenant", DriverConfig: Config{AllowedAlgorithms: []string{"RS256"}}, ContinuationKey: make([]byte, 32)})
	if err == nil || v != nil {
		// The generic factory intentionally uses its own production HTTP client;
		// the local fixture certificate must not be trusted through JSON config.
		t.Fatalf("Open = %v, %v", v, err)
	}
	_, err = authz.Open(context.Background(), authz.Config{Driver: DriverName, Resource: "https://resource.example/mcp", Issuer: f.server.URL + "/tenant", DriverConfig: struct{}{}, ContinuationKey: make([]byte, 32)})
	if err == nil {
		t.Fatal("accepted unknown driver option")
	}
}

func TestParseScopesAndJWKVariants(t *testing.T) {
	for _, tc := range []struct {
		claims jwt.MapClaims
		ok     bool
	}{
		{jwt.MapClaims{}, true}, {jwt.MapClaims{"scope": ""}, true}, {jwt.MapClaims{"scope": "read write"}, true}, {jwt.MapClaims{"scope": " read"}, false}, {jwt.MapClaims{"scope": []string{"read"}}, false},
	} {
		_, ok := parseScopes(tc.claims)
		if ok != tc.ok {
			t.Errorf("parseScopes(%v) = %v", tc.claims, ok)
		}
	}

	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc := func(n *big.Int) string { return base64.RawURLEncoding.EncodeToString(n.Bytes()) }
	ecRaw, _ := json.Marshal(map[string]string{"kty": "EC", "kid": "ec", "use": "sig", "alg": "ES256", "crv": "P-256", "x": enc(ec.X), "y": enc(ec.Y)})
	kid, key, err := parseJWK(ecRaw)
	if err != nil || kid != "ec" || key.key == nil {
		t.Fatalf("EC key = %q, %T, %v", kid, key, err)
	}
	for _, raw := range []string{`{`, `{"kty":"oct","kid":"x","alg":"HS256"}`, `{"kty":"RSA","kid":"x","n":"bad","e":"AQAB"}`, `{"kty":"RSA","kid":"x","alg":"RS256","n":"` + strings.Repeat("A", 342) + `","e":"Ag"}`, `{"kty":"EC","kid":"x","alg":"ES256","crv":"bad","x":"AA","y":"AA"}`} {
		if _, _, err := parseJWK(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestJWKAlgorithmMetadataIsBoundToToken(t *testing.T) {
	for _, tc := range []struct {
		key verificationKey
		alg string
	}{
		{verificationKey{alg: "RS512", kty: "RSA"}, "RS256"},
		{verificationKey{alg: "RS256", kty: "EC", crv: "P-256"}, "RS256"},
		{verificationKey{alg: "ES256", kty: "EC", crv: "P-384"}, "ES256"},
	} {
		if err := tc.key.matches(tc.alg); err == nil {
			t.Fatalf("accepted key %#v for %s", tc.key, tc.alg)
		}
	}
}

func TestJWKKeyOperationsRequireVerify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		keyOps  []string
		wantErr bool
	}{
		{name: "absent"},
		{name: "verify", keyOps: []string{"verify"}},
		{name: "empty", keyOps: []string{}, wantErr: true},
		{name: "encrypt only", keyOps: []string{"encrypt"}, wantErr: true},
		{name: "sign only", keyOps: []string{"sign"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jwk := map[string]any{}
			for name, value := range rsaJWK("operations", &key.PublicKey) {
				jwk[name] = value
			}
			if tc.keyOps != nil {
				jwk["key_ops"] = tc.keyOps
			}
			raw, err := json.Marshal(jwk)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = parseJWK(raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseJWK error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestRejectsOversizedRSAModulus(t *testing.T) {
	n := make([]byte, maxRSAModulusBytes+1)
	n[0] = 1
	raw, err := json.Marshal(map[string]string{
		"kty": "RSA", "kid": "large", "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(n), "e": "AQAB",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseJWK(raw); err == nil {
		t.Fatal("accepted oversized RSA modulus")
	}
}

func TestDiscoveryRedirectsStayOnHTTPSOriginAndClientIsNotMutated(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	tests := map[string]string{
		"downgrade":    "http://example.invalid/discovery",
		"cross-origin": target.URL + "/discovery",
	}
	for name, location := range tests {
		t.Run(name, func(t *testing.T) {
			source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, location, http.StatusFound)
			}))
			defer source.Close()
			client := source.Client()
			if client.CheckRedirect != nil {
				t.Fatal("fixture unexpectedly has redirect policy")
			}
			_, err := New(context.Background(), source.URL+"/tenant", "https://resource.example", Config{HTTPClient: client, AllowedAlgorithms: []string{"RS256"}})
			if err == nil || !strings.Contains(err.Error(), "redirect") {
				t.Fatalf("New redirect error = %v", err)
			}
			if client.CheckRedirect != nil {
				t.Fatal("New mutated caller HTTP client")
			}
		})
	}
}

func TestJWKSRedirectRejectsDowngradeAndCrossOrigin(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	for name, location := range map[string]string{"downgrade": "http://example.invalid/keys", "cross-origin": target.URL + "/keys"} {
		t.Run(name, func(t *testing.T) {
			issuer := ""
			source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/oauth-authorization-server/tenant":
					_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer + "/tenant", "jwks_uri": issuer + "/keys"})
				case "/keys":
					http.Redirect(w, r, location, http.StatusFound)
				}
			}))
			defer source.Close()
			issuer = source.URL
			_, err := New(context.Background(), source.URL+"/tenant", "https://resource.example", Config{HTTPClient: source.Client(), AllowedAlgorithms: []string{"RS256"}})
			if err == nil || !strings.Contains(err.Error(), "key refresh failed") {
				t.Fatalf("New JWKS redirect error = %v", err)
			}
		})
	}
}

func TestFetchJSONFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			http.Error(w, "no", http.StatusBadGateway)
		case "/trailing":
			_, _ = w.Write([]byte(`{} {}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()
	for _, path := range []string{"/status", "/trailing"} {
		var dst map[string]any
		if err := fetchJSON(context.Background(), server.Client(), server.URL+path, 100, &dst); err == nil {
			t.Errorf("%s succeeded", path)
		}
	}
}

func BenchmarkValidate(b *testing.B) {
	f := newIssuer(b)
	key := f.rotate("bench")
	v := newValidator(b, f, Config{})
	token := sign(b, key, "bench", f.server.URL+"/tenant", "https://resource.example/mcp", "subject", time.Now().Add(time.Hour))
	b.ResetTimer()
	for range b.N {
		if _, err := v.Validate(context.Background(), token); err != nil {
			b.Fatal(err)
		}
	}
}

func TestErrorsNeverContainToken(t *testing.T) {
	f := newIssuer(t)
	f.rotate("one")
	v := newValidator(t, f, Config{})
	raw := strings.Join([]string{"eyJ", "not-a-real-token", "signature"}, ".")
	_, err := v.Validate(context.Background(), raw)
	if err == nil || strings.Contains(fmt.Sprint(err), raw) {
		t.Fatalf("unsafe error %q", err)
	}
}

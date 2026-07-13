package authz

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPrincipalDefensiveCopiesAndBinding(t *testing.T) {
	scopes := []string{"read"}
	ctx := WithPrincipal(context.Background(), Principal{Issuer: "ab", Subject: "sensitive-subject-value", Resource: "d", Scopes: scopes})
	scopes[0] = "changed"
	p, ok := PrincipalFromContext(ctx)
	if !ok || p.Scopes[0] != "read" {
		t.Fatalf("principal = %#v, %v", p, ok)
	}
	p.Scopes[0] = "again"
	again, _ := PrincipalFromContext(ctx)
	if again.Scopes[0] != "read" {
		t.Fatal("context principal was mutable")
	}
	if (Principal{Issuer: "ab", Subject: "c", Resource: "d"}).BindingKey() == (Principal{Issuer: "a", Subject: "bc", Resource: "d"}).BindingKey() {
		t.Fatal("ambiguous tuples collided")
	}
	if strings.Contains(p.BindingKey(), p.Subject) {
		t.Fatal("binding exposed identity")
	}
}

func TestConfigMetadataAndURL(t *testing.T) {
	cfg := Config{Driver: "test", Resource: "https://api.example/mcp/v2?fixed=1", Issuer: "https://issuer.example/tenant", Scopes: []string{"tools.read"}, ContinuationKey: make([]byte, 32)}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	got, _ := cfg.MetadataURL()
	if got != "https://api.example/.well-known/oauth-protected-resource/mcp/v2?fixed=1" {
		t.Fatalf("URL = %q", got)
	}
	m := cfg.Metadata()
	m.ScopesSupported[0] = "changed"
	if cfg.Scopes[0] != "tools.read" {
		t.Fatal("metadata aliased config")
	}
	rr := httptest.NewRecorder()
	cfg.MetadataHandler().ServeHTTP(rr, httptest.NewRequest("GET", got, nil))
	if rr.Code != 200 || rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %v", rr.Code, rr.Header())
	}
	var body ProtectedResourceMetadata
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body.Resource != cfg.Resource {
		t.Fatalf("metadata = %#v, %v", body, err)
	}
}

func TestMetadataURLPreservesQueryAndEscapedPath(t *testing.T) {
	tests := map[string]string{
		"https://api.example":                         "https://api.example/.well-known/oauth-protected-resource",
		"https://api.example/a%2Fb/c?tenant=a%2Fb":    "https://api.example/.well-known/oauth-protected-resource/a%2Fb/c?tenant=a%2Fb",
		"https://api.example/%E2%9C%93?q=hello+world": "https://api.example/.well-known/oauth-protected-resource/%E2%9C%93?q=hello+world",
	}
	for resource, want := range tests {
		got, err := (Config{Resource: resource}).MetadataURL()
		if err != nil || got != want {
			t.Errorf("MetadataURL(%q) = %q, %v; want %q", resource, got, err, want)
		}
	}
}

func TestConfigRejectsUnsafeValues(t *testing.T) {
	base := Config{Driver: "x", Resource: "https://resource.example/mcp", Issuer: "https://issuer.example", Scopes: []string{"read"}, ContinuationKey: make([]byte, 32)}
	tests := []Config{{Resource: base.Resource, Issuer: base.Issuer}, {Driver: "x", Resource: "http://resource", Issuer: base.Issuer, ContinuationKey: base.ContinuationKey}, {Driver: "x", Resource: base.Resource, Issuer: base.Issuer + "?tenant=x", ContinuationKey: base.ContinuationKey}, {Driver: "x", Resource: base.Resource, Issuer: base.Issuer, Scopes: []string{"offline_access"}, ContinuationKey: base.ContinuationKey}}
	for _, cfg := range tests {
		if cfg.Validate() == nil {
			t.Fatalf("accepted %#v", cfg)
		}
	}
}

func TestBearerScopesAndChallenges(t *testing.T) {
	for _, tc := range []struct {
		values []string
		want   string
		err    error
	}{
		{nil, "", ErrMissingToken}, {[]string{"Bearer abc.DEF-_~+/="}, "abc.DEF-_~+/=", nil}, {[]string{"bearer token"}, "token", nil}, {[]string{"Bearer  two"}, "", ErrInvalidToken}, {[]string{"Basic abc"}, "", ErrInvalidToken}, {[]string{"Bearer a", "Bearer b"}, "", ErrInvalidToken},
	} {
		got, err := ParseBearer(tc.values)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("ParseBearer(%q) = %q, %v", tc.values, got, err)
		}
	}
	p := Principal{Scopes: []string{"read", "write"}}
	if err := RequireScopes(p, "read", "write"); err != nil {
		t.Fatal(err)
	}
	err := RequireScopes(p, "admin")
	if !errors.Is(err, ErrInsufficientScope) {
		t.Fatalf("error = %v", err)
	}
	challenge := Challenge("https://r.example/.well-known/oauth-protected-resource/mcp", err, []string{"read", "admin"})
	for _, want := range []string{`error="insufficient_scope"`, `scope="read admin"`, `resource_metadata="https://`} {
		if !strings.Contains(challenge, want) {
			t.Errorf("challenge %q lacks %q", challenge, want)
		}
	}
}

func FuzzParseBearer(f *testing.F) {
	for _, seed := range []string{"Bearer token", "", "Bearer  token", "Basic token", "bearer a.b.c"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, header string) {
		token, err := ParseBearer([]string{header})
		if err == nil && (token == "" || strings.ContainsAny(token, " \t\r\n")) {
			t.Fatalf("accepted invalid token %q", token)
		}
	})
}

func TestRegistryConcurrentReads(t *testing.T) {
	name := "authz-test-driver"
	RegisterDriver(name, func(context.Context, Config) (Validator, error) { return fakeValidator{}, nil })
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = Drivers()
			}
		}()
	}
	wg.Wait()
	v, err := Open(context.Background(), Config{Driver: name, Resource: "https://resource.example", Issuer: "https://issuer.example", ContinuationKey: make([]byte, 32)})
	if err != nil || v == nil {
		t.Fatalf("Open = %v, %v", v, err)
	}
}

type fakeValidator struct{}

func (fakeValidator) Validate(context.Context, string) (Principal, error) { return Principal{}, nil }

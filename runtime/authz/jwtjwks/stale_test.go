package jwtjwks

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/authz"
)

func TestStaleIfErrorBounds(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("known")
	start := time.Now()
	const ttl = time.Minute
	const grace = 2 * time.Minute
	token := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", start.Add(time.Hour))
	unknown := sign(t, key, "unknown", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", start.Add(time.Hour))
	expired := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", start.Add(-time.Hour))
	wrongAudience := sign(t, key, "known", f.server.URL+"/tenant", "https://other.example", "alice", start.Add(time.Hour))
	for _, tt := range []struct {
		name     string
		maxStale time.Duration
		age      time.Duration
		token    string
		valid    bool
	}{
		{"at expiry", grace, ttl, token, true},
		{"within window", grace, ttl + time.Second, token, true},
		{"just before deadline", grace, ttl + grace - time.Nanosecond, token, true},
		{"at deadline", grace, ttl + grace, token, false},
		{"beyond deadline", grace, ttl + grace + time.Second, token, false},
		{"disabled", 0, ttl, token, false},
		{"unknown key", grace, ttl + time.Second, unknown, false},
		{"expired token", grace, ttl + time.Second, expired, false},
		{"wrong audience", grace, ttl + time.Second, wrongAudience, false},
		{"recommended window", DefaultMaxStale, ttl + time.Hour, sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", start.Add(48*time.Hour)), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := start
			f.outage.Store(false)
			v := newValidator(t, f, Config{CacheTTL: ttl, MaxStale: tt.maxStale, Now: func() time.Time { return now }})
			f.outage.Store(true)
			now = start.Add(tt.age)
			_, err := v.Validate(context.Background(), tt.token)
			if tt.valid && err != nil || !tt.valid && !errors.Is(err, authz.ErrInvalidToken) {
				t.Fatalf("Validate error = %v, want valid = %v", err, tt.valid)
			}
		})
	}
}

func TestStaleIfErrorCooldownConcurrent(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("known")
	now := time.Now()
	start := now
	v := newValidator(t, f, Config{CacheTTL: time.Second, MaxStale: time.Minute, RefreshCooldown: 10 * time.Second, Now: func() time.Time { return now }})
	token := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
	f.outage.Store(true)
	before := f.requests.Load()
	now = start.Add(time.Second)
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			if _, err := v.Validate(context.Background(), token); err != nil {
				t.Errorf("concurrent stale validation: %v", err)
			}
		})
	}
	wg.Wait()
	if got := f.requests.Load() - before; got != 1 {
		t.Fatalf("concurrent refresh requests = %d, want 1", got)
	}
	// Unknown IDs must still fail, including during the stale retry cooldown.
	unknown := sign(t, key, "unknown", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
	if _, err := v.Validate(context.Background(), unknown); !errors.Is(err, authz.ErrInvalidToken) {
		t.Fatalf("unknown key accepted during cooldown: %v", err)
	}
	now = now.Add(10*time.Second - time.Nanosecond)
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if got := f.requests.Load() - before; got != 1 {
		t.Fatalf("refresh requests before cooldown = %d, want 1", got)
	}
	now = now.Add(time.Nanosecond)
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if got := f.requests.Load() - before; got != 2 {
		t.Fatalf("refresh requests at cooldown = %d, want 2", got)
	}
	// Neither failed nor throttled attempts may extend the original deadline.
	now = start.Add(time.Second + time.Minute)
	if _, err := v.Validate(context.Background(), token); !errors.Is(err, authz.ErrInvalidToken) {
		t.Fatalf("stale deadline extended after retries: %v", err)
	}
}

func TestStaleIfErrorRecoveryReplacesKeys(t *testing.T) {
	for _, kid := range []string{"new", "old"} {
		t.Run(kid, func(t *testing.T) {
			f := newIssuer(t)
			oldKey := f.rotate("old")
			now := time.Now()
			v := newValidator(t, f, Config{CacheTTL: time.Second, MaxStale: time.Minute, RefreshCooldown: time.Second, Now: func() time.Time { return now }})
			oldToken := sign(t, oldKey, "old", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
			f.outage.Store(true)
			now = now.Add(time.Second)
			if _, err := v.Validate(context.Background(), oldToken); err != nil {
				t.Fatal(err)
			}
			newKey := f.rotate(kid)
			newToken := sign(t, newKey, kid, f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
			// Even a known ID with a new signature must fail during the outage.
			if _, err := v.Validate(context.Background(), newToken); !errors.Is(err, authz.ErrInvalidToken) {
				t.Fatalf("unverified rotated token accepted: %v", err)
			}
			f.outage.Store(false)
			now = now.Add(time.Second)
			if _, err := v.Validate(context.Background(), newToken); err != nil {
				t.Fatalf("rotated key rejected after recovery: %v", err)
			}
			if _, err := v.Validate(context.Background(), oldToken); !errors.Is(err, authz.ErrInvalidToken) {
				t.Fatalf("removed key survived successful refresh: %v", err)
			}
			// Recovery starts a new retention window; it does not retain the old set.
			f.outage.Store(true)
			now = now.Add(time.Minute)
			if _, err := v.Validate(context.Background(), newToken); err != nil {
				t.Fatalf("new set did not receive a new stale window: %v", err)
			}
		})
	}
}

type staleRoundTripper func(*http.Request) (*http.Response, error)

func (f staleRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStaleIfErrorChecksDeadlineAfterFetch(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("known")
	now := time.Now()
	client := *f.server.Client()
	transport := client.Transport
	client.Transport = staleRoundTripper(func(r *http.Request) (*http.Response, error) {
		if f.outage.Load() {
			now = now.Add(time.Minute)
		}
		return transport.RoundTrip(r)
	})
	v, err := New(context.Background(), f.server.URL+"/tenant", "https://resource.example/mcp", Config{HTTPClient: &client, AllowedAlgorithms: []string{"RS256"}, CacheTTL: time.Second, MaxStale: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
	f.outage.Store(true)
	now = now.Add(time.Second)
	if _, err := v.Validate(context.Background(), token); !errors.Is(err, authz.ErrInvalidToken) {
		t.Fatalf("key accepted after fetch crossed stale deadline: %v", err)
	}
}

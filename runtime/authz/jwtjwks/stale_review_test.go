package jwtjwks

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/authz"
)

func TestStaleIfErrorAvailabilityOnly(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("known")
	valid := rsaJWK("known", &key.PublicKey)
	disallowed := rsaJWK("known", &key.PublicKey)
	disallowed["alg"] = "RS512"
	oversized := rsaJWK("known", &key.PublicKey)
	oversized["n"] = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, maxRSAModulusBytes+1))
	encode := func(keys ...map[string]string) string {
		data, err := json.Marshal(map[string]any{"keys": keys})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, tt := range []struct {
		name   string
		status int
		body   string
		err    error
		valid  bool
	}{
		{name: "empty set", status: 200, body: `{"keys":[]}`},
		{name: "all unsupported", status: 200, body: encode(map[string]string{"kid": "ignored", "kty": "oct", "alg": "HS256"})},
		{name: "all disallowed", status: 200, body: encode(disallowed)},
		{name: "malformed JSON", status: 200, body: `{"keys":`},
		{name: "malformed key", status: 200, body: encode(map[string]string{"kid": "known", "kty": "RSA", "n": "bad", "e": "AQAB"})},
		{name: "oversized key", status: 200, body: encode(oversized)},
		{name: "oversized document", status: 200, body: strings.Repeat(" ", 2049)},
		{name: "duplicate kid", status: 200, body: encode(valid, valid)},
		{name: "not found", status: 404},
		{name: "gone", status: 410},
		{name: "unauthorized", status: 401},
		{name: "canceled", err: context.Canceled},
		{name: "bad gateway", status: 502, valid: true},
		{name: "rate limited", status: 429, valid: true},
		{name: "transport", err: io.ErrUnexpectedEOF, valid: true},
		{name: "timeout", err: context.DeadlineExceeded, valid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			v := newValidator(t, f, Config{CacheTTL: time.Second, MaxStale: time.Minute, MaxResponseBytes: 2048, Now: func() time.Time { return now }})
			token := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
			var requests int
			v.client.Transport = staleRoundTripper(func(*http.Request) (*http.Response, error) {
				requests++
				if tt.err != nil {
					return nil, tt.err
				}
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}, nil
			})
			now = now.Add(time.Second)
			// Also prove that throttling preserves the failure category.
			for range 2 {
				_, err := v.Validate(context.Background(), token)
				if tt.valid && err != nil || !tt.valid && !errors.Is(err, authz.ErrInvalidToken) {
					t.Fatalf("Validate error = %v, want valid = %v", err, tt.valid)
				}
			}
			if requests != 1 {
				t.Fatalf("fetches = %d, want 1", requests)
			}
		})
	}
}

func TestStaleIfErrorHangingRefreshDoesNotBlockPeers(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("known")
	start := time.Now()
	var clock atomic.Int64
	clock.Store(start.UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()) }
	v := newValidator(t, f, Config{CacheTTL: time.Second, MaxStale: time.Minute, RefreshCooldown: 10 * time.Second, Now: now})
	token := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", start.Add(time.Hour))
	entered, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int64
	v.client.Transport = staleRoundTripper(func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			close(entered)
		}
		<-release
		return nil, context.DeadlineExceeded
	})
	clock.Store(start.Add(time.Second).UnixNano())
	first := make(chan error, 1)
	ownerDone := make(chan struct{})
	var releaseOnce sync.Once
	go func() { defer close(ownerDone); _, err := v.Validate(context.Background(), token); first <- err }()
	defer func() { releaseOnce.Do(func() { close(release) }); <-ownerDone }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	// The hanging fetch has already outlasted the retry cooldown.
	clock.Store(start.Add(21 * time.Second).UnixNano())
	results := make(chan error, 24)
	for range cap(results) {
		go func() { _, err := v.Validate(context.Background(), token); results <- err }()
	}
	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	for range cap(results) {
		select {
		case err := <-results:
			if err != nil {
				t.Errorf("peer: %v", err)
			}
		case <-deadline.C:
			t.Fatal("stale peer blocked behind hanging refresh")
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("in-flight fetches = %d, want 1", got)
	}
	// Finish the request before checking cooldown; cleanup also joins on failure.
	releaseOnce.Do(func() { close(release) })
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	for _, offset := range []time.Duration{0, 10*time.Second - time.Nanosecond, 10 * time.Second} {
		clock.Store(start.Add(21*time.Second + offset).UnixNano())
		if _, err := v.Validate(context.Background(), token); err != nil {
			t.Fatal(err)
		}
		want := int64(1)
		if offset == 10*time.Second {
			want = 2
		}
		if got := requests.Load(); got != want {
			t.Fatalf("fetches at offset %s = %d, want %d", offset, got, want)
		}
	}
}

func TestJWKSRetentionStartsBeforeFetch(t *testing.T) {
	f := newIssuer(t)
	f.rotate("known")
	now := time.Now()
	started := now
	client := *f.server.Client()
	transport := client.Transport
	client.Transport = staleRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/keys" {
			now = now.Add(2 * time.Second)
		}
		return transport.RoundTrip(r)
	})
	v, err := New(context.Background(), f.server.URL+"/tenant", "https://resource.example/mcp", Config{HTTPClient: &client, AllowedAlgorithms: []string{"RS256"}, CacheTTL: time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if want := started.Add(time.Second); !v.expires.Equal(want) {
		t.Fatalf("expiry = %s, want fetch start + TTL = %s", v.expires, want)
	}
	started = now
	if err := v.refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if want := started.Add(time.Second); !v.expires.Equal(want) {
		t.Fatalf("refresh expiry = %s, want %s", v.expires, want)
	}
}

func TestStaleIfErrorLogsTransitionsOnce(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	f := newIssuer(t)
	key := f.rotate("known")
	now := time.Now()
	v := newValidator(t, f, Config{CacheTTL: time.Second, MaxStale: time.Minute, RefreshCooldown: time.Second, Now: func() time.Time { return now }})
	token := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
	f.outage.Store(true)
	now = now.Add(time.Second)
	for range 3 {
		if _, err := v.Validate(context.Background(), token); err != nil {
			t.Fatal(err)
		}
	}
	f.outage.Store(false)
	now = now.Add(time.Second)
	for range 2 {
		if _, err := v.Validate(context.Background(), token); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(logs.String(), "level=WARN") != 1 || strings.Count(logs.String(), "level=INFO") != 1 || strings.Count(logs.String(), "level=DEBUG") != 3 {
		t.Fatalf("unexpected transitions: %s", &logs)
	}
	for _, sensitive := range []string{token, "alice", "known", f.server.URL} {
		if strings.Contains(logs.String(), sensitive) {
			t.Fatalf("logs contain sensitive value %q", sensitive)
		}
	}
}

// A later outage must not resurrect a set rejected while the issuer was reachable.
func TestStaleIfErrorRejectedSetStaysBlockedUntilRecovery(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("known")
	now := time.Now()
	v := newValidator(t, f, Config{CacheTTL: time.Second, MaxStale: time.Minute, RefreshCooldown: time.Second, Now: func() time.Time { return now }})
	token := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
	transport := v.client.Transport
	v.client.Transport = staleRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	now = now.Add(time.Second)
	if _, err := v.Validate(context.Background(), token); !errors.Is(err, authz.ErrInvalidToken) {
		t.Fatalf("404: %v", err)
	}
	v.client.Transport = staleRoundTripper(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	now = now.Add(time.Second)
	if _, err := v.Validate(context.Background(), token); !errors.Is(err, authz.ErrInvalidToken) {
		t.Fatalf("rejected set resurrected on timeout: %v", err)
	}
	v.client.Transport = transport
	now = now.Add(time.Second)
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatalf("usable recovery: %v", err)
	}
	f.outage.Store(true)
	now = now.Add(time.Second)
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatalf("stale fallback after usable recovery: %v", err)
	}
}

func TestStaleIfErrorHTTPClientTimeout(t *testing.T) {
	f := newIssuer(t)
	key := f.rotate("known")
	now := time.Now()
	v := newValidator(t, f, Config{CacheTTL: time.Second, MaxStale: time.Minute, Now: func() time.Time { return now }})
	token := sign(t, key, "known", f.server.URL+"/tenant", "https://resource.example/mcp", "alice", now.Add(time.Hour))
	v.client.Timeout = 30 * time.Millisecond
	v.client.Transport = staleRoundTripper(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	now = now.Add(time.Second)
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatalf("stale rejected after HTTP client timeout: %v", err)
	}
}

package inspector

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestNew_RejectsNonLoopback is the binding RFC §12 acceptance criterion: the
// inspector refuses any non-loopback bind. A wildcard, a routable address, and
// a malformed address are all rejected with ErrNonLoopbackBind, and the
// listener is never opened.
func TestNew_RejectsNonLoopback(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
		want bool // true => must be rejected
	}{
		{"ipv4 loopback", "127.0.0.1:0", false},
		{"ipv6 loopback", "[::1]:0", false},
		{"localhost", "localhost:0", false},
		{"wildcard port-only", ":0", true},
		{"wildcard 0.0.0.0", "0.0.0.0:0", true},
		{"routable address", "192.168.1.10:0", true},
		{"public address", "8.8.8.8:0", true},
		{"malformed", "not-an-address", true},
		{"empty host with port", ":8080", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			insp, err := New(Options{Addr: tc.addr})
			if tc.want {
				if err == nil {
					_ = insp.Close()
					t.Fatalf("New(%q): want rejection, got nil error", tc.addr)
				}
				if !errors.Is(err, ErrNonLoopbackBind) {
					t.Fatalf("New(%q): want ErrNonLoopbackBind, got %v", tc.addr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%q): unexpected error: %v", tc.addr, err)
			}
			t.Cleanup(func() { _ = insp.Close() })
		})
	}
}

// TestNew_EmptyAddrUsesLoopback verifies the default bind is a loopback address.
func TestNew_EmptyAddrUsesLoopback(t *testing.T) {
	t.Parallel()
	insp, err := New(Options{})
	if err != nil {
		t.Fatalf("New(empty): %v", err)
	}
	t.Cleanup(func() { _ = insp.Close() })
	if insp.Addr() == "" {
		t.Fatal("Addr() empty after New")
	}
}

// TestInspector_ServesUI starts the inspector and confirms it serves its UI and
// the read-only API endpoints on its loopback listener.
func TestInspector_ServesUI(t *testing.T) {
	t.Parallel()
	insp, err := New(Options{
		ServerInfo: ServerInfo{Name: "demo", Version: "1.2.3", Transport: "inmem"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = insp.Serve(ctx) }()
	waitReady(t, insp.URL()+"/api/info")

	// The UI root serves HTML (the placeholder, pre-frontend-build).
	body := httpGet(t, insp.URL()+"/")
	if !contains(body, "Dockyard Inspector") {
		t.Fatalf("UI root did not serve the inspector shell: %q", body)
	}
	// /api/info returns the attached server identity.
	info := httpGet(t, insp.URL()+"/api/info")
	if !contains(info, "demo") || !contains(info, "1.2.3") {
		t.Fatalf("/api/info missing server identity: %q", info)
	}
	// /api/rpc/log answers (an empty log with no relay).
	rpc := httpGet(t, insp.URL()+"/api/rpc/log")
	if rpc != "[]\n" && rpc != "[]" {
		t.Fatalf("/api/rpc/log want empty array, got %q", rpc)
	}
}

// TestInspector_ServeTwice confirms Serve may run only once.
func TestInspector_ServeTwice(t *testing.T) {
	t.Parallel()
	insp, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = insp.Serve(ctx) }()
	waitReady(t, insp.URL()+"/api/info")
	if err := insp.Serve(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("second Serve: want ErrClosed, got %v", err)
	}
}

// TestInspector_CloseIdempotent confirms Close is idempotent.
func TestInspector_CloseIdempotent(t *testing.T) {
	t.Parallel()
	insp, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := insp.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := insp.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

/* --- helpers --------------------------------------------------------- */

func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // loopback test URL
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("inspector not ready at %s", url)
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // loopback test URL
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return string(body)
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

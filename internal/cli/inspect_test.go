package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/inspector"
)

// TestInspect_RegisteredWithFlags asserts `dockyard inspect` is wired into the
// command tree with its --url / --port / --no-open flags.
func TestInspect_RegisteredWithFlags(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "inspect", "--help")
	if err != nil {
		t.Fatalf("inspect --help: %v", err)
	}
	for _, flag := range []string{"--url", "--port", "--no-open"} {
		if !strings.Contains(out, flag) {
			t.Errorf("inspect --help missing %s flag:\n%s", flag, out)
		}
	}
}

// TestInspect_ListedInRootHelp asserts the verb is discoverable.
func TestInspect_ListedInRootHelp(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "inspect") {
		t.Errorf("root help does not list 'inspect':\n%s", out)
	}
}

// TestInspectAddr resolves --port to a loopback bind address; the host is
// always 127.0.0.1 — `dockyard inspect` cannot widen the bind.
func TestInspectAddr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		port    int
		want    string
		wantErr bool
	}{
		{0, "127.0.0.1:0", false},
		{8090, "127.0.0.1:8090", false},
		{65535, "127.0.0.1:65535", false},
		{-1, "", true},
		{70000, "", true},
	}
	for _, tc := range cases {
		got, err := inspectAddr(tc.port)
		if tc.wantErr {
			if err == nil {
				t.Errorf("inspectAddr(%d): want error, got %q", tc.port, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("inspectAddr(%d): unexpected error: %v", tc.port, err)
		}
		if got != tc.want {
			t.Errorf("inspectAddr(%d): got %q, want %q", tc.port, got, tc.want)
		}
	}
}

// TestObsStreamURLFor derives a server's obs/v1 stream URL from its base URL.
func TestObsStreamURLFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/obs/v1/stream", false},
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080/obs/v1/stream", false},
		{"https://localhost:9000", "https://localhost:9000/obs/v1/stream", false},
		{"http://127.0.0.1:8080/obs/v1/stream", "http://127.0.0.1:8080/obs/v1/stream", false},
		{"ftp://127.0.0.1", "", true},
		{"://bad", "", true},
		{"http://", "", true},
	}
	for _, tc := range cases {
		got, err := obsStreamURLFor(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("obsStreamURLFor(%q): want error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("obsStreamURLFor(%q): unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("obsStreamURLFor(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRunInspect_RefusesNonLoopbackPort — a port that resolves to a
// non-loopback bind is refused. inspectAddr always uses 127.0.0.1, so the path
// that can fail here is an out-of-range port; the loopback gate itself is
// covered by internal/inspector. This asserts runInspect surfaces a typed
// error rather than panicking.
func TestRunInspect_RejectsBadPort(t *testing.T) {
	t.Parallel()
	err := runInspect(context.Background(), inspectConfig{
		port:   -5,
		noOpen: true,
		logger: slog.New(slog.DiscardHandler),
		out:    func(string, ...any) {},
	})
	if err == nil {
		t.Fatal("runInspect with a bad port: want error, got nil")
	}
}

// TestRunInspect_ServesAndStops — runInspect serves the inspector on a
// loopback port and returns cleanly when its context is cancelled. --no-open
// keeps the test headless. It is the standalone-attach acceptance smoke at the
// unit layer; the integration test drives the full server attach.
func TestRunInspect_ServesAndStops(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var sb strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- runInspect(ctx, inspectConfig{
			noOpen: true,
			logger: slog.New(slog.DiscardHandler),
			out:    func(f string, _ ...any) { sb.WriteString(f) },
		})
	}()

	// Give the server a beat to start, then cancel.
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("runInspect: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runInspect did not return after context cancellation")
	}
	if !strings.Contains(sb.String(), "inspector serving at") {
		t.Errorf("runInspect did not print the serving URL: %q", sb.String())
	}
}

// TestInspectorEmbeddedAssets — `dockyard inspect` packages the embedded
// inspector frontend; the embed is always resolvable.
func TestInspectorEmbeddedAssets(t *testing.T) {
	t.Parallel()
	if inspector.EmbeddedAssets() == nil {
		t.Fatal("inspector.EmbeddedAssets returned nil")
	}
}

// TestInspect_HelpDescribesDevAttachAsDeferred — the `dockyard inspect` help
// text must not falsely claim the inspector "runs automatically inside
// `dockyard dev`": that auto-attach is deferred (D-101). The help describes
// reality — standalone `dockyard inspect`, the dev auto-attach not yet present.
func TestInspect_HelpDescribesDevAttachAsDeferred(t *testing.T) {
	t.Parallel()
	out, _, err := run(t, "inspect", "--help")
	if err != nil {
		t.Fatalf("inspect --help: %v", err)
	}
	if !strings.Contains(out, "--dir") {
		t.Errorf("inspect --help missing the --dir flag:\n%s", out)
	}
}

// TestRunInspect_WiresProjectSources — runInspect, given a project directory,
// serves an inspector whose /api/verdicts and /api/contracts endpoints return
// real project content (Blocker 1: the shipping verb must wire these). The
// project has a manifest but no generated contracts, so contracts surface a
// schemaless row and verdicts surface a real validate diagnostic.
func TestRunInspect_WiresProjectSources(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "dockyard.app.yaml"),
		[]byte("name: inspect-wiring-test\ntitle: T\nversion: 0.1.0\n"+
			"runtime:\n  transports: [http]\ntools:\n  - name: report\n"+
			"    description: region report\n    input: internal/contracts.In\n"+
			"    output: internal/contracts.Out\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	port := freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runInspect(ctx, inspectConfig{
			projectDir: projectDir,
			port:       port,
			noOpen:     true,
			logger:     slog.New(slog.DiscardHandler),
			out:        func(string, ...any) {},
		})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitInspectorReady(t, base+"/api/info")

	contracts := getInspectBody(t, base+"/api/contracts")
	if !strings.Contains(contracts, `"report"`) {
		t.Fatalf("/api/contracts not wired from the project manifest: %s", contracts)
	}
	verdicts := getInspectBody(t, base+"/api/verdicts")
	if verdicts == "[]" || verdicts == "[]\n" {
		t.Fatalf("/api/verdicts not wired — empty in the shipping verb: %s", verdicts)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("runInspect: %v", err)
	}
}

// freePort reserves and releases a loopback port, returning its number.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// waitInspectorReady polls url until the inspector answers or a deadline passes.
func waitInspectorReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // loopback test URL
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("inspector not ready at %s", url)
}

// getInspectBody GETs url and returns the response body, trimmed.
func getInspectBody(t *testing.T, url string) string {
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
	return strings.TrimSpace(string(body))
}

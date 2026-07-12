package integration_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/scaffold"
)

// TestPhase35ScaffoldDualHTTPLifecycle builds a generated blank project and
// proves its real HTTP process accepts both modern discovery and legacy
// initialize on the same listener.
func TestPhase35ScaffoldDualHTTPLifecycle(t *testing.T) {
	root := phase35RepoRoot(t)
	res, err := scaffold.Generate(scaffold.Options{
		Name:            "phase35-dual",
		Dir:             t.TempDir(),
		DockyardReplace: root,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	runPhase35Command(t, res.Dir, "go", "mod", "tidy")
	bin := filepath.Join(res.Dir, "phase35-dual")
	runPhase35Command(t, res.Dir, "go", "build", "-o", bin, ".")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin) //nolint:gosec // generated test binary
	cmd.Dir = res.Dir
	cmd.Env = append(os.Environ(), "DOCKYARD_TRANSPORT=http", "DOCKYARD_HTTP_ADDR="+addr)
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start generated server: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	endpoint := "http://" + addr
	var session *mcpsdk.ClientSession
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "phase35-test", Version: "1"}, nil)
		session, err = client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: endpoint}, nil)
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("modern server/discover connection: %v\nserver stderr:\n%s", err, stderr.String())
	}
	if got := session.InitializeResult().ProtocolVersion; got != "2026-07-28" {
		t.Errorf("modern protocol version = %q, want 2026-07-28", got)
	}
	_ = session.Close()

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy-test","version":"1"}}}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, body)
	if err != nil {
		t.Fatalf("legacy request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy initialize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Mcp-Session-Id") == "" {
		t.Fatalf("legacy initialize status/session/body = %d/%q/%s", resp.StatusCode, resp.Header.Get("Mcp-Session-Id"), raw)
	}
}

func phase35RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func runPhase35Command(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...) //nolint:gosec // test helper runs fixed Go toolchain commands in a temporary scaffold
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

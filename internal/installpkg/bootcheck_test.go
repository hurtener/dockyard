package installpkg

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/scaffold"
)

func TestBootCheckNegotiation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		discoverResult func() (mcpsdk.Result, error)
		wantErr        bool
		wantInitialize bool
	}{
		{
			name: "server discover succeeds without initialize",
			discoverResult: func() (mcpsdk.Result, error) {
				return &mcpsdk.DiscoverResult{
					SupportedVersions: []string{"2026-07-28"},
					Capabilities:      &mcpsdk.ServerCapabilities{},
					ServerInfo:        &mcpsdk.Implementation{Name: "modern", Version: "1"},
				}, nil
			},
		},
		{
			name: "recognized method-not-found falls back to initialize",
			discoverResult: func() (mcpsdk.Result, error) {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "method not found"}
			},
			wantInitialize: true,
		},
		{
			name: "advertised legacy-only version falls back to initialize",
			discoverResult: func() (mcpsdk.Result, error) {
				return &mcpsdk.DiscoverResult{
					SupportedVersions: []string{"2025-11-25"},
					Capabilities:      &mcpsdk.ServerCapabilities{},
					ServerInfo:        &mcpsdk.Implementation{Name: "legacy", Version: "1"},
				}, nil
			},
			wantInitialize: true,
		},
		{
			name: "unknown future advertised version does not downgrade",
			discoverResult: func() (mcpsdk.Result, error) {
				return &mcpsdk.DiscoverResult{
					SupportedVersions: []string{"2027-01-01"},
					Capabilities:      &mcpsdk.ServerCapabilities{},
					ServerInfo:        &mcpsdk.Implementation{Name: "future", Version: "1"},
				}, nil
			},
			wantErr: true,
		},
		{
			name: "malformed advertised version does not downgrade",
			discoverResult: func() (mcpsdk.Result, error) {
				return &mcpsdk.DiscoverResult{
					SupportedVersions: []string{"garbage"},
					Capabilities:      &mcpsdk.ServerCapabilities{},
					ServerInfo:        &mcpsdk.Implementation{Name: "malformed", Version: "1"},
				}, nil
			},
			wantErr: true,
		},
		{
			name: "unrelated discovery error does not downgrade",
			discoverResult: func() (mcpsdk.Result, error) {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "discovery backend unavailable"}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var discoverCalls, initializeCalls atomic.Int32
			srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
			srv.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
				return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
					switch method {
					case "server/discover":
						discoverCalls.Add(1)
						return tt.discoverResult()
					case "initialize":
						initializeCalls.Add(1)
					}
					return next(ctx, method, req)
				}
			})
			clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
			serverSession, err := srv.Connect(context.Background(), serverTransport, nil)
			if err != nil {
				t.Fatalf("server Connect: %v", err)
			}
			t.Cleanup(func() { _ = serverSession.Close() })

			client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "bootcheck", Version: "1"}, nil)
			session, err := client.Connect(context.Background(), &modernFirstTransport{base: clientTransport}, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Client.Connect error = %v, wantErr %v", err, tt.wantErr)
			}
			if session != nil {
				_ = session.Close()
			}
			if discoverCalls.Load() != 1 {
				t.Errorf("server/discover calls = %d, want 1", discoverCalls.Load())
			}
			if got := initializeCalls.Load() > 0; got != tt.wantInitialize {
				t.Errorf("initialize called = %v, want %v", got, tt.wantInitialize)
			}
		})
	}
}

// repoRoot returns the Dockyard repository root — two directories up from this
// test file (internal/installpkg/<file>).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the test file path")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// buildRealServer scaffolds a project and `go build`s it, returning the
// project directory and the built server binary path. The built binary is a
// real Dockyard MCP server serving stdio — the input the boot check verifies.
func buildRealServer(t *testing.T, name string) (projectDir, binaryPath string) {
	t.Helper()
	parent := t.TempDir()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            name,
		Dir:             parent,
		DockyardReplace: repoRoot(t),
	})
	if err != nil {
		t.Fatalf("scaffold.Generate: %v", err)
	}
	tidy := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	binaryPath = filepath.Join(res.Dir, "server")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	build := exec.Command("go", "build", "-o", binaryPath, ".") //nolint:gosec // binaryPath is a test temp path
	build.Dir = res.Dir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build server: %v\n%s", err, out)
	}
	return res.Dir, binaryPath
}

// TestBootCheck_PassesForARealServer verifies the boot check completes a real
// modern MCP discovery against a freshly built Dockyard server.
func TestBootCheck_PassesForARealServer(t *testing.T) {
	t.Parallel()
	_, binaryPath := buildRealServer(t, "bc-ok")
	if err := bootCheck(context.Background(), binaryPath); err != nil {
		t.Errorf("boot check failed for a real server: %v", err)
	}
}

// TestBootCheck_FailsForANonServer verifies the boot check fails cleanly when
// the binary is not an MCP server (it does not speak the protocol).
func TestBootCheck_FailsForANonServer(t *testing.T) {
	t.Parallel()
	// A trivial program that exits immediately — never completes a handshake.
	dir := t.TempDir()
	src := "package main\nfunc main() {}\n"
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "noserver")
	build := exec.Command("go", "build", "-o", bin, srcPath) //nolint:gosec // test temp paths
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build non-server: %v\n%s", err, out)
	}
	err := bootCheck(context.Background(), bin)
	if err == nil {
		t.Fatal("boot check passed for a non-server binary")
	}
	if !errors.Is(err, ErrBootCheck) {
		t.Errorf("boot-check failure not wrapping ErrBootCheck: %v", err)
	}
}

func TestBootCheckClosesChildAfterHandshakeError(t *testing.T) {
	cleanupFile := filepath.Join(t.TempDir(), "child-cleaned-up")
	t.Setenv("DOCKYARD_BOOTCHECK_TEST_CLEANUP", cleanupFile)
	t.Setenv("DOCKYARD_BOOTCHECK_TEST_HANG", "")
	bin := buildBootCheckProbe(t)

	if err := bootCheck(context.Background(), bin); err == nil {
		t.Fatal("boot check accepted unsupported initialize protocol")
	}
	if _, err := os.Stat(cleanupFile); err != nil {
		t.Fatalf("child did not observe connection cleanup: %v", err)
	}
}

func TestBootCheckBoundsUncooperativeChildTermination(t *testing.T) {
	t.Setenv("DOCKYARD_BOOTCHECK_TEST_CLEANUP", "")
	t.Setenv("DOCKYARD_BOOTCHECK_TEST_HANG", "1")
	bin := buildBootCheckProbe(t)

	done := make(chan error, 1)
	go func() {
		done <- bootCheck(context.Background(), bin)
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(bootCheckTimeout):
		t.Fatalf("uncooperative child cleanup exceeded production boot-check timeout %v", bootCheckTimeout)
	}
	if err == nil {
		t.Fatal("boot check accepted unsupported initialize protocol")
	}
}

func buildBootCheckProbe(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `package main

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID any ` + "`json:\"id\"`" + `
			Method string ` + "`json:\"method\"`" + `
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			continue
		}
		var result any
		switch req.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{"2025-11-25"}, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "probe", "version": "1"}}
		case "initialize":
			result = map[string]any{"protocolVersion": "unsupported", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "probe", "version": "1"}}
		default:
			continue
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if req.Method == "initialize" && os.Getenv("DOCKYARD_BOOTCHECK_TEST_HANG") != "" {
			for { time.Sleep(time.Hour) }
		}
	}
	if path := os.Getenv("DOCKYARD_BOOTCHECK_TEST_CLEANUP"); path != "" {
		_ = os.WriteFile(path, []byte("closed"), 0600)
	}
}
`
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	build := exec.Command("go", "build", "-o", bin, srcPath) //nolint:gosec // test temp paths
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build boot-check probe: %v\n%s", err, out)
	}
	return bin
}

// TestInstall_FullWithBootCheck exercises the full Install path including the
// boot check against a real built server — the binding "install confirms the
// server boots" acceptance criterion, at the package level.
func TestInstall_FullWithBootCheck(t *testing.T) {
	t.Parallel()
	projectDir, binaryPath := buildRealServer(t, "bc-install")
	configPath := filepath.Join(t.TempDir(), "claude_desktop_config.json")

	res, err := Install(context.Background(), Options{
		ProjectDir: projectDir,
		Host:       HostClaude,
		ConfigPath: configPath,
		BinaryPath: binaryPath,
	})
	if err != nil {
		t.Fatalf("Install with boot check: %v", err)
	}
	if !res.BootOK {
		t.Error("Install boot check did not pass for a real server")
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("host config not written: %v", err)
	}
}

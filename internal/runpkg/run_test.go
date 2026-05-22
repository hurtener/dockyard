package runpkg

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/scaffold"
)

// TestParseTransport verifies transport-string parsing: stdio/http accepted,
// an empty value defaults to stdio, an unknown value is a clear ErrRun.
func TestParseTransport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    Transport
		wantErr bool
	}{
		{"", TransportStdio, false},
		{"stdio", TransportStdio, false},
		{"http", TransportHTTP, false},
		{"grpc", "", true},
		{"STDIO", "", true},
	}
	for _, tt := range tests {
		got, err := ParseTransport(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseTransport(%q): want an error", tt.in)
			} else if !errors.Is(err, ErrRun) {
				t.Errorf("ParseTransport(%q) error not wrapping ErrRun: %v", tt.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTransport(%q): unexpected error %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ParseTransport(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestRun_RequiresProjectDir verifies an empty ProjectDir is a clear ErrRun.
func TestRun_RequiresProjectDir(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), Options{})
	if err == nil || !errors.Is(err, ErrRun) {
		t.Errorf("empty ProjectDir: want an ErrRun, got %v", err)
	}
}

// buildLongRunningStub compiles a tiny Go program that blocks until killed —
// it stands in for a built MCP server so serveBinary's supervision behaviour
// is exercised without a real server build.
func buildLongRunningStub(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("process-group teardown stub assumes POSIX signals")
	}
	dir := t.TempDir()
	src := `package main
import (
	"os"
	"os/signal"
	"syscall"
)
func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, os.Interrupt)
	<-c
}
`
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write stub source: %v", err)
	}
	bin := filepath.Join(dir, "stub")
	cmd := exec.Command("go", "build", "-o", bin, srcPath) //nolint:gosec // bin/srcPath are test temp-dir paths
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stub: %v\n%s", err, out)
	}
	return bin
}

// TestServeBinary_CleanShutdownOnContextCancel verifies serveBinary tears the
// server child down cleanly when the context is cancelled — no hang, no orphan.
func TestServeBinary_CleanShutdownOnContextCancel(t *testing.T) {
	t.Parallel()
	bin := buildLongRunningStub(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- serveBinary(ctx, bin, filepath.Dir(bin), TransportStdio, "", nil)
	}()

	// Give the child a moment to start, then cancel.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serveBinary returned an error on clean cancellation: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveBinary did not return after context cancellation — possible orphan")
	}
}

// TestServeBinary_ReportsStartFailure verifies serveBinary returns a clear
// ErrRun when the binary cannot be started.
func TestServeBinary_ReportsStartFailure(t *testing.T) {
	t.Parallel()
	err := serveBinary(context.Background(),
		filepath.Join(t.TempDir(), "does-not-exist"), t.TempDir(), TransportStdio, "", nil)
	if err == nil || !errors.Is(err, ErrRun) {
		t.Errorf("start of a missing binary: want an ErrRun, got %v", err)
	}
}

// TestServeBinary_PassesTransportEnv verifies the transport selection reaches
// the child through the DOCKYARD_TRANSPORT environment variable.
func TestServeBinary_PassesTransportEnv(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("stub assumes a POSIX shell-free Go child")
	}
	dir := t.TempDir()
	// A stub that writes its DOCKYARD_TRANSPORT to a file then exits cleanly.
	out := filepath.Join(dir, "transport.txt")
	src := `package main
import "os"
func main() {
	_ = os.WriteFile(` + "`" + out + "`" + `, []byte(os.Getenv("DOCKYARD_TRANSPORT")), 0o600)
}
`
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "stub")
	build := exec.Command("go", "build", "-o", bin, srcPath) //nolint:gosec // bin/srcPath are test temp-dir paths
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stub: %v\n%s", err, b)
	}

	if err := serveBinary(context.Background(), bin, dir, TransportHTTP, ":9999", nil); err != nil {
		t.Fatalf("serveBinary: %v", err)
	}
	got, err := os.ReadFile(out) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read transport file: %v", err)
	}
	if string(got) != string(TransportHTTP) {
		t.Errorf("child saw DOCKYARD_TRANSPORT=%q, want %q", got, TransportHTTP)
	}
}

// repoRoot returns the Dockyard repository root — two directories up from this
// test file.
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

// TestRun_BuildsAndServesThenStopsCleanly exercises the full Run path — build
// the project, serve it on stdio, then a context cancel tears it down — against
// a real scaffolded project.
func TestRun_BuildsAndServesThenStopsCleanly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("process-group teardown assumes POSIX signals")
	}
	parent := t.TempDir()
	res, err := scaffold.Generate(scaffold.Options{
		Name:            "rp-run",
		Dir:             parent,
		DockyardReplace: repoRoot(t),
	})
	if err != nil {
		t.Fatalf("scaffold.Generate: %v", err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = res.Dir
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{ProjectDir: res.Dir, Transport: TransportStdio})
	}()
	// Wait until Run has finished building and started serving — the built
	// binary appears in dist/ — then give the server a moment to start before
	// cancelling. Polling avoids racing the build, which takes several seconds.
	binName := "rp-run-" + runtime.GOOS + "-" + runtime.GOARCH
	builtBin := filepath.Join(res.Dir, "dist", binName)
	deadline := time.Now().Add(45 * time.Second)
	for {
		if _, statErr := os.Stat(builtBin); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("Run did not produce the built binary within the deadline")
		}
		time.Sleep(100 * time.Millisecond)
	}
	// The binary exists; give the server child a moment to come up, then stop.
	time.Sleep(1 * time.Second)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned an error on clean cancellation: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

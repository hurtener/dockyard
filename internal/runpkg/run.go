package runpkg

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hurtener/dockyard/internal/buildpkg"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/scaffold"
)

// ErrRun is the sentinel wrapping a `dockyard run` failure. Callers branch
// with errors.Is(err, ErrRun).
var ErrRun = errors.New("dockyard/internal/runpkg: run failed")

// stopGrace is how long the server child is given to exit after a SIGTERM
// before it is force-killed — the same discipline internal/devloop uses.
const stopGrace = 3 * time.Second

// Transport is the deployment mode `dockyard run` serves the project's MCP
// server over (RFC §5.2, §14). V1 supports stdio and http.
type Transport string

const (
	// TransportStdio is the local single-user subprocess transport — the
	// default `dockyard run` mode.
	TransportStdio Transport = "stdio"
	// TransportHTTP is the streamable-HTTP transport.
	TransportHTTP Transport = "http"
)

// ParseTransport validates a transport flag value and returns the typed
// Transport. An empty value defaults to stdio (the local mode); an unknown
// value is a clear, typed error.
func ParseTransport(s string) (Transport, error) {
	switch Transport(s) {
	case "":
		return TransportStdio, nil
	case TransportStdio:
		return TransportStdio, nil
	case TransportHTTP:
		return TransportHTTP, nil
	default:
		return "", fmt.Errorf("%w: unknown transport %q — use 'stdio' or 'http'", ErrRun, s)
	}
}

// Options configures one `dockyard run` invocation.
type Options struct {
	// ProjectDir is the root of the Dockyard project — the directory holding
	// dockyard.app.yaml. Required.
	ProjectDir string
	// Transport is the deployment mode to serve over. The zero value ("")
	// defaults to stdio.
	Transport Transport
	// Addr is the listen address for the HTTP transport (e.g.
	// "127.0.0.1:8080"). Ignored for stdio. An empty value with TransportHTTP
	// defaults to defaultHTTPAddr.
	Addr string
	// Logger receives the run's structured output. A nil Logger falls back to
	// a discarding logger so Run never panics on a missing logger.
	Logger *slog.Logger
}

// defaultHTTPAddr is the HTTP listen address used when Options.Addr is empty.
// It is localhost-bound, not ":8080": a `dockyard run --transport http` with no
// explicit --addr must not silently widen the scaffolded server's secure
// localhost default to every interface. This matches the scaffold's own
// httpAddr default (internal/scaffold main.go) and D-090.
const defaultHTTPAddr = "127.0.0.1:8080"

// envTransport / envHTTPAddr are the environment variables the server child
// reads to select its transport. The project's main.go owns the wiring; runpkg
// only passes the selection — it never reimplements a transport (RFC §5.2).
const (
	envTransport = "DOCKYARD_TRANSPORT"
	envHTTPAddr  = "DOCKYARD_HTTP_ADDR"
)

// Run builds the project's MCP server (a host-only `dockyard build`) and runs
// the produced binary as a supervised child process on the chosen transport.
// It blocks until ctx is cancelled, at which point the child is torn down
// cleanly (SIGTERM, then SIGKILL after stopGrace) — no orphan process.
//
// Run reuses internal/buildpkg for the build so a `dockyard run` always serves
// a freshly-built, validated, CGo-free binary; a validation blocker fails the
// run before the server is ever started.
func Run(ctx context.Context, opts Options) error {
	logger := opts.logger()

	if opts.ProjectDir == "" {
		return fmt.Errorf("%w: ProjectDir is required", ErrRun)
	}
	transport := opts.Transport
	if transport == "" {
		transport = TransportStdio
	}
	addr := opts.Addr
	if transport == TransportHTTP && addr == "" {
		addr = defaultHTTPAddr
	}

	// The manifest-vs-main.go auto-wire detection (D-164): if the manifest
	// declares any tool with task_support: optional or required but the
	// project's main.go source does not appear to attach a Tasks engine,
	// surface a warning. The run still proceeds — the project's main is
	// authoritative; the warning is a heads-up that the engine the
	// manifest implies is not wired.
	auditAutoWire(ctx, opts.ProjectDir, logger)

	// Build the project host-only — a fresh, validated, CGo-free binary.
	logger.InfoContext(ctx, "run: building project")
	res, err := buildpkg.Build(ctx, buildpkg.Options{
		ProjectDir: opts.ProjectDir,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("%w: build before run: %w", ErrRun, err)
	}
	if len(res.Artifacts) != 1 {
		return fmt.Errorf("%w: host build produced %d artifacts, expected 1",
			ErrRun, len(res.Artifacts))
	}
	binPath := res.Artifacts[0].Path

	return serveBinary(ctx, binPath, opts.ProjectDir, transport, addr, logger)
}

// serveBinary runs the built server binary as a supervised child process on
// the chosen transport, blocking until ctx is cancelled. It is split out so a
// test can drive the supervision behaviour against a stub binary.
func serveBinary(ctx context.Context, binPath, projectDir string,
	transport Transport, addr string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("%w: resolve project dir: %w", ErrRun, err)
	}

	// The child lifecycle is explicit (not exec.CommandContext): a
	// context-killed child SIGKILLs immediately, denying the server its clean
	// stdio flush. Run watches ctx and does an ordered SIGTERM→SIGKILL stop.
	cmd := exec.Command(binPath) //nolint:gosec // binPath is a Dockyard-built artifact
	cmd.Dir = absProject
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		envTransport+"="+string(transport),
		envHTTPAddr+"="+addr,
	)
	setProcessGroup(cmd)

	logger.InfoContext(ctx, "run: starting server",
		slog.String("transport", string(transport)), slog.String("addr", addr))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%w: start server %s: %w", ErrRun, binPath, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		logger.InfoContext(ctx, "run: shutdown requested — stopping server")
		stopChild(cmd, done)
		return nil
	case waitErr := <-done:
		if waitErr != nil {
			return fmt.Errorf("%w: server exited: %w", ErrRun, waitErr)
		}
		logger.InfoContext(ctx, "run: server exited cleanly")
		return nil
	}
}

// stopChild terminates the server child with a SIGTERM, escalating to SIGKILL
// if the child has not exited within stopGrace — so `dockyard run` never
// leaves an orphan process.
func stopChild(cmd *exec.Cmd, done <-chan error) {
	terminateGroup(cmd)
	select {
	case <-done:
	case <-time.After(stopGrace):
		killGroup(cmd)
		<-done
	}
}

// logger returns opts.Logger or a discarding logger.
func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// auditAutoWire checks the project's manifest against its main.go source and
// emits a warning (D-164) when the manifest declares task-supporting tools
// but the source does not appear to attach a Tasks engine via
// server.Options{Tasks: …}.
//
// The check is best-effort: it loads the manifest, then greps main.go for
// the Tasks attachment shape. A malformed manifest, a missing main.go, or
// any other read error is silently ignored — the run still proceeds and the
// real error (if any) surfaces from the build step. The audit's job is to
// help; it must never fail a run on its own.
//
// The heuristic is intentionally conservative: it matches "server.Options{"
// followed (across a few lines) by "Tasks:" or matches the imperative
// "WithTasks(" call. False negatives (a project that wires the engine
// through an out-of-band path) are acceptable; false positives (warning
// the user about a non-issue) are not.
func auditAutoWire(ctx context.Context, projectDir string, logger *slog.Logger) {
	manifestPath := filepath.Join(projectDir, manifest.DefaultFilename)
	m, err := manifest.LoadFile(manifestPath)
	if err != nil {
		return
	}
	if !scaffold.RequiresTasksEngine(m) {
		return
	}
	mainPath := filepath.Join(projectDir, "main.go")
	src, err := os.ReadFile(mainPath) //nolint:gosec // path is a fixed project-relative file under a caller-supplied project dir
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		return
	}
	if mainGoWiresTasks(string(src)) {
		return
	}
	logger.WarnContext(ctx,
		"run: manifest declares task-supporting tools but main.go does not appear to attach a Tasks engine — "+
			"`dockyard new` auto-wires the engine for projects that declare task_support: optional or required; "+
			"see the approval-flows template for the hand-written wiring shape (D-164)",
		slog.String("manifest", manifestPath),
		slog.String("main", mainPath),
	)
}

// mainGoWiresTasks reports whether src appears to attach a Tasks engine to
// the server — either the option-struct shape (server.Options{...Tasks:})
// or the imperative WithTasks call. It is the heuristic the auto-wire
// audit consults.
func mainGoWiresTasks(src string) bool {
	if strings.Contains(src, ".WithTasks(") {
		return true
	}
	// Tolerate intervening whitespace + lines between "server.Options{" and
	// "Tasks:". The structured-option form is the dominant shape the
	// scaffold emits; this is a per-line check that walks the option-struct
	// literal.
	idx := strings.Index(src, "server.Options{")
	for idx >= 0 {
		// Look for the closing brace of this literal; "Tasks:" must appear
		// before it for the wiring to count.
		end := strings.Index(src[idx:], "}")
		if end < 0 {
			return false
		}
		segment := src[idx : idx+end]
		if strings.Contains(segment, "Tasks:") {
			return true
		}
		// Move past this literal and look for another, in case the file
		// constructs two server.Options literals (unusual but valid).
		idx = strings.Index(src[idx+end:], "server.Options{")
		if idx >= 0 {
			idx += end
		}
	}
	return false
}

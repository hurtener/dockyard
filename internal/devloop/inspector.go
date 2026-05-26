package devloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"

	"github.com/hurtener/dockyard/internal/inspector"
)

// defaultInspectorAddr is the inspector's loopback bind when the caller does
// not pass an override. An empty host or a wildcard address is refused by
// internal/inspector.requireLoopback before the listener opens; the
// OS-assigned port (`:0` shorthand) is the safe default for the dev loop —
// a developer who already pinned another inspector to a fixed port does not
// collide with this one.
const defaultInspectorAddr = "127.0.0.1:0"

// inspectorChild is the dev loop's supervised inspector instance. Unlike the
// Go-server and Vite supervisors (which spawn subprocesses), the inspector is
// hosted IN-PROCESS — internal/inspector is an importable Go package, so the
// dev loop builds and serves an Inspector directly rather than re-execing
// `bin/dockyard inspect`. The in-process choice keeps the dev loop a single
// binary (no second `bin/dockyard` resolution, no transitive PATH ordering),
// and lets the inspector lifecycle ride on the dev session's context exactly
// like the file watcher. (See decision D-162 for the rationale.)
//
// inspectorChild is concurrency-safe: Start, Stop, and the running probe may
// be called from different goroutines. It is single-use — a Stop'd instance
// is not restarted; the dev loop tears the whole tree down on cancellation.
type inspectorChild struct {
	logger *slog.Logger

	// serverURL is the attached server's MCP base URL — empty when the
	// dev loop has not yet brought the Go server up on an HTTP transport.
	// The inspector still runs (relays an empty obs stream, renders the
	// project's contracts / verdicts), and the user gets the same useful
	// rail tabs against a stdio-only server.
	serverURL  string
	projectDir string
	addr       string

	mu       sync.Mutex
	insp     *inspector.Inspector
	cancel   context.CancelFunc
	doneCh   chan struct{}
	startErr error
}

// newInspectorChild builds an inspector child for the dev session. The
// caller drives the lifecycle with Start / Stop; Run is internal.
//
// addr is the inspector's loopback bind address; empty selects
// [defaultInspectorAddr] (an OS-assigned loopback port). A non-loopback
// addr is rejected by internal/inspector.requireLoopback when Start opens
// the listener — the dev loop reports the typed error and the rest of the
// supervisor tree stays up.
func newInspectorChild(logger *slog.Logger, addr, serverURL, projectDir string) *inspectorChild {
	if addr == "" {
		addr = defaultInspectorAddr
	}
	return &inspectorChild{
		logger:     logger,
		addr:       addr,
		serverURL:  serverURL,
		projectDir: projectDir,
	}
}

// Start opens the inspector's loopback listener and runs its HTTP server in
// a background goroutine. It returns once the listener is open and the
// inspector has its OS-assigned port — so the caller can print the URL
// straight after Start returns. A listener-bind failure is returned
// synchronously and the dev loop reports it; the Go server + Vite stay up.
func (c *inspectorChild) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.insp != nil {
		return errors.New("devloop: inspector child already started")
	}

	opts, err := buildInspectorOptions(c.addr, c.serverURL, c.projectDir, c.logger)
	if err != nil {
		c.startErr = err
		return err
	}
	insp, err := inspector.New(opts)
	if err != nil {
		c.startErr = err
		return fmt.Errorf("devloop: inspector listen: %w", err)
	}
	c.insp = insp

	// The inspector serves until its context is cancelled or Close is
	// called. We derive a fresh context so the supervisor can stop the
	// inspector independently of the dev session's overall context — the
	// teardown ordering (children first, watcher last) matches the
	// Go-server and Vite supervisors.
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.cancel = cancel
	c.doneCh = done
	// The goroutine captures locals (insp, done) so a concurrent Stop
	// nilling c.insp / c.doneCh cannot race with the serve goroutine
	// reading those fields — race detector friendly (`-race`).
	go func() {
		defer close(done)
		if serveErr := insp.Serve(runCtx); serveErr != nil {
			// Serve only returns a non-nil error on a true failure (a
			// shutdown returns nil). Report it; the rest of the tree
			// stays up.
			c.logger.ErrorContext(ctx, "inspector serve",
				slog.String("error", serveErr.Error()))
		}
	}()
	c.logger.InfoContext(ctx, "inspector ready",
		slog.String("url", insp.URL()))
	return nil
}

// URL returns the inspector's resolved URL — populated after a successful
// Start. Empty if Start has not yet run or has failed.
func (c *inspectorChild) URL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.insp == nil {
		return ""
	}
	return c.insp.URL()
}

// Stop tears the inspector down cleanly. It is idempotent and safe to call
// even if Start failed.
func (c *inspectorChild) Stop() {
	c.mu.Lock()
	insp := c.insp
	cancel := c.cancel
	done := c.doneCh
	c.insp = nil
	c.cancel = nil
	c.doneCh = nil
	c.mu.Unlock()

	if insp == nil {
		return
	}
	// Close stops the HTTP listener; Serve unblocks once Close returns.
	_ = insp.Close()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// buildInspectorOptions assembles the [inspector.Options] for the dev loop's
// in-process inspector. The wiring mirrors `cmd/inspect`'s runInspect path
// (D-099 relay; D-103 App source; D-131 invoker; D-134 elicitor; D-144
// framing) so a dev-loop inspector and a standalone `dockyard inspect`
// surface the same panels against the same project.
//
// A nil serverURL is tolerated: the inspector still runs, its project
// sources (Verdicts, Contracts, on-disk Fixtures) drive the rail tabs, and
// its relay / App / invoke / elicit / prompt sources fall back to their
// empty-source states. A developer with a stdio-only server gets exactly
// the same useful local surface.
func buildInspectorOptions(addr, serverURL, projectDir string, logger *slog.Logger) (inspector.Options, error) {
	opts := inspector.Options{
		Addr:       addr,
		Assets:     inspector.EmbeddedAssets(),
		ServerInfo: inspector.ServerInfo{Name: "inspector", Transport: "detached"},
		Logger:     logger,
	}
	if projectDir != "" {
		opts.Verdicts = inspector.VerdictsFromValidate(projectDir)
		opts.Contracts = inspector.ContractsFromProject(projectDir)
		opts.Fixtures = inspector.FixturesFromDir(projectDir)
	}
	if serverURL != "" {
		obsURL, err := obsStreamURLFor(serverURL)
		if err != nil {
			return inspector.Options{}, err
		}
		opts.Relay = inspector.NewRelay(obsURL)
		opts.Apps = inspector.AppsFromServer(serverURL)
		opts.Invoker = inspector.ToolsFromServer(serverURL)
		opts.Elicitor = inspector.ElicitationFromServer(serverURL)
		promptSrc, promptInv := inspector.PromptsFromServer(serverURL)
		opts.Prompts = promptSrc
		opts.PromptInvoker = promptInv
		opts.ServerInfo = inspector.ServerInfo{Name: serverURL, Transport: "http"}
	}
	return opts, nil
}

// obsStreamURLFor mirrors internal/cli.obsStreamURLFor — it derives the
// obs/v1 SSE stream URL from a bare MCP base URL. Duplicated here rather
// than imported because internal/cli depends on internal/devloop (for the
// dev command); reversing the dependency would create a cycle.
func obsStreamURLFor(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("devloop: inspector: parse server URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("devloop: inspector: server URL %q must be http(s)", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("devloop: inspector: server URL %q is missing a host", raw)
	}
	if strings.Contains(u.Path, "/obs/") {
		return u.String(), nil
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/obs/v1/stream"
	return u.String(), nil
}

package devloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
)

// Config configures one dev-orchestrator run.
type Config struct {
	// ProjectDir is the scaffolded project root — the directory holding
	// dockyard.app.yaml. Required.
	ProjectDir string

	// Logger receives the orchestrator's dev output. Required-ish: a nil
	// Logger falls back to a discarding logger so Run never panics on a
	// missing logger, but a caller should pass the dev-mode text handler.
	Logger *slog.Logger

	// Debounce is the file-event coalescing window. Zero uses defaultDebounce.
	Debounce time.Duration

	// GoServerCommand overrides the default `go run .` for the supervised Go
	// MCP server. Empty uses the default. It is the test/seam injection point:
	// the integration test injects a controllable stub so it does not need to
	// build a real server on every restart.
	GoServerCommand []string

	// ViteCommand overrides the default `npm run dev` for the supervised Vite
	// dev server. Empty uses the default. Test/seam injection point.
	ViteCommand []string

	// SkipCodegen disables the in-process codegen step on a contract change.
	// It exists for tests that exercise the watch+restart choreography without
	// a full Go toolchain codegen run; production always leaves it false.
	SkipCodegen bool

	// hooks, when non-nil, receives lifecycle notifications. Test-only — it is
	// unexported so it is not part of the public seam.
	hooks *hooks
}

// hooks is the test observation seam: deterministic signals an integration
// test waits on instead of sleeping.
type hooks struct {
	// onServerRestart fires after the Go server has been (re)started.
	onServerRestart func()
	// onCodegen fires after an in-process codegen run, with its error (if any).
	onCodegen func(error)
	// onReady fires once the initial process tree is up.
	onReady func()
}

// errFatal wraps an error that ends the dev session. A child crash is not
// fatal (the loop reports and survives); a watcher failure or an inability to
// start the initial tree is.
var errFatal = errors.New("devloop: fatal")

// Run starts the dev orchestrator and blocks until ctx is cancelled or a fatal
// error occurs. On return the whole process tree — the Go server, Vite, and
// the file watcher — has been torn down: no orphan processes, no leaked
// goroutines, no leaked ports.
//
// Run is safe to call once per Config and holds no global state. A
// child-process crash is reported through the logger and the loop survives; a
// context cancellation is a clean, non-error shutdown (Run returns nil).
func Run(ctx context.Context, cfg Config) error {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	if cfg.ProjectDir == "" {
		return fmt.Errorf("%w: ProjectDir is required", errFatal)
	}
	projectDir, err := filepath.Abs(cfg.ProjectDir)
	if err != nil {
		return fmt.Errorf("%w: resolve project dir: %w", errFatal, err)
	}
	if info, statErr := os.Stat(filepath.Join(projectDir, manifest.DefaultFilename)); statErr != nil || info.IsDir() {
		return fmt.Errorf("%w: %s not found in %s — is this a Dockyard project?",
			errFatal, manifest.DefaultFilename, projectDir)
	}

	o := &orchestrator{
		projectDir: projectDir,
		logger:     logger,
		cfg:        cfg,
	}
	return o.run(ctx)
}

// orchestrator holds one dev session's state. It is not reused across Run
// calls.
type orchestrator struct {
	projectDir string
	logger     *slog.Logger
	cfg        Config

	// ctx is the dev session's context — the derived, cancellable context
	// run() builds. It is held on the struct so every log call can use the
	// context-aware slog API (the repo's logging convention) without threading
	// ctx through every helper signature.
	ctx context.Context //nolint:containedctx // a per-run session object; not reused

	goServer *supervisor
	vite     *supervisor
}

// run is the orchestrator's lifecycle: bring up the process tree, drive the
// watch loop, and tear everything down on exit.
func (o *orchestrator) run(ctx context.Context) error {
	// A derived context so a fatal internal error cancels the watcher and the
	// children just as a Ctrl-C would. It is held on the struct so every log
	// call uses the context-aware slog API.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	o.ctx = ctx

	// --- watcher ----------------------------------------------------------
	w, err := newWatcher(o.projectDir, generate.ContractsDir, o.cfg.Debounce, o.logger)
	if err != nil {
		return fmt.Errorf("%w: %w", errFatal, err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.run(ctx)
	}()

	// --- Go server supervisor --------------------------------------------
	o.goServer = newSupervisor(ctx, goServerCommand(o.projectDir, o.cfg.GoServerCommand), o.logger)
	// A Go-server crash is reported, not fatal: the developer's next save
	// rebuilds it. onExit fires only for an unsolicited exit.
	o.goServer.onExit = func(exitErr error) {
		if exitErr != nil {
			o.logger.ErrorContext(o.ctx, "go server exited unexpectedly",
				slog.String("error", exitErr.Error()),
				slog.String("hint", "fix the error and save to rebuild"))
		} else {
			o.logger.WarnContext(o.ctx, "go server exited; save a .go file to restart")
		}
	}
	if startErr := o.goServer.Start(); startErr != nil {
		// An initial start failure is reported but not fatal — a broken build
		// is the most common first-run state; the loop stays up so the next
		// save recovers it.
		o.logger.ErrorContext(ctx, "initial go server start failed",
			slog.String("error", startErr.Error()))
	}

	// --- Vite supervisor (graceful when absent) --------------------------
	if webProject, found := detectViteProject(o.projectDir); found {
		o.vite = newSupervisor(ctx, viteCommand(webProject, o.cfg.ViteCommand), o.logger)
		o.vite.onExit = func(exitErr error) {
			if exitErr != nil {
				o.logger.ErrorContext(o.ctx, "vite dev server exited unexpectedly",
					slog.String("error", exitErr.Error()))
			}
		}
		if startErr := o.vite.Start(); startErr != nil {
			o.logger.ErrorContext(ctx, "vite dev server start failed",
				slog.String("error", startErr.Error()))
		} else {
			o.logger.InfoContext(ctx, "supervising vite dev server (svelte HMR)",
				slog.String("web", webProject))
		}
	} else {
		o.logger.InfoContext(ctx, "no web/ UI project found — supervising the go server only")
	}

	o.logger.InfoContext(ctx, "dockyard dev is watching for changes",
		slog.String("project", o.projectDir))
	if o.cfg.hooks != nil && o.cfg.hooks.onReady != nil {
		o.cfg.hooks.onReady()
	}

	// --- the watch loop ---------------------------------------------------
	loopErr := o.watchLoop(ctx, w.events)

	// --- teardown: children first, then the watcher ----------------------
	o.teardown()
	// Cancel the watcher's context so w.run observes ctx.Done and returns;
	// it closes its own events channel. wg.Wait then confirms the watcher
	// goroutine has exited — no leaked goroutine on teardown.
	cancel()
	wg.Wait()
	if closeErr := w.close(); closeErr != nil {
		o.logger.WarnContext(ctx, "watcher close", slog.String("error", closeErr.Error()))
	}
	o.logger.InfoContext(ctx, "dockyard dev stopped")
	return loopErr
}

// watchLoop reacts to coalesced change events until ctx is cancelled. A
// context cancellation is a clean exit (nil); the loop itself never returns a
// fatal error — a build failure on restart is reported and survived.
func (o *orchestrator) watchLoop(ctx context.Context, events <-chan changeKind) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case kind, ok := <-events:
			if !ok {
				return nil
			}
			o.handleChange(kind)
		}
	}
}

// handleChange applies one coalesced change. On a contract change codegen runs
// first (regenerate-then-restart, RFC §9.2) so the generated types are live
// before the server rebuilds.
func (o *orchestrator) handleChange(kind changeKind) {
	if kind == changeContract && !o.cfg.SkipCodegen {
		o.runCodegen()
	}
	if kind == changeGo || kind == changeContract {
		o.restartGoServer()
	}
}

// runCodegen re-runs the contract codegen in-process via internal/generate —
// never by shelling out to the `dockyard generate` verb. A codegen failure is
// reported and the loop survives: the developer's contract source has a
// transient error; the next save retries.
func (o *orchestrator) runCodegen() {
	o.logger.InfoContext(o.ctx, "contract changed — regenerating contracts")
	m, err := manifest.LoadFile(filepath.Join(o.projectDir, manifest.DefaultFilename))
	if err != nil {
		o.logger.ErrorContext(o.ctx, "regenerate: manifest did not load",
			slog.String("error", err.Error()))
		o.notifyCodegen(err)
		return
	}
	res, err := generate.Run(generate.Options{ProjectDir: o.projectDir, Manifest: m})
	if err != nil {
		o.logger.ErrorContext(o.ctx, "regenerate failed", slog.String("error", err.Error()))
		o.notifyCodegen(err)
		return
	}
	if len(res.Changed) == 0 {
		o.logger.InfoContext(o.ctx, "regenerate: no contract changes")
	} else {
		o.logger.InfoContext(o.ctx, "regenerate: contracts updated",
			slog.Int("files_changed", len(res.Changed)))
	}
	o.notifyCodegen(nil)
}

// restartGoServer cleanly restarts the supervised Go MCP server. A restart
// failure is reported and the loop survives.
func (o *orchestrator) restartGoServer() {
	o.logger.InfoContext(o.ctx, "restarting the go server")
	if err := o.goServer.Restart(); err != nil {
		o.logger.ErrorContext(o.ctx, "go server restart failed",
			slog.String("error", err.Error()))
		return
	}
	o.notifyServerRestart()
}

// teardown stops the supervised children. Children are stopped before the
// watcher so a final file event cannot trigger a restart of a child that is
// being torn down.
func (o *orchestrator) teardown() {
	if o.goServer != nil {
		o.goServer.Stop()
	}
	if o.vite != nil {
		o.vite.Stop()
	}
}

func (o *orchestrator) notifyServerRestart() {
	if o.cfg.hooks != nil && o.cfg.hooks.onServerRestart != nil {
		o.cfg.hooks.onServerRestart()
	}
}

func (o *orchestrator) notifyCodegen(err error) {
	if o.cfg.hooks != nil && o.cfg.hooks.onCodegen != nil {
		o.cfg.hooks.onCodegen(err)
	}
}

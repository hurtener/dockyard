// Command combined-patterns is the Phase 28 worked example showing
// analytics-widgets + approval-flows COMPOSED on one MCP App (D-150).
//
// Two tools on one App:
//   - rollout_health (synchronous analytics widget; metric card)
//   - propose_rollout_action (task-augmented approval flow; approval card)
//
// The agent surfaces the metric, then proposes a follow-up the user
// approves — both rendered in the same chat surface so the
// insight → action pattern is visible end to end.
//
// The transport is chosen by DOCKYARD_TRANSPORT ("stdio" by default;
// "http" for streamable-HTTP). The scaffold wires a real tasks.Engine
// over an in-memory TaskStore — replace with a SQLite-backed store for
// a durable HTTP deployment.
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"

	"github.com/hurtener/dockyard/examples/combined-patterns/internal/contracts"
	"github.com/hurtener/dockyard/examples/combined-patterns/internal/handlers"
)

// uiBundle holds the minimal single-file App HTML. The example ships a
// hand-written index.html (no Vite build needed) co-located with this
// main.go, so the example is buildable + runnable inside the Dockyard
// repo without an `npm install`. A real project uses Vite — see the
// analytics-widgets template for the Vite-built pattern; that template
// embeds `web/dist` from the project root.
//
//go:embed index.html
var uiBundle embed.FS

const (
	httpAddr = "127.0.0.1:8080"
	// Two apps, one per view contract — RFC §8.6 requires tools sharing a
	// ui:// app to agree on task_support. The metric and approval views
	// ship the SAME index.html bundle (a single dispatcher that routes
	// on `structuredContent.kind`), but they are addressed by distinct
	// ui:// URIs.
	metricAppURI    = "ui://combined-patterns/rollout-metric"
	metricAppName   = "rollout_metric"
	approvalAppURI  = "ui://combined-patterns/rollout-approval"
	approvalAppName = "rollout_approval"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	obsSink, err := obs.NewSSESink("")
	if err != nil {
		logger.Error("create obs sink", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = obsSink.Close() }()

	// Tasks engine over an in-memory store. The propose_rollout_action
	// tool's task_support: required setting (in dockyard.app.yaml) means
	// every call runs as an MCP Task; the engine drives the
	// input_required round-trip the user replies to through the bridge.
	taskStore := tasks.NewInMemoryStore()
	engine, err := tasks.NewEngine(taskStore, &tasks.Options{
		Logger:                logger,
		Obs:                   obsSink,
		RequestorIdentifiable: false, // stdio default
		AdvertiseList:         false,
		PollInterval:          250,
	})
	if err != nil {
		logger.Error("create tasks engine", slog.String("error", err.Error()))
		os.Exit(1)
	}
	engine.StartSweep(ctx)
	defer engine.StopSweep()

	srv, err := server.New(server.Info{
		Name:    "combined-patterns",
		Title:   "Combined Patterns",
		Version: "0.1.0",
	}, &server.Options{
		Logger: logger,
		Obs:    obsSink,
		Tasks:  engine,
	})
	if err != nil {
		logger.Error("create server", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("obs/v1 SSE stream",
		slog.String("addr", "http://"+obsSink.Addr()+"/obs/v1/stream"))

	if err := registerApp(srv); err != nil {
		logger.Error("register app", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := registerTools(srv, engine); err != nil {
		logger.Error("register tools", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := serve(ctx, srv, obsSink, logger); err != nil {
		logger.Error("serve", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func registerApp(srv *server.Server) error {
	html, err := fs.ReadFile(uiBundle, "index.html")
	if err != nil {
		return err
	}
	// Register the metric view + the approval view as two distinct
	// ui:// resources. The bundle is identical — the dispatcher inside
	// the HTML routes on structuredContent.kind so one HTML serves
	// both renderers.
	if err := apps.Register(srv, apps.App{
		URI:   metricAppURI,
		Name:  metricAppName,
		Title: "Combined Patterns — rollout metric",
		HTML:  html,
	}); err != nil {
		return err
	}
	return apps.Register(srv, apps.App{
		URI:   approvalAppURI,
		Name:  approvalAppName,
		Title: "Combined Patterns — rollout approval",
		HTML:  html,
	})
}

func registerTools(srv *server.Server, engine *tasks.Engine) error {
	snap := handlers.NewSnapshot()
	if err := tool.New[contracts.RolloutHealthInput, contracts.RolloutHealthOutput]("rollout_health").
		Describe("Show a metric card summarising the current rollout's health (analytics widget).").
		UI(metricAppName).
		Handler(snap.Handler).
		Register(srv); err != nil {
		return err
	}
	approver := handlers.NewApprovalProposer(engine)
	if err := tool.New[contracts.ProposeRolloutActionInput, contracts.ProposeRolloutActionOutput]("propose_rollout_action").
		Describe("Propose the next rollout action (advance / pause / rollback) for human approval.").
		UI(approvalAppName).
		Handler(approver.Handler).
		Register(srv); err != nil {
		return err
	}
	return nil
}

func serve(ctx context.Context, srv *server.Server, obsSink *obs.SSESink, logger *slog.Logger) error {
	switch transport := os.Getenv("DOCKYARD_TRANSPORT"); transport {
	case "", "stdio":
		return srv.ServeStdio(ctx)
	case "http":
		return serveHTTP(ctx, srv, obsSink, logger)
	default:
		return errors.New("unsupported DOCKYARD_TRANSPORT " + transport + " (want \"stdio\" or \"http\")")
	}
}

func serveHTTP(ctx context.Context, srv *server.Server, obsSink *obs.SSESink, logger *slog.Logger) error {
	handler, err := srv.HTTPHandler(nil)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/obs/v1/stream", obsSink.Handler())
	mux.Handle("/", handler)
	addr := httpAddr
	if override := os.Getenv("DOCKYARD_HTTP_ADDR"); override != "" {
		addr = override
	}
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second, // gosec G112
	}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()
	logger.InfoContext(ctx, "serving streamable-HTTP transport", slog.String("addr", addr))
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

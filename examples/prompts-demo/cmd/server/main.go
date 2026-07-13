// Command prompts-demo is the Phase 28 worked example for the
// Dockyard prompts API (D-151).
//
// It registers three MCP Prompts via runtime/server.AddPrompt:
//
//   - summarize_for_review — a careful summary primer (system + user)
//   - code_review          — review a diff against a rubric (4 messages)
//   - explain_error        — explain a runtime error in plain language
//
// And one tool (`summarize_text`) so the manifest is valid and the
// example also showcases the tool / prompt distinction side by side.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"

	"github.com/hurtener/dockyard/examples/prompts-demo/internal/contracts"
	"github.com/hurtener/dockyard/examples/prompts-demo/internal/handlers"
)

const httpAddr = "127.0.0.1:8080"

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

	srv, err := server.New(server.Info{
		Name:    "prompts-demo",
		Title:   "Prompts Demo",
		Version: "0.1.0",
	}, &server.Options{Logger: logger, Obs: obsSink})
	if err != nil {
		logger.Error("create server", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("obs/v1 SSE stream",
		slog.String("addr", "http://"+obsSink.Addr()+"/obs/v1/stream"))

	if err := registerTools(srv); err != nil {
		logger.Error("register tools", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := registerPrompts(srv); err != nil {
		logger.Error("register prompts", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := serve(ctx, srv, obsSink, logger); err != nil {
		logger.Error("serve", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func registerTools(srv *server.Server) error {
	return tool.New[contracts.SummarizeTextInput, contracts.SummarizeTextOutput]("summarize_text").
		Describe("Quick local summarisation — a one-liner over an input passage.").
		Handler(handlers.SummarizeText).
		Register(srv)
}

// registerPrompts wires the three MCP Prompts onto the server via the
// Phase 28 prompts API. PromptDef.Arguments declares the names the
// host advertises in prompts/list; the handler's PromptRequest.Arguments
// is the host-supplied value map.
func registerPrompts(srv *server.Server) error {
	if err := server.AddPrompt(srv, server.PromptDef{
		Name:        "summarize_for_review",
		Title:       "Summarise for engineering review",
		Description: "Two-sentence summary geared at an engineering peer review; preserves named entities; calls out one open question.",
		Arguments: []server.PromptArgument{
			{Name: "passage", Description: "The passage to summarise.", Required: true},
			{Name: "audience", Description: "Audience for the summary; defaults to 'an engineering peer'."},
		},
	}, handlers.SummarizeForReview); err != nil {
		return err
	}
	if err := server.AddPrompt(srv, server.PromptDef{
		Name:        "code_review",
		Title:       "Code review against a rubric",
		Description: "Review a diff against a short rubric.",
		Arguments: []server.PromptArgument{
			{Name: "diff", Description: "The diff to review.", Required: true},
			{Name: "language", Description: "Language of the diff; defaults to Go."},
			{Name: "rubric", Description: "Rubric to apply; defaults to a sensible 4-point list."},
		},
	}, handlers.CodeReview); err != nil {
		return err
	}
	if err := server.AddPrompt(srv, server.PromptDef{
		Name:        "explain_error",
		Title:       "Explain an error in plain language",
		Description: "Explain a runtime error in two sentences and suggest the most likely fix.",
		Arguments: []server.PromptArgument{
			{Name: "error", Description: "The error message to explain.", Required: true},
			{Name: "language", Description: "Language the error came from; defaults to Go."},
		},
	}, handlers.ExplainError); err != nil {
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
	handler, err := srv.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Dual})
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

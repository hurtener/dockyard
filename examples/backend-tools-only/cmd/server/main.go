// Command backend-tools-only is the Phase 28 worked example showing
// Dockyard as a pure-tools MCP server — no MCP App, no UI, just typed
// tool handlers exposed to an agent host (RFC §2; Phase 28, D-150).
//
// The transport is chosen by DOCKYARD_TRANSPORT ("stdio" by default;
// "http" for the streamable-HTTP service mode). Wire it into a host with
// `dockyard install` once you've built it.
//
// Run it directly with `go run ./cmd/server` from the example directory;
// see README.md for the full lifecycle (generate → validate → build → run).
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

	"github.com/hurtener/dockyard/examples/backend-tools-only/internal/contracts"
	"github.com/hurtener/dockyard/examples/backend-tools-only/internal/handlers"
)

// httpAddr is the default address the HTTP transport listens on.
// DOCKYARD_HTTP_ADDR overrides it.
const httpAddr = "127.0.0.1:8080"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The obs/v1 SSE sink is the out-of-band, localhost-bound event stream
	// the inspector subscribes to (RFC §11.3, §12). A pure-tools server
	// gets the same observability as a UI-bearing one.
	obsSink, err := obs.NewSSESink("")
	if err != nil {
		logger.Error("create obs sink", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = obsSink.Close() }()

	srv, err := server.New(server.Info{
		Name:    "backend-tools-only",
		Title:   "Backend Tools Only",
		Version: "0.1.0",
	}, &server.Options{Logger: logger, Obs: obsSink})
	if err != nil {
		logger.Error("create server", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("obs/v1 SSE stream",
		slog.String("addr", "http://"+obsSink.Addr()+"/obs/v1/stream"))

	if err := registerTools(srv, handlers.NewCatalog()); err != nil {
		logger.Error("register tools", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := serve(ctx, srv, obsSink, logger); err != nil {
		logger.Error("serve", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// registerTools wires the three bookmark tools onto the server. Each is a
// contract-first tool: the runtime/tool builder generates the schema from
// the typed Go contract (P1; RFC §6), so the schema the host sees IS the
// Go type, never a hand-written drift.
func registerTools(srv *server.Server, catalog *handlers.Catalog) error {
	if err := tool.New[contracts.ListBookmarksInput, contracts.ListBookmarksOutput]("list_bookmarks").
		Describe("List every bookmark in the catalog, optionally filtered by tag.").
		Handler(adaptList(catalog)).
		Register(srv); err != nil {
		return err
	}
	if err := tool.New[contracts.AddBookmarkInput, contracts.AddBookmarkOutput]("add_bookmark").
		Describe("Add a bookmark to the catalog and return the stored record.").
		Handler(adaptAdd(catalog)).
		Register(srv); err != nil {
		return err
	}
	if err := tool.New[contracts.SearchBookmarksInput, contracts.SearchBookmarksOutput]("search_bookmarks").
		Describe("Substring-search bookmark titles, URLs, and notes.").
		Handler(adaptSearch(catalog)).
		Register(srv); err != nil {
		return err
	}
	return nil
}

// adapt* wraps a typed catalog method in the runtime/tool builder's handler
// signature (the builder returns a tool.Result so the model-facing text +
// the structured payload can diverge).
func adaptList(c *handlers.Catalog) func(context.Context, contracts.ListBookmarksInput) (tool.Result[contracts.ListBookmarksOutput], error) {
	return func(ctx context.Context, in contracts.ListBookmarksInput) (tool.Result[contracts.ListBookmarksOutput], error) {
		out, err := c.ListBookmarks(ctx, in)
		if err != nil {
			return tool.Result[contracts.ListBookmarksOutput]{}, err
		}
		text := summariseList(out.Total, in.Tag)
		return tool.Result[contracts.ListBookmarksOutput]{Text: text, Structured: out}, nil
	}
}

func adaptAdd(c *handlers.Catalog) func(context.Context, contracts.AddBookmarkInput) (tool.Result[contracts.AddBookmarkOutput], error) {
	return func(ctx context.Context, in contracts.AddBookmarkInput) (tool.Result[contracts.AddBookmarkOutput], error) {
		out, err := c.AddBookmark(ctx, in)
		if err != nil {
			return tool.Result[contracts.AddBookmarkOutput]{}, err
		}
		text := "Stored bookmark: " + out.Bookmark.Title
		return tool.Result[contracts.AddBookmarkOutput]{Text: text, Structured: out}, nil
	}
}

func adaptSearch(c *handlers.Catalog) func(context.Context, contracts.SearchBookmarksInput) (tool.Result[contracts.SearchBookmarksOutput], error) {
	return func(ctx context.Context, in contracts.SearchBookmarksInput) (tool.Result[contracts.SearchBookmarksOutput], error) {
		out, err := c.SearchBookmarks(ctx, in)
		if err != nil {
			return tool.Result[contracts.SearchBookmarksOutput]{}, err
		}
		text := "Search matched " + plural(out.Total, "bookmark") + " for " + quoted(in.Query)
		return tool.Result[contracts.SearchBookmarksOutput]{Text: text, Structured: out}, nil
	}
}

func summariseList(n int, tag string) string {
	if tag == "" {
		return "Catalog has " + plural(n, "bookmark") + "."
	}
	return "Catalog has " + plural(n, "bookmark") + " tagged " + quoted(tag) + "."
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

func quoted(s string) string { return `"` + s + `"` }

// itoa avoids importing strconv for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// serve brings up the transport named by DOCKYARD_TRANSPORT.
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
		ReadHeaderTimeout: 10 * time.Second, // gosec G112 — prevent Slowloris
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

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrNoTransport is returned by Run when it is called with a nil transport.
var ErrNoTransport = errors.New("dockyard/runtime/server: nil transport")

// Info identifies a Dockyard app to connecting MCP hosts. It maps onto the
// SDK's mcp.Implementation; Dockyard keeps its own type so the runtime-facing
// API never exposes raw SDK structs to app authors (RFC §5.4, P3).
type Info struct {
	// Name is the programmatic server identifier. Required.
	Name string
	// Title is the human-readable display name. Optional.
	Title string
	// Version is the app's semantic version. Required.
	Version string
}

func (i Info) validate() error {
	if i.Name == "" {
		return errors.New("dockyard/runtime/server: Info.Name is required")
	}
	if i.Version == "" {
		return errors.New("dockyard/runtime/server: Info.Version is required")
	}
	return nil
}

// Options tunes a Server. The zero value is valid; a nil *Options is treated
// as the zero value.
type Options struct {
	// Logger receives the server's structured logs. When nil, slog.Default()
	// is used. AGENTS.md §5 mandates log/slog.
	Logger *slog.Logger
}

func (o *Options) logger() *slog.Logger {
	if o != nil && o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// Server is the Dockyard app-runtime MCP server. It wraps an SDK *mcp.Server
// and is the seam later phases extend with the Apps, Tasks, and obs/v1 layers
// (RFC §5). A Server is safe to construct once and serve repeatedly; tool
// registration happens before Run.
type Server struct {
	info  Info
	log   *slog.Logger
	mcp   *mcpsdk.Server
	tools []string // registered tool names, in registration order
}

// New constructs a Dockyard MCP server. It returns an error rather than
// panicking so a thin app main.go can fail cleanly (AGENTS.md §5: never panic
// across the MCP boundary).
func New(info Info, opts *Options) (*Server, error) {
	if err := info.validate(); err != nil {
		return nil, err
	}
	log := opts.logger()
	mcpSrv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    info.Name,
		Title:   info.Title,
		Version: info.Version,
	}, nil)
	return &Server{
		info: info,
		log:  log,
		mcp:  mcpSrv,
	}, nil
}

// Info returns the server identity.
func (s *Server) Info() Info { return s.info }

// Tools returns the names of registered tools, in registration order. The
// returned slice is a copy and safe for the caller to retain.
func (s *Server) Tools() []string {
	out := make([]string, len(s.tools))
	copy(out, s.tools)
	return out
}

// MCP exposes the underlying SDK server. This is a deliberate, temporary seam
// for sibling Phase 02 / 07 work that needs SDK-level registration before the
// Dockyard-owned builder API lands; it is not part of the long-term app-facing
// surface and app authors should not depend on it.
func (s *Server) MCP() *mcpsdk.Server { return s.mcp }

// Run serves the MCP protocol over the given transport until the context is
// cancelled or the peer disconnects. Phase 01 wires stdio (see ServeStdio);
// streamable-HTTP arrives in Phase 07 (RFC §5.2).
func (s *Server) Run(ctx context.Context, t mcpsdk.Transport) error {
	if t == nil {
		return ErrNoTransport
	}
	s.log.InfoContext(ctx, "dockyard server starting",
		slog.String("name", s.info.Name),
		slog.String("version", s.info.Version),
		slog.Int("tools", len(s.tools)),
	)
	if err := s.mcp.Run(ctx, t); err != nil {
		return fmt.Errorf("dockyard/runtime/server: serve: %w", err)
	}
	s.log.InfoContext(ctx, "dockyard server stopped", slog.String("name", s.info.Name))
	return nil
}

// ServeStdio serves the server over the stdio transport — the local
// deployment mode (RFC §5.2). It blocks until ctx is cancelled or the host
// closes the pipe.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.Run(ctx, &mcpsdk.StdioTransport{})
}

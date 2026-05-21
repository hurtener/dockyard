package server

import (
	"context"
	"encoding/json"
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
	// Extensions are the MCP extension capabilities the server advertises
	// during the initialize handshake (the SEP-2133 `extensions` capability
	// block; RFC §5.3). The Apps layer (runtime/apps, Phase 09) supplies the
	// io.modelcontextprotocol/ui entry here. A nil/empty slice advertises no
	// extensions — the server behaves as a plain MCP server.
	//
	// Each entry's Settings is opaque wire JSON produced by the owning
	// extension layer through internal/protocolcodec; the runtime never
	// inspects it, preserving the protocolcodec isolation seam (P3, RFC §5.4).
	Extensions []ExtensionCapability
}

// ExtensionCapability is one MCP extension-capability advertisement: a
// registry-scoped extension name and its opaque settings JSON. It is the
// runtime-facing carrier for the SEP-2133 `extensions` capability block
// (RFC §5.3) — Settings is produced by internal/protocolcodec, so the runtime
// surface never exposes a raw protocol struct (P3).
type ExtensionCapability struct {
	// Name is the registry-scoped extension identifier, e.g.
	// "io.modelcontextprotocol/ui".
	Name string
	// Settings is the per-extension settings object, as opaque wire JSON. A
	// nil/empty value advertises the extension with no settings object.
	Settings json.RawMessage
}

func (o *Options) logger() *slog.Logger {
	if o != nil && o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// serverCapabilities builds the SDK capability block to advertise, or nil when
// the app declares no extensions (so the SDK keeps its inferred capabilities).
func (o *Options) serverCapabilities() (*mcpsdk.ServerCapabilities, error) {
	if o == nil || len(o.Extensions) == 0 {
		return nil, nil
	}
	caps := &mcpsdk.ServerCapabilities{}
	for _, ext := range o.Extensions {
		if ext.Name == "" {
			return nil, errors.New("dockyard/runtime/server: extension capability with empty Name")
		}
		var settings map[string]any
		if len(ext.Settings) > 0 {
			if err := json.Unmarshal(ext.Settings, &settings); err != nil {
				return nil, fmt.Errorf(
					"dockyard/runtime/server: extension %q settings: %w", ext.Name, err)
			}
		}
		caps.AddExtension(ext.Name, settings)
	}
	return caps, nil
}

// Server is the Dockyard app-runtime MCP server. It wraps an SDK *mcp.Server
// and is the seam later phases extend with the Apps, Tasks, and obs/v1 layers
// (RFC §5). A Server is safe to construct once and serve repeatedly; tool and
// resource registration happens before Run.
type Server struct {
	info              Info
	log               *slog.Logger
	mcp               *mcpsdk.Server
	tools             []string // registered tool names, in registration order
	resources         []string // registered resource URIs, in registration order
	resourceTemplates []string // registered resource-template URI templates, in registration order
}

// New constructs a Dockyard MCP server. It returns an error rather than
// panicking so a thin app main.go can fail cleanly (AGENTS.md §5: never panic
// across the MCP boundary).
//
// When opts.Extensions is non-empty the server advertises those MCP extension
// capabilities during the initialize handshake (RFC §5.3); the SDK's inferred
// tools/resources capabilities are preserved — only the explicit extension
// block is added.
func New(info Info, opts *Options) (*Server, error) {
	if err := info.validate(); err != nil {
		return nil, err
	}
	log := opts.logger()
	caps, err := opts.serverCapabilities()
	if err != nil {
		return nil, err
	}
	var sdkOpts *mcpsdk.ServerOptions
	if caps != nil {
		sdkOpts = &mcpsdk.ServerOptions{Capabilities: caps}
	}
	mcpSrv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    info.Name,
		Title:   info.Title,
		Version: info.Version,
	}, sdkOpts)
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

// The temporary exported MCP() *mcp.Server seam (D-021) is retired in Phase 07
// (D-042): the Dockyard-owned registration surface (AddTool,
// AddToolWithSchemas, AddResource) and the transport entrypoints (Run,
// ServeStdio, ServeInMemory, HTTPHandler) are complete, so no caller needs raw
// SDK access. The SDK *mcp.Server is reached only through the unexported s.mcp
// field, restoring RFC §5.4 / P3 — the runtime surface exposes no raw SDK
// structs.

// Run serves the MCP protocol over the given transport until the context is
// cancelled or the peer disconnects. ServeStdio wires stdio; HTTPHandler wires
// streamable-HTTP; ServeInMemory wires the in-memory transport (RFC §5.2).
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

// ServeInMemory serves the server over an in-memory transport and returns the
// matching client-side transport (RFC §5.2). It is the backbone of the
// inspector and the contract tests (brief 03 §2.3): no OS pipe, no socket — the
// two transports are connected in process.
//
// ServeInMemory starts the server in a background goroutine bound to ctx and
// returns once the server is connected, so the caller can immediately connect a
// client to the returned transport. The server stops when ctx is cancelled. Any
// serve error is logged; callers that need it should use Run directly.
func (s *Server) ServeInMemory(ctx context.Context) mcpsdk.Transport {
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	go func() {
		if err := s.Run(ctx, serverT); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "dockyard server in-memory serve failed",
				slog.String("name", s.info.Name),
				slog.String("error", err.Error()),
			)
		}
	}()
	return clientT
}

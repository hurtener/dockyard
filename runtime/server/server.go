package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/tasks"
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

	// Obs is the obs/v1 observability emitter the server emits tool, resource,
	// and lifecycle events to (RFC §11, P2). A nil emitter disables emission —
	// the server is headless either way; observability is a protocol the
	// runtime PRODUCES, never a back channel anything reads (CLAUDE.md §6).
	//
	// The runtime depends only on the obs.Emitter interface: the ring-buffer
	// driver (Phase 15), the SSE sink and the OTel adapter (Phase 16) all plug
	// in here behind the same seam (CLAUDE.md §4.4).
	Obs obs.Emitter

	// CapturePolicy controls how much of a tool's input/output an emitted
	// event carries. The default — the zero value, obs.CapturePolicyShape —
	// captures shape + size only, never full content (CLAUDE.md §7).
	CapturePolicy obs.CapturePolicy

	// Redactor is the redaction-aware hook obs.CapturePolicyFull requires.
	// Without it, full-content capture degrades to shape+size.
	Redactor obs.Redactor

	// Tasks is the MCP Tasks engine to mount onto the server's transports
	// (RFC §8.2). When non-nil the server intercepts tasks/* JSON-RPC frames
	// at the raw-frame layer ahead of the SDK server — the go-sdk rejects an
	// unknown JSON-RPC method before any middleware runs, so tasks/get,
	// tasks/result, tasks/cancel and tasks/list cannot reach the engine
	// through the SDK's dispatch table; the Tasks transport mount
	// (runtime/tasks.Mount) is the "shim, by necessity" that routes them.
	// The capabilities.tasks block is injected into the initialize handshake
	// response (the SDK has no native field for it).
	//
	// When Tasks is nil the server behaves exactly as a plain MCP server with
	// no tasks/* interception and no added overhead. The engine is attached
	// via WithTasks too — Tasks is the option-struct idiom matching Obs.
	Tasks *tasks.Engine

	// TasksAuthContext derives a requestor's opaque authorization-context
	// token from an HTTP request — for example a verified bearer-token
	// subject. It is consulted only when Tasks is non-nil and the server
	// serves over the streamable-HTTP transport, enabling auth-context
	// binding of tasks/* (RFC §8.5). A nil value treats every requestor as
	// unauthenticated, which is the correct posture for single-user stdio.
	TasksAuthContext tasks.AuthContextFunc
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
	rec               *obs.Recorder // obs/v1 emit helper; never nil (NopEmitter when unconfigured)
	tools             []string      // registered tool names, in registration order
	resources         []string      // registered resource URIs, in registration order
	resourceTemplates []string      // registered resource-template URI templates, in registration order
	prompts           []string      // registered prompt names, in registration order (Phase 28)

	// appLinks maps an App's programmatic name to its ui:// link, recorded by
	// runtime/apps.Register so the runtime/tool builder can resolve a tool's
	// .UI(name) to the App's URI and emit _meta.ui (RFC §7.1; D-173). Like the
	// slices above it is populated during single-threaded registration. nil
	// until the first RegisterAppLink.
	appLinks map[string]AppLink

	// tasksMount routes tasks/* JSON-RPC frames into the attached Tasks engine
	// ahead of the SDK server (RFC §8.2). It is nil unless a Tasks engine was
	// attached via Options.Tasks or WithTasks; a nil mount means the server is
	// a plain MCP server with no tasks/* interception.
	tasksMount *tasks.Mount
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
	s := &Server{
		info: info,
		log:  log,
		mcp:  mcpSrv,
		rec:  newRecorder(info, opts),
	}
	// Attach the Tasks transport mount when a Tasks engine was supplied. The
	// mount is the seam that routes tasks/* frames into the engine ahead of
	// the SDK server; HTTPHandler and ServeStdio consult s.tasksMount (RFC
	// §8.2). A nil opts.Tasks leaves s.tasksMount nil — a plain MCP server.
	if opts != nil && opts.Tasks != nil {
		s.attachTasks(opts.Tasks, opts.TasksAuthContext)
	}
	return s, nil
}

// WithTasks attaches a Tasks engine to the server, mounting tasks/* onto its
// transports (RFC §8.2). It is the imperative counterpart of Options.Tasks for
// a caller that constructs the server before the engine exists; calling it
// replaces any previously attached engine. authContext derives an HTTP
// requestor's authorization context (RFC §8.5) — pass nil for the
// unauthenticated single-user case. WithTasks must be called before Run /
// ServeStdio / HTTPHandler; it is not safe to call concurrently with serving.
func (s *Server) WithTasks(engine *tasks.Engine, authContext tasks.AuthContextFunc) *Server {
	if s == nil || engine == nil {
		return s
	}
	s.attachTasks(engine, authContext)
	return s
}

// attachTasks builds the Tasks transport mount over engine and records it on
// the server. It is the single place s.tasksMount is set, shared by New (the
// Options path) and WithTasks (the imperative path).
func (s *Server) attachTasks(engine *tasks.Engine, authContext tasks.AuthContextFunc) {
	mount := tasks.NewMount(engine)
	if authContext != nil {
		mount = mount.WithAuthContext(authContext)
	}
	s.tasksMount = mount
}

// TasksEnabled reports whether a Tasks engine is attached — true once
// Options.Tasks or WithTasks has supplied one. It lets a transport entrypoint
// and a smoke check observe the wiring without reaching into server internals.
func (s *Server) TasksEnabled() bool {
	return s != nil && s.tasksMount != nil
}

// newRecorder builds the server's obs/v1 emit helper from opts. It is never
// nil: an unconfigured emitter yields a Recorder over obs.NopEmitter, so every
// emit site calls the same methods without a nil guard. The server identity is
// the obs ServerID — events carry a stable server identity (RFC §11.2).
func newRecorder(info Info, opts *Options) *obs.Recorder {
	var emitter obs.Emitter
	var ropts []obs.RecorderOption
	if opts != nil {
		emitter = opts.Obs
		if opts.CapturePolicy != obs.CapturePolicyShape {
			ropts = append(ropts, obs.WithCapturePolicy(opts.CapturePolicy))
		}
		if opts.Redactor != nil {
			ropts = append(ropts, obs.WithRedactor(opts.Redactor))
		}
	}
	return obs.NewRecorder(emitter, info.Name, ropts...)
}

// Recorder returns the server's obs/v1 emit helper. It is never nil. Extension
// layers (runtime/apps, runtime/tasks) emit their own obs/v1 events through it
// so every subsystem shares one emitter and one server identity — they EMIT,
// they never read each other's internals (P2, CLAUDE.md §6).
func (s *Server) Recorder() *obs.Recorder { return s.rec }

// Info returns the server identity.
func (s *Server) Info() Info { return s.info }

// AppLink is the server-recorded link from an App's programmatic name to its
// ui:// resource. runtime/apps.Register records one per App; the runtime/tool
// builder reads it to resolve a tool's .UI(name) to the App's URI and emit
// _meta.ui (RFC §7.1; D-173). It is a small, protocol-agnostic value — the
// server stores no Apps wire types (P3).
type AppLink struct {
	// URI is the App's ui:// resource URI.
	URI string
}

// RegisterAppLink records the link from an App's name to its ui:// resource.
// runtime/apps.Register calls it after the App's resource is installed, so a
// tool registered later can resolve .UI(name) → URI. It returns a typed error
// on a duplicate name — two Apps registered under one name is a programming
// error caught at registration, not a silent overwrite. Like tool/resource
// registration it is called during single-threaded setup.
func (s *Server) RegisterAppLink(name string, link AppLink) error {
	if name == "" {
		return errors.New("dockyard/runtime/server: RegisterAppLink with empty name")
	}
	if _, dup := s.appLinks[name]; dup {
		return fmt.Errorf("dockyard/runtime/server: App name %q already registered", name)
	}
	if s.appLinks == nil {
		s.appLinks = make(map[string]AppLink)
	}
	s.appLinks[name] = link
	return nil
}

// AppLinkByName returns the link recorded for an App name, or ok=false when no
// App was registered under that name. The runtime/tool builder uses it to wire
// a tool's _meta.ui and to fail loud when .UI(name) references no App.
func (s *Server) AppLinkByName(name string) (AppLink, bool) {
	link, ok := s.appLinks[name]
	return link, ok
}

// Tools returns the names of registered tools, in registration order. The
// returned slice is a copy and safe for the caller to retain.
func (s *Server) Tools() []string {
	out := make([]string, len(s.tools))
	copy(out, s.tools)
	return out
}

// Prompts returns the names of registered prompts, in registration order
// (Phase 28). The returned slice is a copy and safe for the caller to
// retain. Empty when no prompt has been registered.
func (s *Server) Prompts() []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s.prompts))
	copy(out, s.prompts)
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
	lifeSpan := obs.NewTrace()
	s.rec.ServerLifecycle(ctx, lifeSpan, obs.ServerLifecyclePayload{
		State:      "starting",
		ServerName: s.info.Name,
		Version:    s.info.Version,
		Tools:      len(s.tools),
	})
	if err := s.mcp.Run(ctx, t); err != nil {
		return fmt.Errorf("dockyard/runtime/server: serve: %w", err)
	}
	s.log.InfoContext(ctx, "dockyard server stopped", slog.String("name", s.info.Name))
	s.rec.ServerLifecycle(ctx, lifeSpan.Child(), obs.ServerLifecyclePayload{
		State:      "stopped",
		ServerName: s.info.Name,
		Version:    s.info.Version,
	})
	return nil
}

// ServeStdio serves the server over the stdio transport — the local
// deployment mode (RFC §5.2). It blocks until ctx is cancelled or the host
// closes the pipe.
//
// When a Tasks engine is attached (Options.Tasks / WithTasks) the stdio path
// runs the Tasks transport mount: tasks/* JSON-RPC frames on stdin are
// intercepted and answered by the engine, every other frame is forwarded to
// the SDK server (RFC §8.2). With no Tasks engine attached the SDK serves
// stdin/stdout directly, exactly as before.
func (s *Server) ServeStdio(ctx context.Context) error {
	if s.tasksMount != nil {
		return s.serveStdioWithTasks(ctx)
	}
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

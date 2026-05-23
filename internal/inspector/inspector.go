package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// defaultAddr is the inspector listen address when [Options.Addr] is empty: an
// OS-assigned port on the IPv4 loopback. It is loopback-only by construction —
// the inspector is a dev surface and is NEVER reachable off-localhost (RFC §12,
// CLAUDE.md §7, P4).
const defaultAddr = "127.0.0.1:0"

// readHeaderTimeout bounds the inspector's HTTP header read — a small dev
// server still sets an explicit timeout rather than inheriting an SDK default.
const readHeaderTimeout = 5 * time.Second

// ErrNonLoopbackBind is returned (wrapped) by [New] when the inspector is asked
// to bind a non-loopback address. The inspector is dev-mode-gated and
// localhost-only; a non-loopback bind is rejected before the listener opens and
// is never served. This is the binding RFC §12 acceptance criterion and the
// CVE-2025-49596 lesson (brief 05 §4.2).
var ErrNonLoopbackBind = errors.New(
	"dockyard/internal/inspector: refuses non-loopback bind address " +
		"(the inspector is dev-mode-only and localhost-bound)")

// ErrClosed is returned by [Inspector.Serve] after [Inspector.Close].
var ErrClosed = errors.New("dockyard/internal/inspector: inspector closed")

// ServerInfo is the read-only identity of the MCP server the inspector is
// attached to. It is surfaced verbatim in the inspector's PageHeader — no raw
// SDK or runtime struct leaks through it (P2/P3).
type ServerInfo struct {
	// Name is the connected server's name.
	Name string `json:"name"`
	// Version is the connected server's version.
	Version string `json:"version"`
	// Transport is the MCP transport in use: stdio | http | inmem.
	Transport string `json:"transport"`
}

// Options configures a new [Inspector].
type Options struct {
	// Addr is the loopback listen address. Empty selects [defaultAddr] (an
	// OS-assigned loopback port). A non-loopback or wildcard address is rejected
	// by [New] with [ErrNonLoopbackBind].
	Addr string

	// ServerInfo is the identity of the attached MCP server, surfaced read-only
	// in the inspector UI. The zero value is tolerated (an unknown server).
	ServerInfo ServerInfo

	// Relay sources the obs/v1 SSE stream and the JSON-RPC log the inspector
	// relays to its UI. When nil, the relay endpoints report an empty stream —
	// the inspector still serves its UI (the four-state empty state).
	Relay *Relay

	// Assets is the embedded web/inspector frontend (its built dist/ tree). When
	// nil, the inspector serves a minimal built-in placeholder so the backend is
	// usable before `vite build` has run — the Go build never depends on the
	// frontend being built.
	Assets fs.FS

	// Verdicts is the read-only source for the inspector's Verdicts panel —
	// contract-drift, schema-validation, and spec-compliance results (RFC §12).
	// When nil, `GET /api/verdicts` returns an empty array and the UI renders
	// its four-state empty state. Use [VerdictsFromValidate] to source it from
	// the `dockyard validate` engine.
	Verdicts VerdictSource

	// Contracts is the read-only source for the inspector's generated tool
	// contracts — the JSON array the fixture switcher derives its fixtures
	// from (RFC §12, §6 — P1, contract-first). When nil, `GET /api/contracts`
	// returns an empty array and the Fixtures/Tools panels render their
	// four-state empty state. The source returns the contracts as a JSON
	// array of `{name, description, inputSchema, outputSchema}` objects.
	Contracts ContractsSource

	// Apps is the read-only source for the inspector's App-preview frame — the
	// attached server's ui:// App resources, read via a read-only resources/read
	// (RFC §12 line 711 — the inspector renders the server's Apps; D-103). When
	// nil, `GET /api/apps` answers with an empty array and the App-frame renders
	// its "No App attached" empty state. Use [AppsFromServer] to source it from
	// a running MCP server's ui:// resources.
	Apps AppSource

	// Fixtures is the read-only source for the inspector's on-disk fixture
	// loader (RFC §12, Phase 24 / D-126). When nil, `GET /api/fixtures` answers
	// with an empty array and the Fixtures switcher falls back to its
	// schema-derived synthetic fixtures (the Phase 23 default). Use
	// [FixturesFromDir] to source it from the developer's project directory.
	Fixtures FixtureSource

	// Logger is the structured logger. When nil, a no-op logger is used.
	Logger *slog.Logger
}

// ContractsSource produces the attached server's generated tool contracts as
// a JSON array, on demand. The inspector calls it per `GET /api/contracts`
// request. It is content-free of any runtime internal — it returns the same
// generated-contract JSON the inspector frontend's contract model decodes.
type ContractsSource func() json.RawMessage

func (o Options) addr() string {
	if o.Addr == "" {
		return defaultAddr
	}
	return o.Addr
}

func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Inspector is the inspector's localhost HTTP backend (RFC §12). It serves the
// web/inspector frontend and relays the obs/v1 stream and JSON-RPC log to it,
// read-only. It is dev-mode-gated and localhost-only: [New] guarantees the bind
// address is a loopback interface.
//
// Inspector is a reusable concurrent artifact: [Inspector.Serve] runs the HTTP
// server, many UI clients may connect and disconnect concurrently, and
// [Inspector.Close] is idempotent and safe to call concurrently with Serve.
type Inspector struct {
	addr     string
	listener net.Listener
	server   *http.Server
	log      *slog.Logger

	mu        sync.Mutex
	closed    bool
	serveOnce bool
}

// New constructs an [Inspector] bound to a loopback address. A non-loopback,
// wildcard, or malformed bind address is rejected with [ErrNonLoopbackBind] —
// the listener is NOT opened. The returned Inspector is not yet serving; call
// [Inspector.Serve].
func New(opts Options) (*Inspector, error) {
	addr := opts.addr()
	if err := requireLoopback(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: listen %q: %w", addr, err)
	}
	log := opts.logger()
	insp := &Inspector{
		addr:     ln.Addr().String(),
		listener: ln,
		log:      log,
	}
	mux := newMux(opts, log)
	insp.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return insp, nil
}

// requireLoopback verifies addr's host resolves to a loopback interface. An
// empty or unspecified host (":0", ":8080") is rejected: the inspector must
// bind an explicit loopback interface, never a wildcard reachable off-localhost.
// A malformed address is rejected too. This is the mechanical enforcement of
// the inspector's localhost-only property (RFC §12).
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrNonLoopbackBind, addr)
	}
	if host == "" {
		// ":0" / ":port" binds every interface — not loopback-only.
		return fmt.Errorf("%w: %q", ErrNonLoopbackBind, addr)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%w: %q", ErrNonLoopbackBind, addr)
	}
	return nil
}

// Addr returns the inspector's resolved listen address, including the
// OS-assigned port when the construction address used port 0. It is always a
// loopback address.
func (i *Inspector) Addr() string { return i.addr }

// URL returns the http:// URL the inspector UI is served at.
func (i *Inspector) URL() string { return "http://" + i.addr }

// Serve runs the inspector HTTP server until ctx is cancelled or [Inspector.Close]
// is called. It blocks. Serve may be called once; a second call returns
// [ErrClosed]. A clean shutdown (ctx cancelled or Close) returns nil.
func (i *Inspector) Serve(ctx context.Context) error {
	i.mu.Lock()
	if i.closed || i.serveOnce {
		i.mu.Unlock()
		return ErrClosed
	}
	i.serveOnce = true
	i.mu.Unlock()

	i.log.InfoContext(ctx, "dockyard inspector serving",
		slog.String("addr", i.addr))

	// Shut the server down when ctx is cancelled so Serve unblocks. The drain
	// itself is [Inspector.Close], which derives its own deadline — ctx is
	// already cancelled at this point, so it cannot also bound the drain.
	stopped := make(chan struct{})
	go i.shutdownOnCancel(ctx, stopped)

	err := i.server.Serve(i.listener)
	close(stopped)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("dockyard/internal/inspector: serve: %w", err)
	}
	return nil
}

// shutdownOnCancel drains the HTTP server once serveCtx is cancelled, so a
// cancelled context unblocks [Inspector.Serve]. It returns early if Serve has
// already returned (stopped is closed). The drain runs through [Inspector.Close],
// which derives its own fresh deadline — serveCtx is cancelled by the time this
// fires, so it cannot bound the drain.
func (i *Inspector) shutdownOnCancel(serveCtx context.Context, stopped <-chan struct{}) {
	select {
	case <-serveCtx.Done():
	case <-stopped:
		return
	}
	_ = i.Close()
}

// Close shuts the inspector down. It stops the HTTP listener and is idempotent
// — a second call is a no-op (CLAUDE.md §5, the Closer contract). Close is safe
// to call concurrently with Serve.
func (i *Inspector) Close() error {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil
	}
	i.closed = true
	i.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := i.server.Shutdown(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("dockyard/internal/inspector: shutdown: %w", err)
	}
	return nil
}

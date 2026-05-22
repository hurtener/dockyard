package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/inspector"
)

// newInspectCmd builds the `dockyard inspect` command — the standalone inspector
// verb (RFC §12, §9.1). It attaches Dockyard's local test/debug surface to any
// running MCP server: it serves the inspector UI on a loopback port and relays
// the server's obs/v1 stream to it, read-only.
//
// The inspector is dev-mode-gated, localhost-only, and read-only — `dockyard
// inspect` is the explicit, deliberate entry into that surface (the inspector
// also runs automatically inside `dockyard dev`). The inspector binds a
// loopback address only: a non-loopback `--port` host is rejected by
// internal/inspector's ErrNonLoopbackBind gate before the listener opens, the
// mechanical enforcement of RFC §12 and the CVE-2025-49596 lesson.
//
// inspect wraps internal/inspector, the reusable inspector backend; the
// orchestration is a thin RunE, not logic buried here.
func newInspectCmd() *cobra.Command {
	var (
		serverURL string
		port      int
		noOpen    bool
	)

	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Attach the local inspector to a running MCP server",
		Long: `Attach Dockyard's local inspector to a running MCP server (RFC §12).

'dockyard inspect' serves the inspector — Dockyard's local test/debug surface —
on a loopback port and relays the obs/v1 event stream of the MCP server named
by --url to it. The inspector renders the server's Apps in a sandboxed iframe,
shows the live obs/v1 stream and the JSON-RPC log, switches fixtures, runs
contract/spec verdicts, and emulates host capability sets.

  --url      the running MCP server's obs/v1 stream URL (e.g.
             http://127.0.0.1:8080); the inspector relays its obs stream.
  --port     the inspector's own loopback port (default: an OS-assigned port).
  --no-open  do not open a browser — for CI and headless use.

The inspector is dev-mode-gated, localhost-only, and read-only: it is never a
production MCP client and never reachable off-localhost. A non-loopback bind is
refused before the listener opens. Press Ctrl-C to stop.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInspect(cmd.Context(), inspectConfig{
				serverURL: serverURL,
				port:      port,
				noOpen:    noOpen,
				logger: slog.New(slog.NewTextHandler(cmd.ErrOrStderr(),
					&slog.HandlerOptions{Level: slog.LevelInfo})),
				out: func(format string, args ...any) {
					fmt.Fprintf(cmd.OutOrStdout(), format, args...) //nolint:errcheck // CLI stdout write
				},
			})
		},
	}

	cmd.Flags().StringVar(&serverURL, "url", "",
		"the running MCP server's base URL (its obs/v1 stream is relayed)")
	cmd.Flags().IntVar(&port, "port", 0,
		"the inspector's loopback port (default: an OS-assigned port)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false,
		"do not open a browser (for CI / headless use)")
	return cmd
}

// inspectConfig is the resolved input to runInspect — extracted so the
// orchestration is unit-testable without a cobra command.
type inspectConfig struct {
	serverURL string
	port      int
	noOpen    bool
	logger    *slog.Logger
	out       func(format string, args ...any)
}

// runInspect serves the inspector backend until ctx is cancelled. It is the
// `dockyard inspect` orchestration, factored out of the RunE so it is testable.
func runInspect(ctx context.Context, cfg inspectConfig) error {
	addr, err := inspectAddr(cfg.port)
	if err != nil {
		return err
	}

	var relay *inspector.Relay
	serverInfo := inspector.ServerInfo{Name: "inspector", Transport: "detached"}
	if cfg.serverURL != "" {
		obsURL, infErr := obsStreamURLFor(cfg.serverURL)
		if infErr != nil {
			return infErr
		}
		relay = inspector.NewRelay(obsURL)
		serverInfo = inspector.ServerInfo{
			Name:      cfg.serverURL,
			Transport: "http",
		}
	}

	insp, err := inspector.New(inspector.Options{
		Addr:       addr,
		Relay:      relay,
		Assets:     inspector.EmbeddedAssets(),
		ServerInfo: serverInfo,
		Logger:     cfg.logger,
	})
	if err != nil {
		// A non-loopback bind is the expected, typed refusal — surface it
		// cleanly rather than as an internal CLI fault.
		return errf("%w", err)
	}
	defer func() { _ = insp.Close() }()

	if relay != nil {
		go relay.Run(ctx)
		defer func() { _ = relay.Close() }()
	}

	cfg.out("dockyard inspect: inspector serving at %s\n", insp.URL())
	if cfg.serverURL != "" {
		cfg.out("dockyard inspect: relaying obs/v1 from %s\n", cfg.serverURL)
	}
	if !cfg.noOpen {
		openBrowser(cfg.logger, insp.URL())
	}

	if err := insp.Serve(ctx); err != nil {
		return errf("inspect: %w", err)
	}
	return nil
}

// inspectAddr resolves the --port flag to the inspector's loopback bind
// address. Port 0 (the default) selects an OS-assigned loopback port. The host
// is always 127.0.0.1 — the inspector is localhost-only by construction; a
// caller cannot widen the bind through `dockyard inspect` (RFC §12). A negative
// or out-of-range port is a typed error.
func inspectAddr(port int) (string, error) {
	if port < 0 || port > 65535 {
		return "", errf("inspect: --port %d is out of range (0-65535)", port)
	}
	return fmt.Sprintf("127.0.0.1:%d", port), nil
}

// obsStreamURLFor derives a server's obs/v1 SSE stream URL from its base URL.
// A bare base ("http://127.0.0.1:8080") gets the canonical "/obs/v1/stream"
// path appended; a URL that already names a stream path is used verbatim. A
// non-http(s) or malformed URL is a typed error.
func obsStreamURLFor(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errf("inspect: --url %q is not a valid URL: %v", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errf("inspect: --url %q must be an http(s) URL", raw)
	}
	if u.Host == "" {
		return "", errf("inspect: --url %q is missing a host", raw)
	}
	if strings.Contains(u.Path, "/obs/") {
		return u.String(), nil
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/obs/v1/stream"
	return u.String(), nil
}

// openBrowser best-effort opens the system browser at url. A failure is logged
// and ignored — `dockyard inspect` still serves the inspector; --no-open
// suppresses the attempt entirely (for CI / headless use).
func openBrowser(logger *slog.Logger, url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	if err := exec.Command(cmd, args...).Start(); err != nil { //nolint:gosec // fixed command, inspector URL
		logger.Warn("dockyard inspect: could not open a browser",
			slog.String("url", url), slog.String("error", err.Error()))
	}
}

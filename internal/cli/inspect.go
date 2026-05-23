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
// inspect` is the standalone entry into that surface. RFC §12 also names an
// automatic attach inside `dockyard dev`; that auto-attach is a deferred seam
// (D-101) and is not yet implemented — `dockyard inspect` is the only shipping
// entry point. The inspector binds a loopback address only: a non-loopback
// `--port` host is rejected by
// internal/inspector's ErrNonLoopbackBind gate before the listener opens, the
// mechanical enforcement of RFC §12 and the CVE-2025-49596 lesson.
//
// inspect wraps internal/inspector, the reusable inspector backend; the
// orchestration is a thin RunE, not logic buried here.
func newInspectCmd() *cobra.Command {
	var (
		serverURL string
		dir       string
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

  --url      the running MCP server's base URL (e.g. http://127.0.0.1:8080);
             the inspector relays its obs stream and reads its ui:// Apps.
  --dir      the Dockyard project directory (default: the current directory);
             sources the contract verdicts and the generated tool contracts.
  --port     the inspector's own loopback port (default: an OS-assigned port).
  --no-open  do not open a browser — for CI and headless use.

The Verdicts panel and the Fixtures switcher are sourced from the project at
--dir: the verdicts re-run 'dockyard validate', the fixtures derive from the
project's generated tool contracts (P1). When --dir names no Dockyard project,
those panels degrade to their honest empty state. The App preview reads the
attached server's ui:// resources read-only.

The inspector is dev-mode-gated, localhost-only, and read-only: it is never a
production MCP client and never reachable off-localhost. A non-loopback bind is
refused before the listener opens. Press Ctrl-C to stop.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectDir, err := resolveProjectDir(dir)
			if err != nil {
				return err
			}
			return runInspect(cmd.Context(), inspectConfig{
				serverURL:  serverURL,
				projectDir: projectDir,
				port:       port,
				noOpen:     noOpen,
				logger: slog.New(slog.NewTextHandler(cmd.ErrOrStderr(),
					&slog.HandlerOptions{Level: slog.LevelInfo})),
				out: func(format string, args ...any) {
					fmt.Fprintf(cmd.OutOrStdout(), format, args...) //nolint:errcheck // CLI stdout write
				},
			})
		},
	}

	cmd.Flags().StringVar(&serverURL, "url", "",
		"the running MCP server's base URL (its obs stream is relayed, its Apps read)")
	cmd.Flags().StringVar(&dir, "dir", "",
		"project directory — sources verdicts and contracts (default: current directory)")
	cmd.Flags().IntVar(&port, "port", 0,
		"the inspector's loopback port (default: an OS-assigned port)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false,
		"do not open a browser (for CI / headless use)")
	return cmd
}

// inspectConfig is the resolved input to runInspect — extracted so the
// orchestration is unit-testable without a cobra command.
type inspectConfig struct {
	serverURL  string
	projectDir string
	port       int
	noOpen     bool
	logger     *slog.Logger
	out        func(format string, args ...any)
}

// runInspect serves the inspector backend until ctx is cancelled. It is the
// `dockyard inspect` orchestration, factored out of the RunE so it is testable.
func runInspect(ctx context.Context, cfg inspectConfig) error {
	addr, err := inspectAddr(cfg.port)
	if err != nil {
		return err
	}

	var relay *inspector.Relay
	var appSource inspector.AppSource
	var invoker inspector.ToolInvoker
	var elicitor inspector.Elicitor
	serverInfo := inspector.ServerInfo{Name: "inspector", Transport: "detached"}
	if cfg.serverURL != "" {
		obsURL, infErr := obsStreamURLFor(cfg.serverURL)
		if infErr != nil {
			return infErr
		}
		relay = inspector.NewRelay(obsURL)
		// The App-preview source reads the server's ui:// resources read-only
		// (RFC §12 line 711, D-103) — it takes the bare MCP base URL, not the
		// derived obs stream URL.
		appSource = inspector.AppsFromServer(cfg.serverURL)
		// The operator-initiated tools/call surface (D-131) — issues a real
		// tools/call to the attached server when the operator presses Invoke
		// in the inspector UI. Same short-lived-session pattern as appSource;
		// the listener's loopback gate keeps it dev-only.
		invoker = inspector.ToolsFromServer(cfg.serverURL)
		// The operator-initiated elicitation-response surface (Phase 25 /
		// D-134) — delivers an App's reply to an `input_required` task to
		// the attached server's `tasks/result` endpoint. Same short-lived,
		// localhost-only posture as the invoker; the operator is the one
		// driving the write through the App's Approve / Reject button.
		elicitor = inspector.ElicitationFromServer(cfg.serverURL)
		serverInfo = inspector.ServerInfo{
			Name:      cfg.serverURL,
			Transport: "http",
		}
	}

	// The Verdicts panel re-runs `dockyard validate` against the project, the
	// Fixtures switcher derives synthetic fixtures from the project's generated
	// tool contracts (P1), and the on-disk fixture loader surfaces the realistic
	// `fixtures/<tool>/<kind>.json` payloads the template ships (Phase 24,
	// D-126). All three are sourced from the project at --dir; when --dir
	// names no Dockyard project the sources degrade to an honest empty state
	// rather than crashing.
	var verdicts inspector.VerdictSource
	var contracts inspector.ContractsSource
	var fixtures inspector.FixtureSource
	if cfg.projectDir != "" {
		verdicts = inspector.VerdictsFromValidate(cfg.projectDir)
		contracts = inspector.ContractsFromProject(cfg.projectDir)
		fixtures = inspector.FixturesFromDir(cfg.projectDir)
	}

	insp, err := inspector.New(inspector.Options{
		Addr:       addr,
		Relay:      relay,
		Assets:     inspector.EmbeddedAssets(),
		ServerInfo: serverInfo,
		Verdicts:   verdicts,
		Contracts:  contracts,
		Apps:       appSource,
		Fixtures:   fixtures,
		Invoker:    invoker,
		Elicitor:   elicitor,
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

package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/hurtener/dockyard/internal/installpkg"
)

// newInstallCmd builds the `dockyard install` command — the host-registration
// verb (RFC §14, §9.1). It is a thin wrapper over internal/installpkg: the
// config-merge and boot-check logic is a reusable, testable package, not logic
// buried in this RunE.
//
// `dockyard install claude|cursor` writes the MCP host's config so the host
// launches this Dockyard server as a local stdio subprocess, then verifies the
// server boots with a real MCP initialize handshake. The write is
// non-destructive — the prior config is backed up and unrelated host entries
// are preserved. install writes a HOST config; it is not an MCP client (P4).
func newInstallCmd() *cobra.Command {
	var (
		dir        string
		binary     string
		configPath string
	)

	cmd := &cobra.Command{
		Use:   "install <claude|cursor>",
		Short: "Register the built server with an MCP host (Claude, Cursor)",
		Long: `Register a built Dockyard server with an MCP host (RFC §14).

'dockyard install claude' (or 'cursor') writes the host's MCP config file so
the host launches this Dockyard server as a local stdio subprocess, then
verifies the server boots by spawning it and driving a real MCP initialize
handshake.

The write is non-destructive: the prior config is backed up to a timestamped
sidecar and every unrelated MCP-server entry is preserved. Pass --binary to
point at the built server (default: build the project first with
'dockyard build'); --config overrides the host config-file path.

This writes a HOST config — it is not an MCP client. The boot check is a
throwaway, localhost, dev-only spawn.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := installpkg.ParseHost(args[0])
			if err != nil {
				return errf("%w", err)
			}
			projectDir, err := resolveProjectDir(dir)
			if err != nil {
				return err
			}
			binaryPath, err := resolveInstallBinary(projectDir, binary)
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(),
				&slog.HandlerOptions{Level: slog.LevelInfo}))

			res, err := installpkg.Install(cmd.Context(), installpkg.Options{
				ProjectDir: projectDir,
				Host:       host,
				ConfigPath: configPath,
				BinaryPath: binaryPath,
				Logger:     logger,
			})
			// A boot-check failure still wrote the config — report both.
			if err != nil {
				if errors.Is(err, installpkg.ErrBootCheck) {
					printInstallResult(cmd.OutOrStdout(), res)
				}
				return errf("install: %w", err)
			}
			printInstallResult(cmd.OutOrStdout(), res)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "",
		"project directory (default: current directory)")
	cmd.Flags().StringVar(&binary, "binary", "",
		"path to the built server binary (default: <project>/dist/<name>-<os>-<arch>)")
	cmd.Flags().StringVar(&configPath, "config", "",
		"host MCP config file path (default: the host's per-OS location)")
	return cmd
}

// resolveInstallBinary turns an explicit --binary into an absolute path, or
// derives the default: the host-platform artifact `dockyard build` writes,
// <project>/dist/<project-name>-<goos>-<goarch>[.exe]. The default is a
// convenience — a developer who built elsewhere passes --binary explicitly.
func resolveInstallBinary(projectDir, binary string) (string, error) {
	if binary != "" {
		abs, err := filepath.Abs(binary)
		if err != nil {
			return "", errf("resolve --binary %q: %w", binary, err)
		}
		return abs, nil
	}
	name := filepath.Base(projectDir)
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	artifact := fmt.Sprintf("%s-%s-%s%s", name, runtime.GOOS, runtime.GOARCH, suffix)
	return filepath.Join(projectDir, "dist", artifact), nil
}

// printInstallResult writes the post-install summary: the host, the config
// path written, the backup, and the boot-check verdict.
func printInstallResult(w io.Writer, res installpkg.Result) {
	fmt.Fprintf(w, "install: registered %q with %s\n", res.ServerName, res.Host) //nolint:errcheck // CLI stdout write
	fmt.Fprintf(w, "  config  %s\n", res.ConfigPath)                             //nolint:errcheck // CLI stdout write
	if res.BackupPath != "" {
		fmt.Fprintf(w, "  backup  %s\n", res.BackupPath) //nolint:errcheck // CLI stdout write
	}
	if res.BootOK {
		fmt.Fprint(w, "  boot    OK — server completed the MCP initialize handshake\n") //nolint:errcheck // CLI stdout write
	} else {
		fmt.Fprint(w, "  boot    FAILED — config written, but the server did not boot cleanly\n") //nolint:errcheck // CLI stdout write
	}
}

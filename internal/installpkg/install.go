package installpkg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hurtener/dockyard/internal/manifest"
)

// ErrInstall is the sentinel wrapping a `dockyard install` failure that is not
// a boot-check failure — a missing project, an unwritable or malformed config,
// an unknown host. Callers branch with errors.Is(err, ErrInstall).
var ErrInstall = errors.New("dockyard/internal/installpkg: install failed")

// ErrBootCheck is the sentinel wrapping a boot-check failure: the host config
// was written, but the spawned server did not complete modern MCP discovery or
// a recognized legacy fallback. It is distinct from ErrInstall so a caller can tell "config not
// written" from "config written but server unhealthy".
var ErrBootCheck = errors.New("dockyard/internal/installpkg: server boot check failed")

// Options configures one `dockyard install` invocation.
type Options struct {
	// ProjectDir is the root of the Dockyard project — the directory holding
	// dockyard.app.yaml. Required: the manifest supplies the server name the
	// host config entry is keyed under.
	ProjectDir string
	// Host is the MCP host to register the server with. Required.
	Host Host
	// ConfigPath overrides the per-OS host config-file path. Empty uses the
	// host default. Tests set it to a temp path so the developer's real
	// ~/.claude / Cursor config is never touched.
	ConfigPath string
	// BinaryPath is the absolute path of the built server binary the host
	// config will launch. Required.
	BinaryPath string
	// SkipBootCheck disables the post-write boot check. It is a test seam —
	// production installs always verify the server boots. Normal use leaves
	// it false.
	SkipBootCheck bool
	// Logger receives the install's structured output. A nil Logger falls
	// back to a discarding logger.
	Logger *slog.Logger
}

// Result reports what an Install produced.
type Result struct {
	// Host is the host the server was registered with.
	Host Host
	// ConfigPath is the absolute path of the host config file written.
	ConfigPath string
	// BackupPath is the absolute path of the backup of the prior config, or
	// "" when there was no prior config to back up.
	BackupPath string
	// ServerName is the key the server entry was written under.
	ServerName string
	// BootOK reports whether the post-write boot check passed. It is false
	// when SkipBootCheck was set.
	BootOK bool
}

// hostConfig is the minimal shape of an MCP host config file Dockyard reads
// and writes. Both Claude Desktop and Cursor key MCP servers under an
// "mcpServers" object. Any unrecognised top-level field is preserved verbatim
// through the Extra map so a merge never drops host settings Dockyard does not
// model.
type hostConfig struct {
	MCPServers map[string]serverEntry `json:"mcpServers"`
	// Extra captures every other top-level key so a non-destructive write
	// round-trips the host's own settings untouched.
	Extra map[string]json.RawMessage `json:"-"`
}

// serverEntry is one MCP server registration: the command the host launches
// and its arguments. A Dockyard server is a local stdio subprocess, so the
// entry is just the binary path (RFC §14: "the host config is just
// {"command": "/path/to/app"}").
type serverEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Install registers the project's built server with an MCP host (RFC §14): it
// writes the host's MCP config non-destructively — backing up the prior file,
// merging this server's entry into the existing mcpServers map without
// touching any unrelated entry — then verifies the server boots with a real
// modern server/discover negotiation, with explicit legacy fallback.
//
// A failure to write the config wraps ErrInstall; a config that was written
// but whose server failed to boot wraps ErrBootCheck. On the latter the
// Result is still returned (the config IS written) so the caller can report
// both facts.
func Install(ctx context.Context, opts Options) (Result, error) {
	logger := opts.logger()

	if opts.ProjectDir == "" {
		return Result{}, fmt.Errorf("%w: ProjectDir is required", ErrInstall)
	}
	if opts.BinaryPath == "" {
		return Result{}, fmt.Errorf("%w: BinaryPath is required", ErrInstall)
	}
	profile, err := profileFor(opts.Host)
	if err != nil {
		return Result{}, err
	}
	binaryPath, err := filepath.Abs(opts.BinaryPath)
	if err != nil {
		return Result{}, fmt.Errorf("%w: resolve binary path: %w", ErrInstall, err)
	}
	if info, statErr := os.Stat(binaryPath); statErr != nil || info.IsDir() {
		return Result{}, fmt.Errorf(
			"%w: server binary %s not found — run 'dockyard build' first", ErrInstall, binaryPath)
	}

	serverName, err := readServerName(opts.ProjectDir)
	if err != nil {
		return Result{}, err
	}
	configPath, err := resolveConfigPath(profile, opts.ConfigPath)
	if err != nil {
		return Result{}, err
	}

	// --- non-destructive merge --------------------------------------------
	cfg, err := loadHostConfig(configPath)
	if err != nil {
		return Result{}, err
	}
	backupPath, err := backupConfig(configPath)
	if err != nil {
		return Result{}, err
	}
	cfg.MCPServers[serverName] = serverEntry{Command: binaryPath}
	if err := writeHostConfig(configPath, cfg); err != nil {
		return Result{}, err
	}
	logger.InfoContext(ctx, "install: host config written",
		slog.String("host", string(opts.Host)),
		slog.String("config", configPath),
		slog.String("server", serverName))

	res := Result{
		Host:       opts.Host,
		ConfigPath: configPath,
		BackupPath: backupPath,
		ServerName: serverName,
	}

	// --- boot check --------------------------------------------------------
	if opts.SkipBootCheck {
		logger.WarnContext(ctx, "install: boot check skipped (test seam)")
		return res, nil
	}
	logger.InfoContext(ctx, "install: verifying server boots")
	if err := bootCheck(ctx, binaryPath); err != nil {
		// The config IS written; the server is just not yet runnable. Return
		// the Result alongside the error so the caller reports both.
		return res, err
	}
	res.BootOK = true
	logger.InfoContext(ctx, "install: server boot check passed")
	return res, nil
}

// readServerName loads the project manifest and returns the server name the
// host config entry is keyed under.
func readServerName(projectDir string) (string, error) {
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		return "", fmt.Errorf("%w: load manifest: %w", ErrInstall, err)
	}
	if m.Name == "" {
		return "", fmt.Errorf("%w: manifest has no server name", ErrInstall)
	}
	return m.Name, nil
}

// loadHostConfig reads and parses the host's MCP config file. A missing file
// is not an error — it yields an empty config the merge populates (a
// first-time install). A file that exists but is not valid JSON is a clear,
// typed error: Install must never clobber a config it cannot understand.
func loadHostConfig(path string) (hostConfig, error) {
	cfg := hostConfig{MCPServers: map[string]serverEntry{}, Extra: map[string]json.RawMessage{}}

	data, err := os.ReadFile(path) //nolint:gosec // path is a host config the user pointed install at
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return hostConfig{}, fmt.Errorf("%w: read host config %s: %w", ErrInstall, path, err)
	}
	if len(data) == 0 {
		return cfg, nil
	}

	// Decode into a raw map first so every top-level key the host owns is
	// preserved — Dockyard only models mcpServers and must not drop the rest.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return hostConfig{}, fmt.Errorf(
			"%w: host config %s is not valid JSON — fix or remove it, install will not overwrite it: %w",
			ErrInstall, path, err)
	}
	for key, val := range raw {
		if key == "mcpServers" {
			if err := json.Unmarshal(val, &cfg.MCPServers); err != nil {
				return hostConfig{}, fmt.Errorf(
					"%w: host config %s has a malformed mcpServers block: %w", ErrInstall, path, err)
			}
			continue
		}
		cfg.Extra[key] = val
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]serverEntry{}
	}
	return cfg, nil
}

// writeHostConfig serialises the merged config back to path, creating the
// parent directory if the host config dir does not yet exist. The mcpServers
// block and every preserved Extra key are written; the output is indented so
// a developer can read and hand-edit it.
func writeHostConfig(path string, cfg hostConfig) error {
	merged := map[string]json.RawMessage{}
	for key, val := range cfg.Extra {
		merged[key] = val
	}
	serversJSON, err := json.Marshal(cfg.MCPServers)
	if err != nil {
		return fmt.Errorf("%w: encode mcpServers: %w", ErrInstall, err)
	}
	merged["mcpServers"] = serversJSON

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode host config: %w", ErrInstall, err)
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // a host config dir is browsable, not a secret
		return fmt.Errorf("%w: create host config dir: %w", ErrInstall, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil { //nolint:gosec // a host config is not a secret
		return fmt.Errorf("%w: write host config %s: %w", ErrInstall, path, err)
	}
	return nil
}

// backupConfig copies an existing host config to a timestamped sidecar before
// it is overwritten, so a developer can roll back. A missing config has
// nothing to back up — it returns ("", nil).
func backupConfig(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a host config the user pointed install at
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: read host config for backup: %w", ErrInstall, err)
	}
	backupPath := fmt.Sprintf("%s.dockyard-backup.%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(backupPath, data, 0o644); err != nil { //nolint:gosec // a host config backup is not a secret
		return "", fmt.Errorf("%w: write host config backup: %w", ErrInstall, err)
	}
	return backupPath, nil
}

// logger returns opts.Logger or a discarding logger.
func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.DiscardHandler)
}

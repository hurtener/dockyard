package installpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Host identifies an MCP host `dockyard install` can register a server with.
type Host string

const (
	// HostClaude is Claude Desktop — its config is claude_desktop_config.json
	// under the per-OS Claude application-support directory.
	HostClaude Host = "claude"
	// HostCursor is the Cursor editor — its MCP config is ~/.cursor/mcp.json.
	HostCursor Host = "cursor"
)

// ParseHost validates a host argument and returns the typed Host. An unknown
// value is a clear, typed error naming the supported hosts.
func ParseHost(s string) (Host, error) {
	switch Host(s) {
	case HostClaude:
		return HostClaude, nil
	case HostCursor:
		return HostCursor, nil
	default:
		return "", fmt.Errorf("%w: unknown host %q — use 'claude' or 'cursor'", ErrInstall, s)
	}
}

// hostProfile is the small per-host structure that derives an MCP host's
// config-file location. It is deliberately NOT a sprawling capability matrix
// (CLAUDE.md §6) — it carries one thing, a filesystem-path derivation, so a
// host's config-location change is localized to one function here.
type hostProfile struct {
	// host is the host this profile is for.
	host Host
	// configPath derives the absolute MCP config-file path for the host on the
	// current OS, given the user's home directory. It is a function so the
	// per-OS branching lives in one place per host.
	configPath func(home string) (string, error)
}

// profileFor returns the hostProfile for a Host.
func profileFor(h Host) (hostProfile, error) {
	switch h {
	case HostClaude:
		return hostProfile{host: HostClaude, configPath: claudeConfigPath}, nil
	case HostCursor:
		return hostProfile{host: HostCursor, configPath: cursorConfigPath}, nil
	default:
		return hostProfile{}, fmt.Errorf("%w: no profile for host %q", ErrInstall, h)
	}
}

// resolveConfigPath returns the host's MCP config-file path. An explicit
// override (Options.ConfigPath — used by tests so the real user config is
// never touched) is returned as-is; otherwise the per-OS host default is
// derived.
func resolveConfigPath(p hostProfile, override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: locate home directory: %w", ErrInstall, err)
	}
	return p.configPath(home)
}

// claudeConfigPath derives Claude Desktop's claude_desktop_config.json path
// for the current OS.
func claudeConfigPath(home string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude",
			"claude_desktop_config.json"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json"), nil
	default: // linux and other unixes
		return filepath.Join(home, ".config", "Claude",
			"claude_desktop_config.json"), nil
	}
}

// cursorConfigPath derives Cursor's mcp.json path. Cursor uses a single
// ~/.cursor/mcp.json across operating systems.
func cursorConfigPath(home string) (string, error) {
	return filepath.Join(home, ".cursor", "mcp.json"), nil
}

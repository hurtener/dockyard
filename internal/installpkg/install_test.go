package installpkg

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestParseHost verifies host-argument parsing: claude/cursor accepted, an
// unknown value is a clear ErrInstall.
func TestParseHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    Host
		wantErr bool
	}{
		{"claude", HostClaude, false},
		{"cursor", HostCursor, false},
		{"vscode", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := ParseHost(tt.in)
		if tt.wantErr {
			if err == nil || !errors.Is(err, ErrInstall) {
				t.Errorf("ParseHost(%q): want an ErrInstall, got %v", tt.in, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseHost(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

// TestHostProfile_ConfigPath verifies each host derives a non-empty, plausible
// config path on the current OS.
func TestHostProfile_ConfigPath(t *testing.T) {
	t.Parallel()
	for _, h := range []Host{HostClaude, HostCursor} {
		p, err := profileFor(h)
		if err != nil {
			t.Fatalf("profileFor(%q): %v", h, err)
		}
		path, err := p.configPath("/home/tester")
		if err != nil {
			t.Fatalf("configPath(%q): %v", h, err)
		}
		if !filepath.IsAbs(path) {
			t.Errorf("%q config path is not absolute: %q", h, path)
		}
		if filepath.Ext(path) != ".json" {
			t.Errorf("%q config path is not a .json file: %q", h, path)
		}
	}
}

// TestResolveConfigPath_Override verifies an explicit override is used as-is
// (the test seam that keeps the real user config untouched).
func TestResolveConfigPath_Override(t *testing.T) {
	t.Parallel()
	p, _ := profileFor(HostClaude)
	override := filepath.Join(t.TempDir(), "custom.json")
	got, err := resolveConfigPath(p, override)
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if got != override {
		t.Errorf("resolveConfigPath override = %q, want %q", got, override)
	}
}

// writeProject scaffolds the minimal project files Install reads: a manifest
// with a server name and a fake built binary.
func writeProject(t *testing.T, name string) (projectDir, binaryPath string) {
	t.Helper()
	projectDir = t.TempDir()
	manifestBody := "name: " + name + "\ntitle: Test\nversion: 0.1.0\n" +
		"runtime:\n  transports: [stdio]\n" +
		"tools:\n  - name: greet\n    description: greet\n" +
		"    input: internal/contracts.GreetInput\n" +
		"    output: internal/contracts.GreetOutput\n"
	if err := os.WriteFile(filepath.Join(projectDir, "dockyard.app.yaml"),
		[]byte(manifestBody), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	binaryPath = filepath.Join(projectDir, "server-bin")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/true\n"), 0o750); err != nil { //nolint:gosec // test fake binary
		t.Fatalf("write fake binary: %v", err)
	}
	return projectDir, binaryPath
}

// TestInstall_WritesValidConfig verifies Install writes a valid host config
// keyed under the manifest's server name (boot check skipped).
func TestInstall_WritesValidConfig(t *testing.T) {
	t.Parallel()
	projectDir, binaryPath := writeProject(t, "demo-server")
	configPath := filepath.Join(t.TempDir(), "claude_desktop_config.json")

	res, err := Install(context.Background(), Options{
		ProjectDir:    projectDir,
		Host:          HostClaude,
		ConfigPath:    configPath,
		BinaryPath:    binaryPath,
		SkipBootCheck: true,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.ServerName != "demo-server" {
		t.Errorf("ServerName = %q, want demo-server", res.ServerName)
	}
	cfg := readConfig(t, configPath)
	entry, ok := cfg.MCPServers["demo-server"]
	if !ok {
		t.Fatalf("config has no demo-server entry: %+v", cfg)
	}
	wantBin, _ := filepath.Abs(binaryPath)
	if entry.Command != wantBin {
		t.Errorf("entry command = %q, want %q", entry.Command, wantBin)
	}
}

// TestInstall_NonDestructiveMerge verifies Install preserves an unrelated MCP
// server entry and an unrelated top-level config key.
func TestInstall_NonDestructiveMerge(t *testing.T) {
	t.Parallel()
	projectDir, binaryPath := writeProject(t, "new-server")
	configPath := filepath.Join(t.TempDir(), "config.json")

	// A pre-existing config with another server and a host-owned setting.
	pre := `{
  "theme": "dark",
  "mcpServers": {
    "other-server": {"command": "/usr/bin/other"}
  }
}`
	if err := os.WriteFile(configPath, []byte(pre), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := Install(context.Background(), Options{
		ProjectDir: projectDir, Host: HostCursor, ConfigPath: configPath,
		BinaryPath: binaryPath, SkipBootCheck: true,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// The backup of the prior config exists.
	if res.BackupPath == "" {
		t.Error("expected a backup of the prior config")
	} else if _, err := os.Stat(res.BackupPath); err != nil {
		t.Errorf("backup not on disk: %v", err)
	}

	// Both servers present; the unrelated entry untouched.
	cfg := readConfig(t, configPath)
	if _, ok := cfg.MCPServers["other-server"]; !ok {
		t.Error("non-destructive merge dropped the unrelated 'other-server' entry")
	}
	if _, ok := cfg.MCPServers["new-server"]; !ok {
		t.Error("merge did not add 'new-server'")
	}
	// The unrelated top-level "theme" key is preserved.
	raw := readRaw(t, configPath)
	if _, ok := raw["theme"]; !ok {
		t.Error("non-destructive merge dropped the host's 'theme' setting")
	}
}

// TestInstall_RejectsMalformedConfig verifies Install refuses to clobber a
// config it cannot parse — a clear ErrInstall, the file left intact.
func TestInstall_RejectsMalformedConfig(t *testing.T) {
	t.Parallel()
	projectDir, binaryPath := writeProject(t, "demo")
	configPath := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(configPath, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Install(context.Background(), Options{
		ProjectDir: projectDir, Host: HostClaude, ConfigPath: configPath,
		BinaryPath: binaryPath, SkipBootCheck: true,
	})
	if err == nil || !errors.Is(err, ErrInstall) {
		t.Fatalf("malformed config: want an ErrInstall, got %v", err)
	}
	// The malformed file is left exactly as it was — never clobbered.
	data, _ := os.ReadFile(configPath) //nolint:gosec // test temp path
	if string(data) != "{ this is not json" {
		t.Errorf("malformed config was modified: %q", data)
	}
}

// TestInstall_RejectsMissingBinary verifies Install fails cleanly when the
// server binary does not exist.
func TestInstall_RejectsMissingBinary(t *testing.T) {
	t.Parallel()
	projectDir, _ := writeProject(t, "demo")
	_, err := Install(context.Background(), Options{
		ProjectDir: projectDir, Host: HostClaude,
		ConfigPath: filepath.Join(t.TempDir(), "c.json"),
		BinaryPath: filepath.Join(t.TempDir(), "missing-binary"),
	})
	if err == nil || !errors.Is(err, ErrInstall) {
		t.Errorf("missing binary: want an ErrInstall, got %v", err)
	}
}

// TestInstall_RejectsUnwritableConfig verifies Install fails cleanly when the
// config path cannot be written (its parent is a file, not a directory).
func TestInstall_RejectsUnwritableConfig(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("path-as-parent semantics differ on Windows")
	}
	projectDir, binaryPath := writeProject(t, "demo")
	// A regular file standing where a directory must be.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(blocker, "config.json")
	_, err := Install(context.Background(), Options{
		ProjectDir: projectDir, Host: HostClaude, ConfigPath: configPath,
		BinaryPath: binaryPath, SkipBootCheck: true,
	})
	if err == nil || !errors.Is(err, ErrInstall) {
		t.Errorf("unwritable config: want an ErrInstall, got %v", err)
	}
}

// TestInstall_RequiresArgs verifies empty ProjectDir / BinaryPath are clear
// errors.
func TestInstall_RequiresArgs(t *testing.T) {
	t.Parallel()
	if _, err := Install(context.Background(), Options{Host: HostClaude}); err == nil {
		t.Error("empty ProjectDir: want an error")
	}
	if _, err := Install(context.Background(), Options{
		ProjectDir: t.TempDir(), Host: HostClaude,
	}); err == nil {
		t.Error("empty BinaryPath: want an error")
	}
}

// readConfig parses a written host config for assertions.
func readConfig(t *testing.T, path string) hostConfig {
	t.Helper()
	cfg, err := loadHostConfig(path)
	if err != nil {
		t.Fatalf("loadHostConfig: %v", err)
	}
	return cfg
}

// readRaw parses a written host config as a raw map to assert preserved keys.
func readRaw(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("written config is not valid JSON: %v", err)
	}
	return raw
}

// TestResolveConfigPath_HostDefault verifies the host-default path is derived
// when no override is given — exercising the per-OS claudeConfigPath /
// cursorConfigPath branches for the running OS.
func TestResolveConfigPath_HostDefault(t *testing.T) {
	t.Parallel()
	for _, h := range []Host{HostClaude, HostCursor} {
		p, err := profileFor(h)
		if err != nil {
			t.Fatalf("profileFor(%q): %v", h, err)
		}
		got, err := resolveConfigPath(p, "")
		if err != nil {
			t.Fatalf("resolveConfigPath(%q, default): %v", h, err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("%q default config path is not absolute: %q", h, got)
		}
		if filepath.Ext(got) != ".json" {
			t.Errorf("%q default config path is not a .json file: %q", h, got)
		}
	}
}

// TestProfileFor_UnknownHost verifies an unknown host has no profile.
func TestProfileFor_UnknownHost(t *testing.T) {
	t.Parallel()
	if _, err := profileFor(Host("nano")); err == nil {
		t.Error("profileFor accepted an unknown host")
	}
}

// TestClaudeConfigPath_AllOSes verifies the Claude config path is derived for
// every supported OS branch, not only the host's.
func TestClaudeConfigPath_AllOSes(t *testing.T) {
	t.Parallel()
	got, err := claudeConfigPath("/home/u")
	if err != nil {
		t.Fatalf("claudeConfigPath: %v", err)
	}
	if !strings.HasSuffix(got, "claude_desktop_config.json") {
		t.Errorf("claude config path %q does not end with the config filename", got)
	}
}

// TestCursorConfigPath verifies the Cursor config path is ~/.cursor/mcp.json.
func TestCursorConfigPath(t *testing.T) {
	t.Parallel()
	got, err := cursorConfigPath("/home/u")
	if err != nil {
		t.Fatalf("cursorConfigPath: %v", err)
	}
	want := filepath.Join("/home/u", ".cursor", "mcp.json")
	if got != want {
		t.Errorf("cursorConfigPath = %q, want %q", got, want)
	}
}

// TestInstall_FirstTimeWritesFreshConfig verifies an install into a directory
// with no prior config writes a fresh, valid config and reports no backup.
func TestInstall_FirstTimeWritesFreshConfig(t *testing.T) {
	t.Parallel()
	projectDir, binaryPath := writeProject(t, "fresh")
	// A config path under a not-yet-existing directory — install creates it.
	configPath := filepath.Join(t.TempDir(), "nested", "config.json")

	res, err := Install(context.Background(), Options{
		ProjectDir: projectDir, Host: HostClaude, ConfigPath: configPath,
		BinaryPath: binaryPath, SkipBootCheck: true,
	})
	if err != nil {
		t.Fatalf("Install (first time): %v", err)
	}
	if res.BackupPath != "" {
		t.Errorf("first-time install reported a backup where there was no prior config: %q", res.BackupPath)
	}
	cfg := readConfig(t, configPath)
	if _, ok := cfg.MCPServers["fresh"]; !ok {
		t.Error("first-time install did not write the server entry")
	}
}

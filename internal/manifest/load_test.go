package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadFile_ValidFull(t *testing.T) {
	m, err := LoadFile(filepath.Join("testdata", "valid-full.yaml"))
	if err != nil {
		t.Fatalf("LoadFile: unexpected error: %v", err)
	}

	if m.Name != "customer-health" {
		t.Errorf("Name = %q, want customer-health", m.Name)
	}
	if m.Title != "Customer Health" {
		t.Errorf("Title = %q, want Customer Health", m.Title)
	}
	if m.Version != "0.1.0" {
		t.Errorf("Version = %q, want 0.1.0", m.Version)
	}
	if got := m.Runtime.Transports; len(got) != 2 || got[0] != TransportStdio || got[1] != TransportHTTP {
		t.Errorf("Runtime.Transports = %v, want [stdio http]", got)
	}
	if m.Runtime.UI == nil {
		t.Fatal("Runtime.UI is nil, want populated")
	}
	// valid-full.yaml uses bundle: multi-file because its app opts into an
	// external csp.connect origin — a single-file bundle loads no external
	// origin, so the two are mutually exclusive (RFC §7.4).
	if m.Runtime.UI.Framework != UIFrameworkSvelte || m.Runtime.UI.Bundle != BundleMultiFile {
		t.Errorf("Runtime.UI = %+v, want svelte/multi-file", *m.Runtime.UI)
	}
	if len(m.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(m.Tools))
	}
	tool := m.Tools[0]
	if tool.Name != "show_customer_health" {
		t.Errorf("tool.Name = %q", tool.Name)
	}
	if tool.Input != "internal/contracts.ShowCustomerHealthInput" {
		t.Errorf("tool.Input = %q", tool.Input)
	}
	if tool.Output != "internal/contracts.ShowCustomerHealthOutput" {
		t.Errorf("tool.Output = %q", tool.Output)
	}
	if tool.UI != "customer_health" {
		t.Errorf("tool.UI = %q", tool.UI)
	}
	if tool.TaskSupport != TaskSupportOptional {
		t.Errorf("tool.TaskSupport = %q, want optional", tool.TaskSupport)
	}
	if len(m.Apps) != 1 {
		t.Fatalf("len(Apps) = %d, want 1", len(m.Apps))
	}
	app := m.Apps[0]
	if app.ID != "customer_health" || app.URI != "ui://customer-health/main" {
		t.Errorf("app id/uri = %q/%q", app.ID, app.URI)
	}
	if len(app.DisplayModes) != 2 {
		t.Errorf("len(DisplayModes) = %d, want 2", len(app.DisplayModes))
	}
	if got := app.CSP.Connect; len(got) != 1 || got[0] != "https://api.company.com" {
		t.Errorf("app.CSP.Connect = %v", got)
	}
	if !m.Quality.RequireLoadingState || !m.Quality.RequireSpecCompliance {
		t.Errorf("Quality gates not parsed: %+v", m.Quality)
	}
}

func TestLoadFile_ValidMinimal(t *testing.T) {
	m, err := LoadFile(filepath.Join("testdata", "valid-minimal.yaml"))
	if err != nil {
		t.Fatalf("LoadFile: unexpected error: %v", err)
	}
	if m.Runtime.UI != nil {
		t.Errorf("Runtime.UI = %+v, want nil for a plain server", m.Runtime.UI)
	}
	if len(m.Apps) != 0 {
		t.Errorf("len(Apps) = %d, want 0 for a plain server", len(m.Apps))
	}
	if m.Tools[0].TaskSupport != "" {
		t.Errorf("omitted TaskSupport = %q, want zero value", m.Tools[0].TaskSupport)
	}
}

// TestLoadFile_ExampleManifest is the RFC §4.2 acceptance: the shipped example
// manifest round-trips load -> validate -> no error.
func TestLoadFile_ExampleManifest(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "customer-health", DefaultFilename)
	m, err := LoadFile(path)
	if err != nil {
		t.Fatalf("example manifest failed to load: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("example manifest failed to validate: %v", err)
	}
	if m.Name != "customer-health" {
		t.Errorf("example Name = %q", m.Name)
	}
}

func TestLoadFile_MissingFile(t *testing.T) {
	_, err := LoadFile(filepath.Join("testdata", "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("LoadFile of a missing file: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("error does not wrap ErrInvalidManifest: %v", err)
	}
}

func TestLoad_Empty(t *testing.T) {
	_, err := Load(strings.NewReader(""), "empty.yaml")
	if err == nil {
		t.Fatal("Load of empty input: want error, got nil")
	}
	var me *Error
	if !errors.As(err, &me) {
		t.Fatalf("error is not *Error: %T", err)
	}
	if me.Source != "empty.yaml" {
		t.Errorf("Error.Source = %q, want empty.yaml", me.Source)
	}
}

func TestLoad_SyntaxError_IsSourceLocated(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "bad-syntax.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Load(strings.NewReader(string(raw)), "bad-syntax.yaml")
	if err == nil {
		t.Fatal("Load of malformed YAML: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("syntax error does not wrap ErrInvalidManifest: %v", err)
	}
	if !strings.Contains(err.Error(), "bad-syntax.yaml") {
		t.Errorf("syntax error not source-located: %q", err.Error())
	}
}

func TestLoad_UnknownField_Rejected(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "bad-unknown-field.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Load(strings.NewReader(string(raw)), "bad-unknown-field.yaml")
	if err == nil {
		t.Fatal("Load with an unknown field: want error, got nil")
	}
	if !strings.Contains(err.Error(), "mascot") {
		t.Errorf("error should name the unknown field: %q", err.Error())
	}
}

// TestLoad_ConcurrentReadAfterLoad proves a loaded Manifest is safe for
// concurrent read — the accessors only read (AGENTS.md §5, reusable artifact).
func TestLoad_ConcurrentReadAfterLoad(t *testing.T) {
	m, err := LoadFile(filepath.Join("testdata", "valid-full.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := m.Tool("show_customer_health"); !ok {
				t.Error("Tool lookup failed under concurrency")
			}
			if _, ok := m.App("customer_health"); !ok {
				t.Error("App lookup failed under concurrency")
			}
			_ = m.Validate()
		}()
	}
	wg.Wait()
}

func TestManifest_ToolAndApp_Lookup(t *testing.T) {
	m, err := LoadFile(filepath.Join("testdata", "valid-full.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Tool("missing"); ok {
		t.Error("Tool(missing) returned ok=true")
	}
	if _, ok := m.App("missing"); ok {
		t.Error("App(missing) returned ok=true")
	}
}

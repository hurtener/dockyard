package generate

import (
	"errors"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/manifest"
)

// testManifest is a minimal valid manifest with one tool whose contracts live
// in internal/contracts — the shape `dockyard new` scaffolds.
func testManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	src := `name: demo
title: Demo
version: 0.1.0
runtime:
  transports: [stdio]
tools:
  - name: greet
    description: Greet someone.
    input: internal/contracts.GreetInput
    output: internal/contracts.GreetOutput
    task_support: forbidden
`
	m, err := manifest.Load(strings.NewReader(src), "test")
	if err != nil {
		t.Fatalf("load test manifest: %v", err)
	}
	return m
}

func TestRun_RequiresProjectDir(t *testing.T) {
	t.Parallel()
	_, err := Run(Options{Manifest: testManifest(t)})
	if !errors.Is(err, ErrGenerate) {
		t.Fatalf("Run with no ProjectDir: want ErrGenerate, got %v", err)
	}
}

func TestRun_RequiresManifest(t *testing.T) {
	t.Parallel()
	_, err := Run(Options{ProjectDir: t.TempDir()})
	if !errors.Is(err, ErrGenerate) {
		t.Fatalf("Run with no Manifest: want ErrGenerate, got %v", err)
	}
}

func TestPlan_MissingContractsDir(t *testing.T) {
	t.Parallel()
	// A directory that is a project root but has no internal/contracts.
	_, err := Plan(Options{ProjectDir: t.TempDir(), Manifest: testManifest(t)})
	if !errors.Is(err, ErrGenerate) {
		t.Fatalf("Plan on a dir with no contracts: want ErrGenerate, got %v", err)
	}
	if !strings.Contains(err.Error(), ContractsDir) {
		t.Errorf("error should name the missing contracts dir, got: %v", err)
	}
}

func TestSchemaFileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool, side, want string
	}{
		{"greet", "input", "internal/contracts/greet_input.schema.json"},
		{"greet", "output", "internal/contracts/greet_output.schema.json"},
		{"show_revenue", "input", "internal/contracts/show_revenue_input.schema.json"},
	}
	for _, tt := range tests {
		if got := SchemaFileName(tt.tool, tt.side); got != tt.want {
			t.Errorf("SchemaFileName(%q, %q) = %q, want %q", tt.tool, tt.side, got, tt.want)
		}
	}
}

func TestTSFileName(t *testing.T) {
	t.Parallel()
	if got, want := TSFileName(), "internal/contracts/contracts.ts"; got != want {
		t.Errorf("TSFileName() = %q, want %q", got, want)
	}
}

func TestPlanRejectsNoncanonicalContractPackage(t *testing.T) {
	t.Parallel()
	m := testManifest(t)
	m.Tools[0].Input = "internal/other.GreetInput"
	_, err := Plan(Options{ProjectDir: t.TempDir(), Manifest: m})
	if !errors.Is(err, ErrGenerate) || !strings.Contains(err.Error(), `canonical package "internal/contracts"`) {
		t.Fatalf("Plan error = %v, want canonical package rejection", err)
	}
}

func TestGeneratedArtifactPathAndMarkerProfile(t *testing.T) {
	t.Parallel()
	if isGeneratedArtifactPath("README.md") || isGeneratedArtifactPath("internal/contracts/manual.json") {
		t.Fatal("arbitrary project paths must not be accepted as generated artifacts")
	}
	if !isGeneratedArtifactPath(SchemaFileName("greet", "output")) || !isGeneratedArtifactPath(TSFileName()) {
		t.Fatal("canonical schema and TypeScript paths must be accepted")
	}
	withoutMarker := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`)
	if err := validateGeneratedMarker(SchemaFileName("old", "output"), withoutMarker); err == nil {
		t.Fatal("schema without Dockyard's generated marker must not be removable")
	}
}

func TestResolveContractImports(t *testing.T) {
	t.Parallel()
	m := testManifest(t)
	imports, err := resolveContractImports(m, "example.com/demo")
	if err != nil {
		t.Fatalf("resolveContractImports: %v", err)
	}
	ci, ok := imports["internal/contracts"]
	if !ok {
		t.Fatalf("expected an entry keyed by the manifest-relative package, got %#v", imports)
	}
	if want := "example.com/demo/internal/contracts"; ci.path != want {
		t.Errorf("import path = %q, want %q", ci.path, want)
	}
	if ci.alias == "" {
		t.Error("import alias must be non-empty")
	}
}

func TestRenderGeneratorProgram(t *testing.T) {
	t.Parallel()
	m := testManifest(t)
	imports, err := resolveContractImports(m, "example.com/demo")
	if err != nil {
		t.Fatalf("resolveContractImports: %v", err)
	}
	src, err := renderGeneratorProgram(m, imports, map[string][]any{"Severity": {"info", "warn"}})
	if err != nil {
		t.Fatalf("renderGeneratorProgram: %v", err)
	}
	// The program must be a self-contained main that imports only public
	// packages and instantiates the tool builder per contract.
	for _, want := range []string{
		"package main",
		`"github.com/hurtener/dockyard/runtime/tool"`,
		`"example.com/demo/internal/contracts"`,
		`tool.InputSchemaFor[`,
		`tool.OutputSchemaFor[`,
		`tool.WithEnum("Severity"`,
		"GreetInput",
		"GreetOutput",
		"tool.MarshalSchema",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generator program missing %q\n--- program ---\n%s", want, src)
		}
	}
	// It must never import an internal/ Dockyard package — the project module
	// cannot.
	if strings.Contains(src, "hurtener/dockyard/internal/") {
		t.Errorf("generator program imports a Dockyard internal/ package — a scaffolded project cannot:\n%s", src)
	}
}

func TestRenderGeneratorProgram_NoTools(t *testing.T) {
	t.Parallel()
	m := &manifest.Manifest{}
	_, err := renderGeneratorProgram(m, nil, nil)
	if !errors.Is(err, ErrGenerate) {
		t.Fatalf("renderGeneratorProgram with no tools: want ErrGenerate, got %v", err)
	}
}

func TestReadModulePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/widget\n\ngo 1.26.2\n")
	got, err := readModulePath(dir)
	if err != nil {
		t.Fatalf("readModulePath: %v", err)
	}
	if want := "example.com/widget"; got != want {
		t.Errorf("readModulePath = %q, want %q", got, want)
	}
}

func TestReadModulePath_NoGoMod(t *testing.T) {
	t.Parallel()
	_, err := readModulePath(t.TempDir())
	if !errors.Is(err, ErrGenerate) {
		t.Fatalf("readModulePath with no go.mod: want ErrGenerate, got %v", err)
	}
}

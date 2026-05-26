package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/manifest"
)

func TestValidateName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"simple", "myserver", true},
		{"kebab", "my-mcp-server", true},
		{"digits", "server2", true},
		{"empty", "", false},
		{"uppercase", "MyServer", false},
		{"leading digit", "2server", false},
		{"leading hyphen", "-server", false},
		{"trailing hyphen", "server-", false},
		{"underscore", "my_server", false},
		{"slash", "my/server", false},
		{"too short", "a", false},
		{"space", "my server", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateName(tt.input)
			if tt.ok && err != nil {
				t.Errorf("validateName(%q) = %v, want nil", tt.input, err)
			}
			if !tt.ok {
				if err == nil {
					t.Errorf("validateName(%q) = nil, want error", tt.input)
				} else if !errors.Is(err, ErrInvalidName) {
					t.Errorf("validateName(%q) error not ErrInvalidName: %v", tt.input, err)
				}
			}
		})
	}
}

func TestGenerate_ProducesProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := Generate(Options{Name: "demo-server", Dir: dir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.Dir != filepath.Join(dir, "demo-server") {
		t.Errorf("Result.Dir = %q, want %q", res.Dir, filepath.Join(dir, "demo-server"))
	}

	// Every expected file exists on disk.
	want := []string{
		".gitignore",
		"README.md",
		"dockyard.app.yaml",
		"go.mod",
		"greet.go",
		"greet_test.go",
		"internal/contracts/contracts.go",
		"internal/contracts/contracts.ts",
		"internal/contracts/greet_input.schema.json",
		"internal/contracts/greet_output.schema.json",
		"main.go",
	}
	for _, rel := range want {
		full := filepath.Join(res.Dir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %s: %v", rel, err)
		}
	}
	if len(res.Files) != len(want) {
		t.Errorf("Result.Files has %d entries, want %d: %v", len(res.Files), len(want), res.Files)
	}
}

// TestScaffoldMainHonoursTransport proves the scaffolded main.go reads
// DOCKYARD_TRANSPORT and wires the HTTP transport — the Phase 20↔17 wiring-gap
// fix. A scaffold that only ever served stdio would make `dockyard run
// --transport http` a silent no-op.
func TestScaffoldMainHonoursTransport(t *testing.T) {
	t.Parallel()
	main := renderMainGo(Options{Name: "demo-server"})
	for _, want := range []string{
		`os.Getenv("DOCKYARD_TRANSPORT")`,
		`case "http":`,
		"HTTPHandler",
		"ServeStdio",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("scaffolded main.go does not reference %q — the transport seam is not wired", want)
		}
	}
}

func TestGenerate_RejectsInvalidName(t *testing.T) {
	t.Parallel()
	_, err := Generate(Options{Name: "Bad_Name", Dir: t.TempDir()})
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Generate with invalid name: err = %v, want ErrInvalidName", err)
	}
}

func TestGenerate_RejectsNonEmptyTarget(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	target := filepath.Join(parent, "occupied")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "x.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Generate(Options{Name: "occupied", Dir: parent})
	if !errors.Is(err, ErrTargetExists) {
		t.Fatalf("Generate into non-empty dir: err = %v, want ErrTargetExists", err)
	}
}

func TestGenerate_AllowsEmptyTarget(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	target := filepath.Join(parent, "empty")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// An empty pre-existing directory is a fine target — Generate writes into it.
	if _, err := Generate(Options{Name: "empty", Dir: parent}); err != nil {
		t.Fatalf("Generate into empty dir: %v", err)
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	t.Parallel()
	d1, d2 := t.TempDir(), t.TempDir()
	r1, err := Generate(Options{Name: "twice", Dir: d1})
	if err != nil {
		t.Fatalf("Generate 1: %v", err)
	}
	r2, err := Generate(Options{Name: "twice", Dir: d2})
	if err != nil {
		t.Fatalf("Generate 2: %v", err)
	}
	for i := range r1.Files {
		b1, _ := os.ReadFile(filepath.Join(r1.Dir, filepath.FromSlash(r1.Files[i])))
		b2, _ := os.ReadFile(filepath.Join(r2.Dir, filepath.FromSlash(r2.Files[i])))
		if string(b1) != string(b2) {
			t.Errorf("file %s differs between two identical generations — not deterministic", r1.Files[i])
		}
	}
}

func TestGenerate_ModulePathAndReplace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := Generate(Options{
		Name:            "custom-mod",
		Dir:             dir,
		ModulePath:      "github.com/acme/custom-mod",
		DockyardReplace: "/somewhere/dockyard",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	gomod, err := os.ReadFile(filepath.Join(res.Dir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	s := string(gomod)
	if !strings.Contains(s, "module github.com/acme/custom-mod") {
		t.Errorf("go.mod missing custom module path:\n%s", s)
	}
	if !strings.Contains(s, "replace github.com/hurtener/dockyard => /somewhere/dockyard") {
		t.Errorf("go.mod missing replace directive:\n%s", s)
	}
}

func TestGenerate_DefaultModulePathNoReplace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := Generate(Options{Name: "plain", Dir: dir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	gomod, _ := os.ReadFile(filepath.Join(res.Dir, "go.mod"))
	s := string(gomod)
	if !strings.Contains(s, "module example.com/plain") {
		t.Errorf("go.mod missing default module path:\n%s", s)
	}
	if strings.Contains(s, "replace") {
		t.Errorf("go.mod has a replace directive with no DockyardReplace set:\n%s", s)
	}
}

// TestGenerate_ContractsAreGenerated proves the contract artifacts carry the
// generated-code header — they are GENERATED from the Go structs, never
// hand-written (P1, CLAUDE.md §6/§13).
func TestGenerate_ContractsAreGenerated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := Generate(Options{Name: "gen-check", Dir: dir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ts, err := os.ReadFile(filepath.Join(res.Dir, "internal/contracts/contracts.ts"))
	if err != nil {
		t.Fatalf("read contracts.ts: %v", err)
	}
	if !strings.HasPrefix(string(ts), "// Code generated by dockyard; DO NOT EDIT.") {
		t.Error("contracts.ts is missing the generated-code header — a contract artifact must be generated, not hand-written")
	}
	for _, rel := range []string{
		"internal/contracts/greet_input.schema.json",
		"internal/contracts/greet_output.schema.json",
	} {
		b, err := os.ReadFile(filepath.Join(res.Dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(b), `"type": "object"`) {
			t.Errorf("%s is not a JSON Schema object:\n%s", rel, b)
		}
	}
}

// TestScaffoldMainGo_AutoWiresTasksEngine proves the D-164 auto-wire: when
// the example tool is scaffolded with task_support: required (or optional),
// the rendered main.go constructs a real tasks.Engine over an in-memory
// TaskStore and attaches it via server.Options{Tasks: engine}. The plain
// shape (task_support: forbidden) keeps the engine-free main.go.
func TestScaffoldMainGo_AutoWiresTasksEngine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		support          manifest.TaskSupport
		wantAutoWire     bool
		mustContain      []string
		mustNotContain   []string
		mustContainPlain []string // markers that should appear regardless
	}{
		{
			name:    "forbidden — no auto-wire",
			support: manifest.TaskSupportForbidden,
			mustContain: []string{
				`server.New(server.Info{`,
			},
			mustNotContain: []string{
				"tasks.NewEngine",
				"tasks.NewInMemoryStore",
				"Tasks: engine",
			},
		},
		{
			name:         "required — auto-wire on",
			support:      manifest.TaskSupportRequired,
			wantAutoWire: true,
			mustContain: []string{
				"tasks.NewInMemoryStore",
				"tasks.NewEngine",
				"Tasks: engine",
				"engine.StartSweep",
				"defer engine.StopSweep",
				"runtime/tasks",
				"D-164", // the comment block names the decision so a future reader can find it
			},
		},
		{
			name:         "optional — auto-wire on",
			support:      manifest.TaskSupportOptional,
			wantAutoWire: true,
			mustContain: []string{
				"tasks.NewEngine",
				"Tasks: engine",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := renderMainGo(Options{
				Name:                   "auto-wire-server",
				ExampleToolTaskSupport: tt.support,
			})
			for _, m := range tt.mustContain {
				if !strings.Contains(src, m) {
					t.Errorf("rendered main.go missing %q\nfull source:\n%s", m, src)
				}
			}
			for _, m := range tt.mustNotContain {
				if strings.Contains(src, m) {
					t.Errorf("rendered main.go unexpectedly contains %q", m)
				}
			}
		})
	}
}

// TestScaffoldManifest_HonoursTaskSupport proves the rendered manifest's
// example tool carries the requested task_support value verbatim — the
// scaffold writes the explicit form so the manifest is self-documenting,
// not relying on the loader's "omitted == forbidden" normalisation.
func TestScaffoldManifest_HonoursTaskSupport(t *testing.T) {
	t.Parallel()
	for _, want := range []manifest.TaskSupport{
		manifest.TaskSupportForbidden,
		manifest.TaskSupportOptional,
		manifest.TaskSupportRequired,
	} {
		got := renderManifest(Options{
			Name:                   "demo-server",
			ExampleToolTaskSupport: want,
		})
		if !strings.Contains(got, "task_support: "+string(want)) {
			t.Errorf("manifest for support=%q does not contain task_support: %q\n%s",
				want, want, got)
		}
	}
}

// TestGenerate_AutoWireEndToEnd proves Generate's end-to-end auto-wire path:
// when the Options carry ExampleToolTaskSupport=required, the scaffolded
// project ships both a manifest declaring greet as task_support: required
// AND a main.go that constructs the engine.
func TestGenerate_AutoWireEndToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := Generate(Options{
		Name:                   "tasks-demo",
		Dir:                    dir,
		ExampleToolTaskSupport: manifest.TaskSupportRequired,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	yaml, err := os.ReadFile(filepath.Join(res.Dir, "dockyard.app.yaml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(yaml), "task_support: required") {
		t.Errorf("scaffolded manifest missing task_support: required:\n%s", yaml)
	}
	mainGo, err := os.ReadFile(filepath.Join(res.Dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	for _, m := range []string{"tasks.NewEngine", "tasks.NewInMemoryStore", "Tasks: engine"} {
		if !strings.Contains(string(mainGo), m) {
			t.Errorf("scaffolded main.go missing auto-wire marker %q", m)
		}
	}
}

// TestScaffoldContractsMatchModel proves the contract Go source emitted into
// the scaffolded project declares the same GreetInput/GreetOutput fields the
// scaffold's own Go types use for schema generation — so the shipped schema
// genuinely matches the shipped contract source (no drift between the codegen
// model and the emitted source).
func TestScaffoldContractsMatchModel(t *testing.T) {
	t.Parallel()
	for _, field := range []string{
		"Name string `json:\"name\"`",
		"Greeting string `json:\"greeting,omitempty\"`",
		"Message string `json:\"message\"`",
		"Length int `json:\"length\"`",
	} {
		if !strings.Contains(contractsGoSource, field) {
			t.Errorf("emitted contracts source is missing field declaration: %s", field)
		}
	}
}

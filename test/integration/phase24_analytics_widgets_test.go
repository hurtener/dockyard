// This file is the Phase 24 integration test (CLAUDE.md §17). Phase 24's
// deps name Phase 19, 20, and 10a; this test exercises the binding seam
// they touch — the template-discovery seam in internal/scaffold + the
// runtime/server tool-call boundary the materialised handlers run over —
// with REAL components and no mocks at the boundaries.
//
// The test:
//  1. Builds the real `dockyard` binary (it embeds the analytics-widgets
//     template via //go:embed at compile time — proving the embed works
//     end to end, not just in unit-test isolation).
//  2. Materialises the template into a temp directory via the real binary
//     (`dockyard new --template analytics-widgets`).
//  3. Tidies + builds + tests the materialised project with the real Go
//     toolchain (the "builds" and "test suite passes" halves of the
//     acceptance criterion).
//  4. Spins up an in-process MCP server using the same runtime/server +
//     runtime/tool packages the materialised main.go uses, registers the
//     three template handlers, and drives each tool with a real SDK client
//     (the "serves" half).
//  5. Validates each fixture against the contract: the fixtures the
//     inspector switcher will drive map onto the typed input schema, so a
//     passing fixture proves the inspector wiring will hold.
//
// Covers ≥1 failure mode per seam: an unknown template name surfaces the
// typed `ErrUnknownTemplate`, and re-materialising into a non-empty target
// is refused. Runs under -race.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/scaffold"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"

	_ "github.com/hurtener/dockyard/templates/analytics-widgets" // register the builtin
	awcontracts "github.com/hurtener/dockyard/templates/analytics-widgets/pkg/contracts"
	awhandlers "github.com/hurtener/dockyard/templates/analytics-widgets/pkg/handlers"
)

// TestPhase24_TemplateMaterialisesBuildsAndTests drives the entire
// scaffold → build → test cycle on the analytics-widgets template against
// the real dockyard binary. This is the binding acceptance check for the
// phase: a developer running `dockyard new --template analytics-widgets`
// must get a working project out of the box.
func TestPhase24_TemplateMaterialisesBuildsAndTests(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	// Build the real dockyard binary into a temp dir — proves the embed +
	// init() registration are wired end to end.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "dockyard")
	build := exec.CommandContext(context.Background(), //nolint:gosec // test driver: args are constants + a temp path
		"go", "build", "-o", binPath, "./cmd/dockyard")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build dockyard: %v\n%s", err, out)
	}

	// Materialise the template via the real binary into a fresh temp dir.
	parent := t.TempDir()
	mat := exec.CommandContext(context.Background(), //nolint:gosec // test driver: binPath is the test's own freshly built binary
		binPath, "new", "aw-itest",
		"--template", "analytics-widgets",
		"--dir", parent,
		"--dockyard-path", root)
	if out, err := mat.CombinedOutput(); err != nil {
		t.Fatalf("dockyard new --template analytics-widgets: %v\n%s", err, out)
	}
	proj := filepath.Join(parent, "aw-itest")

	// The materialised tree carries the expected shape.
	for _, rel := range []string{
		"dockyard.app.yaml",
		"main.go",
		"internal/contracts/contracts.go",
		"internal/handlers/handlers.go",
		"internal/handlers/handlers_test.go",
		"web/src/App.svelte",
		"web/src/widgets/ChartFrame.svelte",
		"web/src/widgets/Chart.svelte",
		"web/src/widgets/Table.svelte",
		"web/src/widgets/MetricCardWidget.svelte",
		"go.mod",
		"README.md",
		".gitignore",
	} {
		if _, err := os.Stat(filepath.Join(proj, rel)); err != nil {
			t.Errorf("materialised project missing %s: %v", rel, err)
		}
	}

	// The manifest carries the three tools + the one inline-only app.
	manifest, err := os.ReadFile(filepath.Join(proj, "dockyard.app.yaml")) //nolint:gosec // proj is a test temp dir
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	for _, want := range []string{
		"name: create_chart",
		"name: create_table",
		"name: create_metric_card",
		"id: widgets",
		"display_modes: [inline]",
		"bundle: single-file",
	} {
		if !strings.Contains(string(manifest), want) {
			t.Errorf("manifest missing %q", want)
		}
	}

	// Tidy + build + test the materialised project — the "builds + tests
	// pass" acceptance criterion.
	tidy := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidy.Dir = proj
	tidy.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"build", "./..."},
		{"vet", "./..."},
		{"test", "./..."},
	} {
		cmd := exec.CommandContext(context.Background(), "go", args...) //nolint:gosec // test driver: args is a fixed table above
		cmd.Dir = proj
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// Eighteen fixtures total (6 per tool) — the inspector switcher's
	// surface area.
	for _, tool := range []string{"create_chart", "create_table", "create_metric_card"} {
		for _, state := range []string{"happy", "empty", "error", "permission", "slow", "large"} {
			if _, err := os.Stat(filepath.Join(proj, "fixtures", tool, state+".json")); err != nil {
				t.Errorf("fixture %s/%s missing: %v", tool, state, err)
			}
		}
	}
}

// TestPhase24_HandlersServeOverMCP runs the three template handlers
// in-process against a real runtime/server, with a real SDK client driving
// each tool over the in-memory transport. No mocks at the MCP boundary.
//
// The in-process re-statement of the handlers (the awcontracts shim
// package below) imports the template's contract package directly — the
// same types the materialised handlers use — so the structured output
// shape proven here is the same shape the inspector will render.
func TestPhase24_HandlersServeOverMCP(t *testing.T) {
	t.Parallel()
	srv, err := server.New(server.Info{
		Name:    "aw-itest",
		Version: "0.1.0",
	}, &server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := tool.New[awcontracts.CreateChartInput, awcontracts.CreateChartOutput]("create_chart").
		Describe("Render a chart inline.").
		Handler(awhandlers.CreateChart).
		Register(srv); err != nil {
		t.Fatalf("register create_chart: %v", err)
	}
	if err := tool.New[awcontracts.CreateTableInput, awcontracts.CreateTableOutput]("create_table").
		Describe("Render a table inline.").
		Handler(awhandlers.CreateTable).
		Register(srv); err != nil {
		t.Fatalf("register create_table: %v", err)
	}
	if err := tool.New[awcontracts.CreateMetricCardInput, awcontracts.CreateMetricCardOutput]("create_metric_card").
		Describe("Render a metric card inline.").
		Handler(awhandlers.CreateMetricCard).
		Register(srv); err != nil {
		t.Fatalf("register create_metric_card: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT := srv.ServeInMemory(ctx)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "itest", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// tools/list — all three tools advertised.
	listed, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	want := map[string]bool{"create_chart": false, "create_table": false, "create_metric_card": false}
	for _, tl := range listed.Tools {
		if _, ok := want[tl.Name]; ok {
			want[tl.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tools/list missing %s", name)
		}
	}

	// Drive each tool once with a realistic happy-path input and assert
	// the structured payload's `kind` discriminator + `state` are what the
	// App's dispatcher expects.
	cases := []struct {
		tool     string
		args     any
		wantKind string
	}{
		{
			tool: "create_chart",
			args: awcontracts.CreateChartInput{
				Type: awcontracts.ChartType("bar"),
				Data: awcontracts.ChartData{
					Series:     []awcontracts.ChartSeries{{Name: "Revenue", Values: []float64{1, 2, 3}}},
					Categories: []string{"Jan", "Feb", "Mar"},
				},
				Title: "Revenue",
			},
			wantKind: "chart",
		},
		{
			tool: "create_table",
			args: awcontracts.CreateTableInput{
				Columns: []awcontracts.TableColumn{
					{Key: "n", Label: "Name", Type: awcontracts.TableColumnType("string")},
				},
				Rows: []map[string]any{{"n": "Acme"}},
			},
			wantKind: "table",
		},
		{
			tool: "create_metric_card",
			args: awcontracts.CreateMetricCardInput{
				Label: "Health",
				Value: 87,
				Unit:  "/100",
				Delta: &awcontracts.MetricDelta{Value: "+3", Tone: awcontracts.MetricDeltaTone("ok")},
			},
			wantKind: "metric_card",
		},
	}
	for _, tc := range cases {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      tc.tool,
			Arguments: tc.args,
		})
		if err != nil {
			t.Fatalf("%s: tools/call: %v", tc.tool, err)
		}
		if res.IsError {
			t.Fatalf("%s: IsError: %+v", tc.tool, res.Content)
		}
		sc, ok := res.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("%s: structuredContent is %T, want object", tc.tool, res.StructuredContent)
		}
		if sc["kind"] != tc.wantKind {
			t.Errorf("%s: kind = %v, want %q", tc.tool, sc["kind"], tc.wantKind)
		}
		if sc["state"] != "ready" {
			t.Errorf("%s: state = %v, want ready", tc.tool, sc["state"])
		}
	}
}

// TestPhase24_TemplateRegistry_TypedErrors covers the failure modes the
// seam advertises (CLAUDE.md §17 — ≥1 failure mode per seam):
//   - an unknown template surfaces ErrUnknownTemplate (typed).
//   - generating into a non-empty target is refused.
func TestPhase24_TemplateRegistry_TypedErrors(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	_, err := scaffold.GenerateFromTemplate(
		scaffold.Options{Name: "x-unknown", Dir: parent},
		"no-such-template",
	)
	if !errors.Is(err, scaffold.ErrUnknownTemplate) {
		t.Errorf("err = %v, want ErrUnknownTemplate", err)
	}

	// Materialise once into a fresh path — should succeed (analytics-widgets
	// is registered in this test binary via the blank import at the top).
	target := t.TempDir()
	if _, err := scaffold.GenerateFromTemplate(
		scaffold.Options{Name: "ok-target", Dir: target, DockyardReplace: repoRoot(t)},
		"analytics-widgets",
	); err != nil {
		t.Fatalf("first materialise: %v", err)
	}
	// A second materialise into the same path is refused.
	_, err = scaffold.GenerateFromTemplate(
		scaffold.Options{Name: "ok-target", Dir: target, DockyardReplace: repoRoot(t)},
		"analytics-widgets",
	)
	if !errors.Is(err, scaffold.ErrTargetExists) {
		t.Errorf("re-materialise: err = %v, want ErrTargetExists", err)
	}
}

// TestPhase24_FixturesValidAgainstContracts decodes every shipped fixture
// against the typed input contract — proves the inspector's fixture
// switcher (Phase 23) will be able to drive the materialised handlers
// without a schema mismatch.
func TestPhase24_FixturesValidAgainstContracts(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	cases := map[string]func([]byte) error{
		"create_chart": func(b []byte) error {
			var f struct {
				Input awcontracts.CreateChartInput `json:"input"`
			}
			return json.Unmarshal(b, &f)
		},
		"create_table": func(b []byte) error {
			var f struct {
				Input awcontracts.CreateTableInput `json:"input"`
			}
			return json.Unmarshal(b, &f)
		},
		"create_metric_card": func(b []byte) error {
			var f struct {
				Input awcontracts.CreateMetricCardInput `json:"input"`
			}
			return json.Unmarshal(b, &f)
		},
	}
	for tool, decode := range cases {
		for _, state := range []string{"happy", "empty", "error", "permission", "slow", "large"} {
			rel := filepath.Join("templates/analytics-widgets/fixtures", tool, state+".json")
			b, err := os.ReadFile(filepath.Join(root, rel)) //nolint:gosec // test reads bundled fixtures
			if err != nil {
				t.Errorf("read %s: %v", rel, err)
				continue
			}
			if err := decode(b); err != nil {
				t.Errorf("decode %s against contract: %v", rel, err)
			}
		}
	}
}

package approvalflows

import (
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/scaffold"
)

// TestBuiltin_TemplateShape proves the approval-flows EmbeddedTemplate
// declares the wire name + summary the CLI surfaces and the textual-ext
// + path-remap configuration the materialiser uses. Mirrors the
// analytics-widgets precedent — every templates/<name>/ builtin follows
// the same shape (decision D-128's "one new templates/<name>/ + one
// registration" goal).
func TestBuiltin_TemplateShape(t *testing.T) {
	t.Parallel()
	tmpl := builtin()
	if tmpl.Name() != "approval-flows" {
		t.Errorf("Name = %q, want approval-flows", tmpl.Name())
	}
	if !strings.Contains(tmpl.Summary(), "approval") {
		t.Errorf("Summary = %q, want approval-flows summary text", tmpl.Summary())
	}
	files, err := tmpl.Materialise(scaffold.Options{Name: "dual-test"})
	if err != nil {
		t.Fatalf("Materialise: %v", err)
	}
	if !strings.Contains(string(files["main.go"]), "HTTPOptions{ProtocolMode: server.Dual}") {
		t.Fatal("main.go.tmpl does not explicitly enable dual HTTP lifecycle support")
	}
	readme := string(files["README.md"])
	if !strings.Contains(readme, "Optional OAuth protection for HTTP") || !strings.Contains(readme, "examples/oauth-resource-server") {
		t.Fatal("README does not document opt-in OAuth resource-server configuration")
	}

	// TextExts covers the textual file types the template ships.
	wantExts := []string{".tmpl", ".yaml", ".md", ".go", ".ts", ".svelte", ".json", ".html", ".css"}
	have := map[string]bool{}
	for _, e := range tmpl.TextExts {
		have[e] = true
	}
	for _, want := range wantExts {
		if !have[want] {
			t.Errorf("TextExts missing %q", want)
		}
	}

	// PathRemap pushes pkg/ → internal/ — the framework keeps the in-tree
	// path non-internal so the Phase 25 integration test can import it.
	if len(tmpl.PathRemap) == 0 ||
		tmpl.PathRemap[0].From != "pkg/" ||
		tmpl.PathRemap[0].To != "internal/" {
		t.Errorf("PathRemap = %+v, want pkg/ → internal/", tmpl.PathRemap)
	}
}

// TestBuiltin_SubstitutionsFor exercises the per-materialisation
// substitution table — the placeholder forms a developer's options
// resolve to, plus the conditional dockyard-replace block.
func TestBuiltin_SubstitutionsFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts scaffold.Options
		want map[string]string
	}{
		{
			name: "default module path, no replace",
			opts: scaffold.Options{Name: "approver-test"},
			want: map[string]string{
				"__PROJECT_NAME__":           "approver-test",
				"__PROJECT_TITLE__":          "Approver Test",
				"__MODULE_PATH__":            "example.com/approver-test",
				"__DOCKYARD_VERSION__":       "v0.0.0",
				"__DOCKYARD_REPLACE_BLOCK__": "",
				"__DOCKYARD_BRIDGE_SPEC__":   "*",
				"__DOCKYARD_UI_SPEC__":       "*",
				"github.com/hurtener/dockyard/templates/approval-flows/pkg": "example.com/approver-test/internal",
			},
		},
		{
			name: "explicit module path + replace + web path",
			opts: scaffold.Options{
				Name:            "ops",
				ModulePath:      "github.com/acme/ops",
				DockyardReplace: "/some/path",
				DockyardWebPath: "/some/path/web",
			},
			want: map[string]string{
				"__PROJECT_NAME__":         "ops",
				"__PROJECT_TITLE__":        "Ops",
				"__MODULE_PATH__":          "github.com/acme/ops",
				"__DOCKYARD_VERSION__":     "v0.0.0",
				"__DOCKYARD_BRIDGE_SPEC__": "file:/some/path/web/bridge",
				"__DOCKYARD_UI_SPEC__":     "file:/some/path/web/ui",
				"github.com/hurtener/dockyard/templates/approval-flows/pkg": "github.com/acme/ops/internal",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			subs := substitutionsFor(tt.opts)
			got := map[string]string{}
			for _, s := range subs {
				got[s.From] = s.To
			}
			for from, want := range tt.want {
				if got[from] != want {
					t.Errorf("substitution[%q] = %q, want %q", from, got[from], want)
				}
			}
			if tt.opts.DockyardReplace != "" {
				if !strings.Contains(got["__DOCKYARD_REPLACE_BLOCK__"], tt.opts.DockyardReplace) {
					t.Errorf("__DOCKYARD_REPLACE_BLOCK__ = %q, want to contain %q",
						got["__DOCKYARD_REPLACE_BLOCK__"], tt.opts.DockyardReplace)
				}
			}
		})
	}
}

// TestBuiltin_TitleCase covers the title helper.
func TestBuiltin_TitleCase(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"my-approver", "My Approver"},
		{"approval-flows-demo", "Approval Flows Demo"},
		{"single", "Single"},
		{"", ""},
	}
	for _, c := range cases {
		if got := titleCase(c.in); got != c.want {
			t.Errorf("titleCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBuiltin_MaterialiseSampleProject exercises Materialise end to end
// via the EmbeddedTemplate the package registers — proves substitution +
// path remap land the right shape (the smoke + integration tests prove
// the real build / serve cycle).
func TestBuiltin_MaterialiseSampleProject(t *testing.T) {
	t.Parallel()
	files, err := builtin().Materialise(scaffold.Options{Name: "ut-app"})
	if err != nil {
		t.Fatalf("Materialise: %v", err)
	}
	want := []string{
		"dockyard.app.yaml",
		"main.go",
		"internal/contracts/contracts.go",
		"internal/handlers/handlers.go",
		"internal/handlers/handlers_test.go",
		"web/src/App.svelte",
		"go.mod",
		"README.md",
		".gitignore",
		"fixtures/request_approval/happy.json",
		"fixtures/propose_with_edits/happy.json",
	}
	for _, rel := range want {
		if _, ok := files[rel]; !ok {
			t.Errorf("Materialise missing %s (have %d files)", rel, len(files))
		}
	}
	// Substitution landed: the manifest carries the project name; the
	// main.go imports the project's module path; the contracts package
	// is at internal/, not pkg/.
	if !strings.Contains(string(files["dockyard.app.yaml"]), "name: ut-app") {
		t.Error("manifest did not get the project name substituted")
	}
	if !strings.Contains(string(files["main.go"]), "example.com/ut-app/internal/contracts") {
		t.Errorf("main.go did not get the import path substituted:\n%s", files["main.go"])
	}
	// Builtin.go must NOT have been materialised — it is framework glue.
	if _, ok := files["builtin.go"]; ok {
		t.Error("builtin.go leaked into the materialised project")
	}
}

// TestBuiltin_GoModPinsReleaseVersion is the template-path counterpart of
// internal/scaffold.TestGenerate_PinsReleaseVersion. A released CLI scaffolding
// `--template approval-flows` WITHOUT --dockyard-path must pin the real release
// version in go.mod (no replace) so `go mod tidy` resolves the published module
// instead of failing on the `v0.0.0: unknown revision` sharp edge. Regression
// guard for the v1.7.3 fix: the template's go.mod.tmpl used to hardcode
// `v0.0.0`, so every template scaffold was broken flag-free even from a
// published CLI.
func TestBuiltin_GoModPinsReleaseVersion(t *testing.T) {
	t.Parallel()
	files, err := builtin().Materialise(scaffold.Options{Name: "pinned", DockyardVersion: "v1.7.3"})
	if err != nil {
		t.Fatalf("Materialise: %v", err)
	}
	goMod := string(files["go.mod"])
	if !strings.Contains(goMod, "require github.com/hurtener/dockyard v1.7.3") {
		t.Errorf("go.mod did not pin the release version:\n%s", goMod)
	}
	if strings.Contains(goMod, "v0.0.0") {
		t.Errorf("go.mod still carries the v0.0.0 placeholder:\n%s", goMod)
	}
	if strings.Contains(goMod, "replace ") {
		t.Errorf("go.mod has a replace directive without --dockyard-path:\n%s", goMod)
	}
}

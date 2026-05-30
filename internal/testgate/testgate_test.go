package testgate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/runtime/apps"
)

// writeManifest writes a dockyard.app.yaml into dir and returns dir. The
// content is a minimal-but-valid blank-server manifest unless overridden.
func writeManifest(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, manifest.DefaultFilename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

// blankManifest is a structurally valid no-template-server manifest — one
// plain tool, no apps[].
const blankManifest = `name: fixture-server
title: Fixture Server
version: 0.1.0
runtime:
  transports: [stdio]
tools:
  - name: greet
    description: Greet a person by name and return the assembled greeting.
    input: internal/contracts.GreetInput
    output: internal/contracts.GreetOutput
    task_support: forbidden
quality:
  require_fixtures: true
  require_contract_tests: true
  require_spec_compliance: true
`

func TestRun_RejectsEmptyProjectDir(t *testing.T) {
	t.Parallel()
	_, err := Run(Options{ProjectDir: ""})
	if !errors.Is(err, ErrTestGate) {
		t.Fatalf("Run with empty ProjectDir: got err %v, want ErrTestGate", err)
	}
}

func TestRun_RejectsMissingProject(t *testing.T) {
	t.Parallel()
	_, err := Run(Options{ProjectDir: filepath.Join(t.TempDir(), "does-not-exist")})
	if !errors.Is(err, ErrTestGate) {
		t.Fatalf("Run on a missing project: got err %v, want ErrTestGate", err)
	}
}

func TestRun_RejectsUnloadableManifest(t *testing.T) {
	t.Parallel()
	// A manifest missing the required `name` field will not load.
	dir := writeManifest(t, t.TempDir(), "title: No Name\nversion: 0.1.0\n")
	_, err := Run(Options{ProjectDir: dir})
	if !errors.Is(err, ErrTestGate) {
		t.Fatalf("Run on an unloadable manifest: got err %v, want ErrTestGate", err)
	}
}

// TestRun_SkipGoTestOmitsCategory proves Options.SkipGoTest drops exactly the
// go-test category and keeps the rest.
func TestRun_SkipGoTestOmitsCategory(t *testing.T) {
	t.Parallel()
	dir := writeManifest(t, t.TempDir(), blankManifest)

	rep, err := Run(Options{ProjectDir: dir, SkipGoTest: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, res := range rep.Results {
		if res.Category == CategoryGoTest {
			t.Errorf("SkipGoTest set but the go-test category still ran")
		}
	}
	// The other four categories must still be present.
	if got := len(rep.Results); got != len(categoryOrder)-1 {
		t.Errorf("SkipGoTest run has %d categories, want %d", got, len(categoryOrder)-1)
	}
}

// TestRun_ConcurrentInvocationsAreIndependent proves Run holds no shared
// mutable state — a reusable-artifact requirement (CLAUDE.md §5). Run under
// -race.
func TestRun_ConcurrentInvocationsAreIndependent(t *testing.T) {
	t.Parallel()
	dir := writeManifest(t, t.TempDir(), blankManifest)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Run(Options{ProjectDir: dir, SkipGoTest: true}); err != nil {
				t.Errorf("concurrent Run: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestReport_FailedFlipsOnGatingFailure exercises the exit-code seam.
func TestReport_FailedFlipsOnGatingFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		results []Result
		want    bool
	}{
		{
			name:    "all passing",
			results: []Result{{Category: CategoryContract, Passed: true, Gating: true}},
			want:    false,
		},
		{
			name:    "a gating category failed",
			results: []Result{{Category: CategoryContract, Passed: false, Gating: true}},
			want:    true,
		},
		{
			name:    "a non-gating category failed — does not flip",
			results: []Result{{Category: CategoryGolden, Passed: false, Gating: false}},
			want:    false,
		},
		{
			name: "mixed — one gating failure is enough",
			results: []Result{
				{Category: CategoryContract, Passed: true, Gating: true},
				{Category: CategorySpecCompliance, Passed: false, Gating: true},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &Report{Results: tt.results}
			if got := r.Failed(); got != tt.want {
				t.Errorf("Failed() = %v, want %v", got, tt.want)
			}
			if r.Passed() == r.Failed() {
				t.Errorf("Passed() and Failed() are not inverses")
			}
		})
	}
}

func TestResult_StringRendersVerdict(t *testing.T) {
	t.Parallel()
	pass := Result{Category: CategoryContract, Passed: true, Detail: "ok"}
	if !strings.Contains(pass.String(), "[PASS]") {
		t.Errorf("passing Result.String() = %q, want a [PASS] verdict", pass.String())
	}
	fail := Result{Category: CategoryContract, Passed: false, Detail: "drift"}
	if !strings.Contains(fail.String(), "[FAIL]") {
		t.Errorf("failing Result.String() = %q, want a [FAIL] verdict", fail.String())
	}
}

// --- capability category (pure: manifest-only, no module compile) -----------

// TestRunCapability_BlankServerPasses: a no-template server has no apps[], so
// there is no UI to degrade — the category passes trivially.
func TestRunCapability_BlankServerPasses(t *testing.T) {
	t.Parallel()
	m := loadManifestString(t, blankManifest)
	res := runCapability(t.TempDir(), m)
	if !res.Passed {
		t.Errorf("runCapability on a blank server failed: %s", res.Detail)
	}
	if res.Category != CategoryCapability {
		t.Errorf("Category = %q, want %q", res.Category, CategoryCapability)
	}
}

// TestRunCapability_UIAppPassesWithoutSyntheticURL: a UI-bearing App is
// resolved through every registered host profile WITHOUT the synthetic
// placeholder URL (D-165 — supersedes D-145's workaround). A profile that
// declares RequiresServerURL is exempt from the empty-URL derivation; a
// profile that does not require one derives cleanly against an empty URL.
// The category passes for every shipped host profile.
func TestRunCapability_UIAppPassesWithoutSyntheticURL(t *testing.T) {
	t.Parallel()
	const widgetsManifest = `name: widgets-server
title: Widgets Server
version: 0.1.0
runtime:
  transports: [stdio]
  ui:
    framework: svelte
    bundle: single-file
apps:
  - id: widgets
    uri: ui://widgets-server/widgets
    entry: web/src/App.svelte
    display_modes: [inline]
tools:
  - name: render
    description: Render a widget inline in the host.
    input: internal/contracts.RenderInput
    output: internal/contracts.RenderOutput
    ui: widgets
    task_support: forbidden
`
	m := loadManifestString(t, widgetsManifest)
	res := runCapability(t.TempDir(), m)
	if !res.Passed {
		t.Fatalf("runCapability failed for a UI App: %s", res.Detail)
	}
	if strings.Contains(res.Detail, "capability-test.example") {
		t.Errorf("capability-test detail leaks the retired synthetic URL: %s", res.Detail)
	}
}

// signingHostProfile is a test-only host profile that declares
// RequiresServerURL — the signing-host shape the host-profile seam keeps a home
// for after D-176 retired the synthesising Claude profile. The built-in
// registry ships only the generic verbatim profile, so without this the
// capability category's "a profile that requires a server URL is exempt from
// the empty-URL derivation" branch would go unexercised.
type signingHostProfile struct{ id string }

func (s signingHostProfile) ID() string { return s.id }
func (signingHostProfile) DeriveDomain(label, serverURL string) (string, error) {
	if label == "" {
		return "", nil
	}
	if serverURL == "" {
		// A signing profile refuses to derive without a server URL; the
		// capability category must NOT reach here (it exempts the profile via
		// RequiresServerURL).
		return "", errors.New("signing profile requires a server URL")
	}
	return label + ".signed.example", nil
}
func (signingHostProfile) RequiresServerURL() bool { return true }

// TestRunCapability_SigningProfileExemptFromEmptyURL: a registered signing host
// profile (RequiresServerURL == true) is exempt from the empty-URL derivation
// the capability category drives, so the category still passes — exercising the
// exemption branch the retired Claude profile used to cover (D-176). Without the
// exemption the signing profile's DeriveDomain would error on the empty URL and
// fail the gate.
func TestRunCapability_SigningProfileExemptFromEmptyURL(t *testing.T) {
	t.Parallel()
	if err := apps.RegisterHostProfile(signingHostProfile{id: "testgate-signing-exempt"}); err != nil {
		t.Fatalf("RegisterHostProfile: %v", err)
	}
	const widgetsManifest = `name: widgets-server
title: Widgets Server
version: 0.1.0
runtime:
  transports: [stdio]
  ui:
    framework: svelte
    bundle: single-file
apps:
  - id: widgets
    uri: ui://widgets-server/widgets/index.html
    entry: web/src/App.svelte
    display_modes: [inline]
tools:
  - name: render
    description: Render a widget inline in the host.
    input: internal/contracts.RenderInput
    output: internal/contracts.RenderOutput
    ui: widgets
    task_support: forbidden
`
	m := loadManifestString(t, widgetsManifest)
	res := runCapability(t.TempDir(), m)
	if !res.Passed {
		t.Fatalf("runCapability failed with a registered signing profile (exemption not applied): %s", res.Detail)
	}
}

// TestRunCapability_UIToolWithoutOutputFails: a UI-bearing tool with no output
// contract cannot degrade to a model-facing result when Apps is not
// negotiated — a capability regression.
func TestRunCapability_UIToolWithoutOutputFails(t *testing.T) {
	t.Parallel()
	const uiManifest = `name: ui-server
title: UI Server
version: 0.1.0
runtime:
  transports: [stdio]
  ui:
    framework: svelte
    bundle: single-file
apps:
  - id: card
    uri: ui://card/main
    entry: web/src/apps/card.svelte
    display_modes: [inline]
tools:
  - name: show
    description: Show a card for the requested item with a rich UI panel.
    input: internal/contracts.ShowInput
    output: internal/contracts.ShowOutput
    ui: card
    task_support: optional
`
	m := loadManifestString(t, uiManifest)
	// Blank the tool's Output ref so the UI tool has no model-facing contract.
	m.Tools[0].Output = ""
	res := runCapability(t.TempDir(), m)
	if res.Passed {
		t.Fatalf("runCapability passed a UI tool with no output contract")
	}
	if !strings.Contains(res.Detail, "degrade") {
		t.Errorf("detail %q does not explain the degradation regression", res.Detail)
	}
}

// loadManifestString parses a manifest from an in-memory string via a temp
// file — manifest.LoadFile is the only loader, and it enforces the schema.
func loadManifestString(t *testing.T, content string) *manifest.Manifest {
	t.Helper()
	dir := writeManifest(t, t.TempDir(), content)
	m, err := manifest.LoadFile(filepath.Join(dir, manifest.DefaultFilename))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	return m
}

// --- helpers ----------------------------------------------------------------

func TestOriginLabel(t *testing.T) {
	t.Parallel()
	tests := []struct{ uri, want string }{
		{"ui://card/main", "card"},
		{"ui://customer-health", "customer-health"},
		{"ui://a/b/c", "a"},
		{"https://example.com", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := originLabel(tt.uri); got != tt.want {
			t.Errorf("originLabel(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}

func TestContractTypeName(t *testing.T) {
	t.Parallel()
	tool := manifest.Tool{
		Input:  "internal/contracts.GreetInput",
		Output: "internal/contracts.GreetOutput",
	}
	if got := contractTypeName(tool, "input"); got != "GreetInput" {
		t.Errorf("contractTypeName input = %q, want GreetInput", got)
	}
	if got := contractTypeName(tool, "output"); got != "GreetOutput" {
		t.Errorf("contractTypeName output = %q, want GreetOutput", got)
	}
}

func TestIndent(t *testing.T) {
	t.Parallel()
	if got := indent("a\nb"); got != "  a\n  b" {
		t.Errorf("indent = %q, want %q", got, "  a\n  b")
	}
	if got := indent(""); got != "" {
		t.Errorf("indent(\"\") = %q, want empty", got)
	}
}

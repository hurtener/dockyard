package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_InvalidFixtures is the RFC §4.2 acceptance: each invalid manifest
// fails, the failure wraps ErrInvalidManifest, and the message is source-located
// (it names the file, and a line where the YAML position is available). The
// wantSubstr column pins the specific rejection so a regression in a rule
// surfaces.
func TestValidate_InvalidFixtures(t *testing.T) {
	tests := []struct {
		file       string
		wantSubstr string
		wantLine   bool // true when the fault should carry a file:line position
	}{
		{"bad-missing-name.yaml", "name: required", false},
		{"bad-version.yaml", "not a semantic version", true},
		{"bad-transport.yaml", "unknown value \"carrier-pigeon\"", true},
		{"bad-task-support.yaml", "unknown value \"maybe\"", true},
		{"bad-display-mode.yaml", "unknown value \"hologram\"", true},
		{"bad-ui-ref.yaml", "references unknown app id", true},
		{"bad-dup-tool.yaml", "duplicate tool name", true},
		{"bad-ui-uri.yaml", "not a well-formed ui:// resource URI", true},
		{"bad-visibility.yaml", "unknown value \"everyone\"", true},
		{"bad-contract-ref.yaml", "not a Go type reference", true},
		{"bad-no-tools.yaml", "at least one tool is required", true},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			raw := readFixture(t, tc.file)
			_, err := Load(strings.NewReader(raw), tc.file)
			if err == nil {
				t.Fatalf("%s: want error, got nil", tc.file)
			}
			if !errors.Is(err, ErrInvalidManifest) {
				t.Errorf("error does not wrap ErrInvalidManifest: %v", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.wantSubstr) {
				t.Errorf("error %q does not contain %q", msg, tc.wantSubstr)
			}
			if !strings.Contains(msg, tc.file) {
				t.Errorf("error %q is not source-located (no file name)", msg)
			}
			if tc.wantLine && !strings.Contains(msg, tc.file+":") {
				t.Errorf("error %q is not line-located (want %s:N)", msg, tc.file)
			}
		})
	}
}

// TestValidate_MultiError reports every fault in one pass.
func TestValidate_MultiError(t *testing.T) {
	raw := readFixture(t, "bad-multi.yaml")
	_, err := Load(strings.NewReader(raw), "bad-multi.yaml")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var list ErrorList
	if !errors.As(err, &list) {
		t.Fatalf("error is not an ErrorList: %T (%v)", err, err)
	}
	if len(list) < 3 {
		t.Errorf("want >= 3 faults reported at once, got %d: %v", len(list), list)
	}
	for _, e := range list {
		if e.Source != "bad-multi.yaml" {
			t.Errorf("fault not source-located: %+v", e)
		}
	}
}

// TestError_Rendering pins the rendered form of a source-located error — the
// golden assertion that a regression in Error.Error() wording surfaces.
func TestError_Rendering(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "full",
			err:  &Error{Source: "dockyard.app.yaml", Line: 7, Field: "tools[0].input", Msg: "required"},
			want: "dockyard.app.yaml:7: tools[0].input: required",
		},
		{
			name: "no line degrades to file",
			err:  &Error{Source: "dockyard.app.yaml", Field: "name", Msg: "required"},
			want: "dockyard.app.yaml: name: required",
		},
		{
			name: "no field",
			err:  &Error{Source: "dockyard.app.yaml", Line: 3, Msg: "manifest is empty"},
			want: "dockyard.app.yaml:3: manifest is empty",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestValidate_OnHandBuiltManifest exercises Validate() (no YAML positions) and
// confirms it agrees with the loader path on a hand-built struct.
func TestValidate_OnHandBuiltManifest(t *testing.T) {
	ok := &Manifest{
		Name:    "hand-built",
		Title:   "Hand Built",
		Version: "2.3.4",
		Runtime: Runtime{Transports: []Transport{TransportStdio}},
		Tools: []Tool{{
			Name:        "t",
			Description: "A tool.",
			Input:       "pkg.In",
			Output:      "pkg.Out",
		}},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid hand-built manifest rejected: %v", err)
	}

	bad := &Manifest{Name: "Bad Name", Version: "x"}
	err := bad.Validate()
	if err == nil {
		t.Fatal("invalid hand-built manifest accepted")
	}
	if !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("error does not wrap ErrInvalidManifest: %v", err)
	}
	// Validate() has no positions, so faults carry no line.
	var list ErrorList
	if errors.As(err, &list) {
		for _, e := range list {
			if e.Line != 0 {
				t.Errorf("Validate() fault carries a line %d, want 0: %+v", e.Line, e)
			}
		}
	}
}

func TestValidate_RuntimeUIPartial(t *testing.T) {
	m := &Manifest{
		Name:    "ui-partial",
		Title:   "UI Partial",
		Version: "1.0.0",
		Runtime: Runtime{
			Transports: []Transport{TransportStdio},
			UI:         &RuntimeUI{Framework: UIFrameworkSvelte}, // Bundle omitted.
		},
		Tools: []Tool{{Name: "t", Description: "d", Input: "p.In", Output: "p.Out"}},
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "runtime.ui.bundle") {
		t.Errorf("want a runtime.ui.bundle fault, got %v", err)
	}
}

func TestValidate_DuplicateAppID(t *testing.T) {
	m := &Manifest{
		Name: "dup-app", Title: "Dup App", Version: "1.0.0",
		Runtime: Runtime{Transports: []Transport{TransportStdio}},
		Tools:   []Tool{{Name: "t", Description: "d", Input: "p.In", Output: "p.Out"}},
		Apps: []App{
			{ID: "w", URI: "ui://dup-app/a", Entry: "a.svelte", DisplayModes: []DisplayMode{DisplayModeInline}},
			{ID: "w", URI: "ui://dup-app/b", Entry: "b.svelte", DisplayModes: []DisplayMode{DisplayModeInline}},
		},
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate app id") {
		t.Errorf("want a duplicate app id fault, got %v", err)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // name is a test-local fixture filename.
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(raw)
}

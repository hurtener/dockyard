package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/scaffold"
	"github.com/hurtener/dockyard/internal/testgate"
	"github.com/hurtener/dockyard/internal/validate"
)

// This file holds Phase 21.5 edge-case unit tests for the CLI's pure
// report-rendering helpers and error-mapping — surfaces the test-quality audit
// flagged as thin. Each helper is exercised directly (the tests are in-package)
// across its branches, table-driven, with typed-error assertions where an
// error path is involved.

// --- printGenerateResult ---------------------------------------------------

func TestPrintGenerateResult(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		res      generate.Result
		contains []string
	}{
		"no changes": {
			res:      generate.Result{Written: []string{"a.ts", "b.json"}},
			contains: []string{"2 files up to date", "no changes"},
		},
		"some changed": {
			res: generate.Result{
				Written: []string{"a.ts", "b.json", "c.json"},
				Changed: []string{"b.json", "c.json"},
			},
			contains: []string{"3 files written", "2 changed", "changed  b.json", "changed  c.json"},
		},
		"empty result": {
			res:      generate.Result{},
			contains: []string{"0 files up to date"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			printGenerateResult(&buf, tc.res)
			got := buf.String()
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
		})
	}
}

// --- printValidateReport ---------------------------------------------------

func TestPrintValidateReport(t *testing.T) {
	t.Parallel()
	blocker := validate.Diagnostic{Check: validate.CheckManifest, Severity: validate.Blocker, Message: "manifest is invalid"}
	warning := validate.Diagnostic{Check: validate.CheckSchema, Severity: validate.Warning, Message: "schema could be tighter"}

	cases := map[string]struct {
		report   *validate.Report
		contains []string
	}{
		"clean": {
			report:   &validate.Report{},
			contains: []string{"validate: OK — no issues"},
		},
		"warnings only": {
			report:   &validate.Report{Diagnostics: []validate.Diagnostic{warning}},
			contains: []string{"validate: OK — 0 build blockers, 1 warning", "schema could be tighter"},
		},
		"blockers and warnings": {
			report:   &validate.Report{Diagnostics: []validate.Diagnostic{blocker, warning}},
			contains: []string{"validate: FAILED — 1 build blocker", "1 warning", "manifest is invalid"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			printValidateReport(&buf, tc.report)
			got := buf.String()
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
		})
	}
}

// --- printTestReport -------------------------------------------------------

func TestPrintTestReport(t *testing.T) {
	t.Parallel()
	pass := testgate.Result{Category: "go-test", Passed: true, Gating: true, Detail: "all unit tests passed"}
	fail := testgate.Result{Category: "contract", Passed: false, Gating: true, Detail: "generated schema is stale"}

	cases := map[string]struct {
		report   *testgate.Report
		contains []string
	}{
		"all pass": {
			report:   &testgate.Report{Results: []testgate.Result{pass}},
			contains: []string{"test: OK — 1/1 categories passed", "all unit tests passed"},
		},
		"one fails": {
			report:   &testgate.Report{Results: []testgate.Result{pass, fail}},
			contains: []string{"test: FAILED — 1/2 categories passed", "generated schema is stale"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			printTestReport(&buf, tc.report)
			got := buf.String()
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
		})
	}
}

// --- mapScaffoldError ------------------------------------------------------

func TestMapScaffoldError(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in       error
		wantWrap error
		contains string
	}{
		"invalid name": {
			in:       fmt.Errorf("%w: bad chars", scaffold.ErrInvalidName),
			wantWrap: scaffold.ErrInvalidName,
			contains: "bad chars",
		},
		"target exists": {
			in:       fmt.Errorf("%w: ./srv", scaffold.ErrTargetExists),
			wantWrap: scaffold.ErrTargetExists,
			contains: "choose another name, remove the directory, or pass --here",
		},
		"other error": {
			in:       errors.New("disk full"),
			wantWrap: nil,
			contains: "scaffold failed: disk full",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := mapScaffoldError(tc.in)
			if got == nil {
				t.Fatal("mapScaffoldError returned nil")
			}
			if tc.wantWrap != nil && !errors.Is(got, tc.wantWrap) {
				t.Errorf("error should wrap %v, got %v", tc.wantWrap, got)
			}
			if !strings.Contains(got.Error(), tc.contains) {
				t.Errorf("error %q should contain %q", got.Error(), tc.contains)
			}
		})
	}
}

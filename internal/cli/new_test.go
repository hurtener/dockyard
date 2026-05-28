package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// These tests cover the `dockyard new` post-scaffold step (D-166): the
// best-effort `go mod tidy` + `dockyard generate` run, its --no-postgen
// opt-out, the non-fatal failure path, and the next-step rendering. The two
// toolchain-touching steps are package vars (goModTidyFn / generateFn) so the
// success and failure paths are driven without a real toolchain or network.

// withPostScaffoldStubs swaps the post-step seams for the duration of a test,
// restoring them after.
func withPostScaffoldStubs(t *testing.T, tidy func(context.Context, string) error, gen func(string) error) {
	t.Helper()
	origTidy, origGen := goModTidyFn, generateFn
	goModTidyFn, generateFn = tidy, gen
	t.Cleanup(func() { goModTidyFn, generateFn = origTidy, origGen })
}

func TestRunPostScaffold(t *testing.T) {
	cases := map[string]struct {
		tidyErr  error
		genErr   error
		wantOK   bool
		wantOut  []string // on stdout
		wantWarn []string // on stderr
		noGenRun bool     // generate must not be attempted
	}{
		"both succeed": {
			wantOK:  true,
			wantOut: []string{"go mod tidy", "dockyard generate"},
		},
		"tidy fails — generate skipped": {
			tidyErr:  errors.New("dial tcp: lookup proxy.golang.org: no such host\nextra noise"),
			wantOK:   false,
			wantWarn: []string{"go mod tidy did not complete", "no such host", "finish setup"},
			noGenRun: true,
		},
		"generate fails after tidy": {
			genErr:   errors.New("schema generator failed"),
			wantOK:   false,
			wantWarn: []string{"dockyard generate did not complete", "schema generator failed"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			genCalled := false
			withPostScaffoldStubs(t,
				func(context.Context, string) error { return tc.tidyErr },
				func(string) error { genCalled = true; return tc.genErr },
			)
			var out, errOut bytes.Buffer
			got := runPostScaffold(context.Background(), &out, &errOut, "demo")
			if got != tc.wantOK {
				t.Errorf("runPostScaffold ok = %v, want %v", got, tc.wantOK)
			}
			if tc.noGenRun && genCalled {
				t.Error("generate ran despite a tidy failure")
			}
			for _, w := range tc.wantOut {
				if !strings.Contains(out.String(), w) {
					t.Errorf("stdout missing %q\n--- got ---\n%s", w, out.String())
				}
			}
			for _, w := range tc.wantWarn {
				if !strings.Contains(errOut.String(), w) {
					t.Errorf("stderr missing %q\n--- got ---\n%s", w, errOut.String())
				}
			}
			// A failure must never leak a multi-line dump into the warning.
			if !tc.wantOK && strings.Count(errOut.String(), "\n") > 3 {
				t.Errorf("warning is too noisy (multi-line dump leaked):\n%s", errOut.String())
			}
		})
	}
}

func TestPrintNextSteps(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		ready       bool
		wantContain []string
		wantAbsent  []string
	}{
		"ready — no manual setup": {
			ready:       true,
			wantContain: []string{"cd demo", "go test ./...", "go run ."},
			wantAbsent:  []string{"go mod tidy", "dockyard generate"},
		},
		"not ready — manual setup listed": {
			ready:       false,
			wantContain: []string{"cd demo", "go mod tidy", "dockyard generate", "go test ./..."},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			printNextSteps(&buf, "demo", tc.ready)
			got := buf.String()
			for _, w := range tc.wantContain {
				if !strings.Contains(got, w) {
					t.Errorf("output missing %q\n--- got ---\n%s", w, got)
				}
			}
			for _, w := range tc.wantAbsent {
				if strings.Contains(got, w) {
					t.Errorf("output should not contain %q when ready\n--- got ---\n%s", w, got)
				}
			}
		})
	}
}

func TestFirstLine(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"single line":          "single line",
		"first\nsecond\nthird": "first",
		"  padded  ":           "padded",
		"":                     "",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

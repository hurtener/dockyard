package coveragecheck

import (
	"errors"
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	t.Parallel()
	profile := strings.Join([]string{
		"mode: atomic",
		"github.com/hurtener/dockyard/internal/pkga/file.go:10.20,12.3 2 1",
		"github.com/hurtener/dockyard/internal/pkga/file.go:14.2,16.3 3 0",
		"github.com/hurtener/dockyard/internal/pkgb/other.go:1.1,2.2 5 4",
	}, "\n")

	covs, err := ParseProfile(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseProfile: %v", err)
	}
	if len(covs) != 2 {
		t.Fatalf("want 2 packages, got %d", len(covs))
	}
	// Sorted by import path: pkga then pkgb.
	a := covs[0]
	if a.Package != "github.com/hurtener/dockyard/internal/pkga" {
		t.Fatalf("pkga: unexpected import path %q", a.Package)
	}
	if a.Covered != 2 || a.Total != 5 {
		t.Fatalf("pkga: want covered=2 total=5, got covered=%d total=%d", a.Covered, a.Total)
	}
	if got := a.Percent(); got < 39.9 || got > 40.1 {
		t.Fatalf("pkga: want ~40%%, got %.2f", got)
	}
	b := covs[1]
	if b.Covered != 5 || b.Total != 5 {
		t.Fatalf("pkgb: want covered=5 total=5, got covered=%d total=%d", b.Covered, b.Total)
	}
}

func TestParseProfileEmptyAndHeaderOnly(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"empty":       "",
		"header-only": "mode: set\n",
		"blank-lines": "mode: count\n\n\n",
	} {
		covs, err := ParseProfile(strings.NewReader(in))
		if err != nil {
			t.Fatalf("%s: ParseProfile: %v", name, err)
		}
		if len(covs) != 0 {
			t.Fatalf("%s: want 0 packages, got %d", name, len(covs))
		}
	}
}

func TestParseProfileMalformed(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"no-colon":       "mode: set\nnotaprofileline",
		"too-few-fields": "mode: set\npkg/f.go:1.1,2.2 3",
		"bad-stmt":       "mode: set\npkg/f.go:1.1,2.2 x 1",
		"bad-count":      "mode: set\npkg/f.go:1.1,2.2 3 y",
	} {
		if _, err := ParseProfile(strings.NewReader(in)); err == nil {
			t.Fatalf("%s: want parse error, got nil", name)
		}
	}
}

func TestPercentNoStatements(t *testing.T) {
	t.Parallel()
	pc := PackageCoverage{Package: "x", Covered: 0, Total: 0}
	if got := pc.Percent(); got != 100 {
		t.Fatalf("a package with no statements is 100%%, got %.2f", got)
	}
}

const goodConfig = `{
  "packages": {
    "github.com/hurtener/dockyard/internal/pkga": {"min": 80, "band": "new-package"},
    "github.com/hurtener/dockyard/internal/pkgb": {"min": 70, "band": "cli-tooling"}
  },
  "exempt": ["github.com/hurtener/dockyard/cmd/x"]
}`

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(strings.NewReader(goodConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Packages) != 2 {
		t.Fatalf("want 2 packages, got %d", len(cfg.Packages))
	}
	if cfg.Packages["github.com/hurtener/dockyard/internal/pkga"].Min != 80 {
		t.Fatalf("pkga min: want 80")
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"not-json":       `{bad`,
		"unknown-field":  `{"packages":{"p":{"min":80,"band":"cli-tooling"}},"bogus":1}`,
		"no-packages":    `{"packages":{}}`,
		"min-too-high":   `{"packages":{"p":{"min":101,"band":"cli-tooling"}}}`,
		"min-negative":   `{"packages":{"p":{"min":-1,"band":"cli-tooling"}}}`,
		"override-no-rs": `{"packages":{"p":{"min":50,"band":"harness-override"}}}`,
	}
	for name, in := range cases {
		if _, err := LoadConfig(strings.NewReader(in)); err == nil {
			t.Fatalf("%s: want error, got nil", name)
		}
	}
}

func TestLoadConfigOverrideWithReasonOK(t *testing.T) {
	t.Parallel()
	in := `{"packages":{"p":{"min":50,"band":"subprocess-override","reason":"orchestrates subprocesses"}}}`
	if _, err := LoadConfig(strings.NewReader(in)); err != nil {
		t.Fatalf("override with reason should load: %v", err)
	}
}

func TestCheckAllPass(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(strings.NewReader(goodConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// pkga: 9 of 10 stmts covered (90%); pkgb: 8 of 10 (80%) — each block
	// line carries its own statement count, the profile format Go writes.
	profile := strings.Join([]string{
		"mode: atomic",
		"github.com/hurtener/dockyard/internal/pkga/f.go:1.1,2.2 9 1",
		"github.com/hurtener/dockyard/internal/pkga/f.go:3.1,4.2 1 0",
		"github.com/hurtener/dockyard/internal/pkgb/f.go:1.1,2.2 8 3",
		"github.com/hurtener/dockyard/internal/pkgb/f.go:3.1,4.2 2 0",
	}, "\n")
	rep, err := Check(cfg, strings.NewReader(profile))
	if err != nil {
		t.Fatalf("Check: unexpected error %v", err)
	}
	if len(rep.Failures) != 0 {
		t.Fatalf("want no failures, got %d", len(rep.Failures))
	}
}

func TestCheckShortfallFails(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(strings.NewReader(goodConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// pkga measured at 50% against an 80% band — must fail; pkgb at 90% passes.
	profile := strings.Join([]string{
		"mode: atomic",
		"github.com/hurtener/dockyard/internal/pkga/f.go:1.1,2.2 5 1",
		"github.com/hurtener/dockyard/internal/pkga/f.go:3.1,4.2 5 0",
		"github.com/hurtener/dockyard/internal/pkgb/f.go:1.1,2.2 9 2",
		"github.com/hurtener/dockyard/internal/pkgb/f.go:3.1,4.2 1 0",
	}, "\n")
	rep, err := Check(cfg, strings.NewReader(profile))
	if !errors.Is(err, ErrShortfall) {
		t.Fatalf("want ErrShortfall, got %v", err)
	}
	if len(rep.Failures) != 1 || rep.Failures[0].Package != "github.com/hurtener/dockyard/internal/pkga" {
		t.Fatalf("want pkga as the single failure, got %+v", rep.Failures)
	}
}

func TestCheckUnconfiguredPackageFails(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(strings.NewReader(goodConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	profile := strings.Join([]string{
		"mode: atomic",
		"github.com/hurtener/dockyard/internal/pkga/f.go:1.1,2.2 10 10",
		"github.com/hurtener/dockyard/internal/pkgb/f.go:1.1,2.2 10 10",
		"github.com/hurtener/dockyard/internal/surprise/f.go:1.1,2.2 10 10",
	}, "\n")
	_, err = Check(cfg, strings.NewReader(profile))
	if !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("want ErrUnconfigured for an ungated package, got %v", err)
	}
}

func TestCheckExemptPackageIgnored(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(strings.NewReader(goodConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// cmd/x is exempt — even at 0% it is not a failure or an unconfigured fault.
	profile := strings.Join([]string{
		"mode: atomic",
		"github.com/hurtener/dockyard/internal/pkga/f.go:1.1,2.2 10 10",
		"github.com/hurtener/dockyard/internal/pkgb/f.go:1.1,2.2 10 10",
		"github.com/hurtener/dockyard/cmd/x/main.go:1.1,2.2 10 0",
	}, "\n")
	if _, err := Check(cfg, strings.NewReader(profile)); err != nil {
		t.Fatalf("exempt package must not fail the gate: %v", err)
	}
}

func TestCheckMissingMeasurementPasses(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(strings.NewReader(goodConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// Only pkga is in the profile; pkgb is configured but unmeasured.
	profile := "mode: atomic\ngithub.com/hurtener/dockyard/internal/pkga/f.go:1.1,2.2 10 10\n"
	rep, err := Check(cfg, strings.NewReader(profile))
	if err != nil {
		t.Fatalf("a configured-but-unmeasured package must not fail the gate: %v", err)
	}
	for _, r := range rep.Results {
		if r.Package == "github.com/hurtener/dockyard/internal/pkgb" && r.Coverage != 100 {
			t.Fatalf("unmeasured package should report 100%%, got %.1f", r.Coverage)
		}
	}
}

func TestWriteReport(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(strings.NewReader(goodConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	profile := strings.Join([]string{
		"mode: atomic",
		"github.com/hurtener/dockyard/internal/pkga/f.go:1.1,2.2 4 1",
		"github.com/hurtener/dockyard/internal/pkga/f.go:3.1,4.2 6 0",
		"github.com/hurtener/dockyard/internal/pkgb/f.go:1.1,2.2 9 1",
		"github.com/hurtener/dockyard/internal/pkgb/f.go:3.1,4.2 1 0",
	}, "\n")
	rep, _ := Check(cfg, strings.NewReader(profile))
	var sb strings.Builder
	WriteReport(&sb, rep)
	out := sb.String()
	if !strings.Contains(out, "FAIL") || !strings.Contains(out, "below threshold") {
		t.Fatalf("report should flag the pkga shortfall, got:\n%s", out)
	}
	if !strings.Contains(out, "pkgb") {
		t.Fatalf("report should list every package, got:\n%s", out)
	}
}

package coveragecheck

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// ErrShortfall is returned (wrapped) by [Check] when one or more packages fall
// below their required coverage threshold.
var ErrShortfall = errors.New("coverage below threshold")

// ErrUnconfigured is returned (wrapped) by [Check] when the profile names a
// package that has no entry in the threshold config and is not exempt — a new
// package must be added to coverage.json deliberately, never silently ungated.
var ErrUnconfigured = errors.New("package has no coverage threshold configured")

// Band is one AGENTS.md §11 coverage band. The band a package belongs to is
// recorded in the config so a report can show which bar a number is held to.
type Band string

// The three AGENTS.md §11 bands plus the harness carve-out.
const (
	// BandNewPackage is the 80% band for a new package.
	BandNewPackage Band = "new-package"
	// BandConformance is the 85% band for the Store drivers and the other
	// conformance-tested subsystems.
	BandConformance Band = "conformance"
	// BandCLI is the 70% band for CLI / tooling packages.
	BandCLI Band = "cli-tooling"
	// BandHarness marks a conformance *harness* package — a test-helper
	// package whose statements are exercised only when a driver runs the
	// suite, so its self-coverage sits below every product band by
	// construction. Its threshold is a documented override, not a band.
	BandHarness Band = "harness-override"
	// BandSubprocess marks a package that orchestrates subprocesses with
	// branches a hermetic suite cannot reach (the Phase 20 buildpkg/runpkg/
	// installpkg finding); its threshold is a documented override.
	BandSubprocess Band = "subprocess-override"
)

// PackageThreshold is one package's required coverage and the rationale for
// the number.
type PackageThreshold struct {
	// Min is the minimum statement coverage percentage the package must hold.
	Min float64 `json:"min"`
	// Band records which AGENTS.md band (or override class) Min derives from.
	Band Band `json:"band"`
	// Reason documents an override — required when Band is an override class,
	// optional otherwise. It travels in the config so the justification is
	// reviewable (CLAUDE.md §11: never a silent lowering of a band).
	Reason string `json:"reason,omitempty"`
}

// Config is the parsed coverage.json: the per-package threshold map plus the
// list of import-path prefixes exempt from the gate entirely (a package with
// no statements to cover — a `main`, a pure-declaration package).
type Config struct {
	// Comment is a free-text header documenting the config; it has no effect
	// on the gate. Present so coverage.json can carry its own rationale (JSON
	// has no comment syntax).
	Comment string `json:"_comment,omitempty"`
	// Packages maps a full Go import path to its threshold.
	Packages map[string]PackageThreshold `json:"packages"`
	// Exempt lists import paths excused from the gate — a package that
	// legitimately has no coverable statements (an entrypoint shim). An exempt
	// package missing from the profile is not an error.
	Exempt []string `json:"exempt"`
}

// LoadConfig parses a Config from JSON.
func LoadConfig(r io.Reader) (*Config, error) {
	var c Config
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("coveragecheck: parse config: %w", err)
	}
	if len(c.Packages) == 0 {
		return nil, errors.New("coveragecheck: config names no packages")
	}
	for pkg, t := range c.Packages {
		if t.Min < 0 || t.Min > 100 {
			return nil, fmt.Errorf("coveragecheck: %s: min %.1f out of range 0..100", pkg, t.Min)
		}
		if (t.Band == BandHarness || t.Band == BandSubprocess) && strings.TrimSpace(t.Reason) == "" {
			return nil, fmt.Errorf("coveragecheck: %s: %q band requires a documented reason", pkg, t.Band)
		}
	}
	return &c, nil
}

// PackageCoverage is the computed statement coverage of one package.
type PackageCoverage struct {
	// Package is the full Go import path.
	Package string
	// Covered is the number of covered statements.
	Covered int64
	// Total is the total number of statements.
	Total int64
}

// Percent is the package's statement-coverage percentage. A package with no
// statements is reported as 100% — there is nothing to leave uncovered.
func (p PackageCoverage) Percent() float64 {
	if p.Total == 0 {
		return 100
	}
	return float64(p.Covered) / float64(p.Total) * 100
}

// ParseProfile parses a Go coverage profile (the format `go test
// -coverprofile` writes) and aggregates per-package statement counts.
//
// A profile line is `import/path/file.go:startLine.col,endLine.col numStmt
// count`. The package is the import path with the trailing `/file.go` stripped.
// The first line is the `mode:` header and is skipped.
func ParseProfile(r io.Reader) ([]PackageCoverage, error) {
	agg := map[string]*PackageCoverage{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.HasPrefix(line, "mode:") {
				continue
			}
		}
		stmts, count, file, err := parseProfileLine(line)
		if err != nil {
			return nil, err
		}
		pkg := packageOf(file)
		pc := agg[pkg]
		if pc == nil {
			pc = &PackageCoverage{Package: pkg}
			agg[pkg] = pc
		}
		pc.Total += stmts
		if count > 0 {
			pc.Covered += stmts
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("coveragecheck: read profile: %w", err)
	}
	out := make([]PackageCoverage, 0, len(agg))
	for _, pc := range agg {
		out = append(out, *pc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out, nil
}

// parseProfileLine extracts the statement count and hit count from one profile
// block line and returns the file part (everything before the first colon).
func parseProfileLine(line string) (stmts, count int64, file string, err error) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return 0, 0, "", fmt.Errorf("coveragecheck: malformed profile line %q", line)
	}
	file = line[:colon]
	fields := strings.Fields(line[colon+1:])
	if len(fields) != 3 {
		return 0, 0, "", fmt.Errorf("coveragecheck: malformed profile line %q", line)
	}
	stmts, err = strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("coveragecheck: bad stmt count in %q: %w", line, err)
	}
	count, err = strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("coveragecheck: bad hit count in %q: %w", line, err)
	}
	return stmts, count, file, nil
}

// packageOf returns the import path of a profile file entry by stripping the
// trailing `/filename.go`.
func packageOf(file string) string {
	if slash := strings.LastIndexByte(file, '/'); slash >= 0 {
		return file[:slash]
	}
	return file
}

// Result is one package's verdict.
type Result struct {
	// Package is the import path.
	Package string
	// Coverage is the measured statement-coverage percentage.
	Coverage float64
	// Required is the threshold the package was held to.
	Required float64
	// Band is the band (or override class) the threshold derives from.
	Band Band
	// Reason carries an override's documented justification, if any.
	Reason string
	// Exempt is true when the package is gate-exempt (no coverable statements).
	Exempt bool
	// Pass is true when the package met its threshold or is exempt.
	Pass bool
}

// Report is the outcome of a full [Check] run.
type Report struct {
	// Results is every checked package, sorted by import path.
	Results []Result
	// Failures is the subset of Results that fell short of their threshold.
	Failures []Result
}

// Check parses profile against cfg and returns a [Report]. It returns a
// wrapped [ErrShortfall] when any non-exempt package is below its threshold,
// and a wrapped [ErrUnconfigured] when the profile names a package with no
// config entry and no exemption — a new package must be gated deliberately.
//
// A package listed in cfg but absent from the profile (it shipped no test
// binary, or it has no statements) is reported as a 100% pass: the gate never
// fails for a missing measurement, only for a measured shortfall.
func Check(cfg *Config, profile io.Reader) (*Report, error) {
	covs, err := ParseProfile(profile)
	if err != nil {
		return nil, err
	}
	measured := make(map[string]PackageCoverage, len(covs))
	for _, c := range covs {
		measured[c.Package] = c
	}

	rep := &Report{}
	seen := map[string]bool{}

	for pkg, thr := range cfg.Packages {
		seen[pkg] = true
		res := Result{Package: pkg, Required: thr.Min, Band: thr.Band, Reason: thr.Reason}
		if cov, ok := measured[pkg]; ok {
			res.Coverage = cov.Percent()
		} else {
			res.Coverage = 100 // no measurement ⇒ nothing to fail on.
		}
		res.Pass = res.Coverage+1e-9 >= res.Required
		rep.Results = append(rep.Results, res)
		if !res.Pass {
			rep.Failures = append(rep.Failures, res)
		}
	}

	// A measured package neither configured nor exempt is a config gap.
	var unconfigured []string
	for _, c := range covs {
		if seen[c.Package] || isExempt(cfg, c.Package) {
			continue
		}
		unconfigured = append(unconfigured, c.Package)
	}

	sort.Slice(rep.Results, func(i, j int) bool {
		return rep.Results[i].Package < rep.Results[j].Package
	})
	sort.Slice(rep.Failures, func(i, j int) bool {
		return rep.Failures[i].Package < rep.Failures[j].Package
	})

	if len(unconfigured) > 0 {
		sort.Strings(unconfigured)
		return rep, fmt.Errorf("%w: %s", ErrUnconfigured, strings.Join(unconfigured, ", "))
	}
	if len(rep.Failures) > 0 {
		return rep, fmt.Errorf("%w: %d package(s)", ErrShortfall, len(rep.Failures))
	}
	return rep, nil
}

// isExempt reports whether pkg matches an exemption entry exactly.
func isExempt(cfg *Config, pkg string) bool {
	for _, e := range cfg.Exempt {
		if e == pkg {
			return true
		}
	}
	return false
}

// WriteReport renders rep as an aligned, human-readable table to w. A write
// error to the report stream is not actionable, so it is intentionally not
// surfaced (the same convention the CLI print helpers follow).
func WriteReport(w io.Writer, rep *Report) {
	fmt.Fprintf(w, "%-52s %8s %8s  %s\n", "PACKAGE", "COVER", "MIN", "BAND") //nolint:errcheck // report stream write
	for _, r := range rep.Results {
		mark := "ok  "
		if !r.Pass {
			mark = "FAIL"
		}
		fmt.Fprintf(w, "%s %-52s %7.1f%% %7.1f%%  %s\n", //nolint:errcheck // report stream write
			mark, r.Package, r.Coverage, r.Required, r.Band)
	}
	if len(rep.Failures) > 0 {
		fmt.Fprintf(w, "\n%d package(s) below threshold:\n", len(rep.Failures)) //nolint:errcheck // report stream write
		for _, f := range rep.Failures {
			fmt.Fprintf(w, "  %s: %.1f%% < %.1f%% (%s)\n", //nolint:errcheck // report stream write
				f.Package, f.Coverage, f.Required, f.Band)
		}
	}
}

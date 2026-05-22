// Command coveragecheck is the mechanical coverage gate's entry point: it
// reads a Go coverage profile and a threshold config, compares each package's
// statement coverage against its required band, prints a report, and exits
// non-zero on a shortfall (or on an unconfigured package). `make coverage`
// invokes it; CI's `go` job runs `make coverage` so a coverage regression
// fails the build (Phase 21.5; CLAUDE.md §11).
//
// Usage:
//
//	coveragecheck -profile coverage.out [-config internal/coveragecheck/coverage.json]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hurtener/dockyard/internal/coveragecheck"
)

func main() {
	profilePath := flag.String("profile", "coverage.out", "path to the `go test -coverprofile` output")
	configPath := flag.String("config", "internal/coveragecheck/coverage.json", "path to the threshold config")
	flag.Parse()

	if err := run(*profilePath, *configPath); err != nil {
		fmt.Fprintln(os.Stderr, "coveragecheck:", err)
		os.Exit(1)
	}
}

func run(profilePath, configPath string) error {
	cf, err := os.Open(configPath) //nolint:gosec // config path is a build-tool argument, by design.
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = cf.Close() }()
	cfg, err := coveragecheck.LoadConfig(cf)
	if err != nil {
		return err
	}

	pf, err := os.Open(profilePath) //nolint:gosec // profile path is a build-tool argument, by design.
	if err != nil {
		return fmt.Errorf("open profile: %w", err)
	}
	defer func() { _ = pf.Close() }()

	rep, checkErr := coveragecheck.Check(cfg, pf)
	if rep != nil {
		coveragecheck.WriteReport(os.Stdout, rep)
	}
	if checkErr != nil {
		return checkErr
	}
	fmt.Printf("\ncoverage gate: OK — %d package(s) meet their band\n", len(rep.Results))
	return nil
}

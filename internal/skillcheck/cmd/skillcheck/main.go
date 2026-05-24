// Command skillcheck validates a tree of Dockyard agent skills against the
// SKILL.md format (agentskills.io specification). It is the small CLI the
// drift-audit script and the phase-29 smoke check invoke via `go run`.
//
// Usage:
//
//	skillcheck <skills-dir> [<skills-dir> ...]
//
// Exits 0 when every SKILL.md found under each given directory parses
// cleanly and conforms to the spec; exits 1 with a diagnostic line per
// violation otherwise. Designed to be the simplest possible thing the
// drift hook can wrap: zero flags, deterministic exit code, one issue per
// line on stderr.
package main

import (
	"fmt"
	"os"

	"github.com/hurtener/dockyard/internal/skillcheck"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: skillcheck <skills-dir> [<skills-dir> ...]")
		os.Exit(2)
	}
	exitCode := 0
	totalSkills := 0
	totalIssues := 0
	for _, dir := range os.Args[1:] {
		report, err := skillcheck.Validate(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skillcheck: %v\n", err)
			exitCode = 1
			continue
		}
		totalSkills += len(report.Skills)
		totalIssues += len(report.Issues)
		for _, issue := range report.Issues {
			fmt.Fprintln(os.Stderr, issue)
		}
		if !report.Ok() {
			exitCode = 1
		}
	}
	if exitCode == 0 {
		fmt.Fprintf(os.Stdout, "skillcheck: %d skill(s), 0 issue(s)\n", totalSkills) //nolint:errcheck // stdout write
	} else {
		fmt.Fprintf(os.Stderr, "skillcheck: %d skill(s), %d issue(s)\n", totalSkills, totalIssues) //nolint:errcheck // stderr write
	}
	os.Exit(exitCode)
}

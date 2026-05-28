package changelogx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This file implements the Conventional-Commits changelog supplement
// (D-167): the auto-generated, commit-derived PR list the release pipeline
// appends BELOW the hand-authored CHANGELOG section in a GitHub Release body.
// The hand-authored prose stays the canonical narrative (D-154); the
// supplement is the "what landed in detail" companion.
//
// Like the extractor (D-157), the classifier is pure, stdlib-only, and
// golden-tested — a future authoring/format change is a unit-test failure in
// PR, never a release-time failure at tag push. The git-log invocation lives
// in the cmd/changelogx driver; this package only parses + renders.

// Commit is one parsed Conventional-Commit subject line.
type Commit struct {
	// Type is the conventional-commit type token: feat, fix, perf, refactor,
	// docs, chore, … (empty if the subject is not a conventional commit).
	Type string
	// Scope is the optional parenthesised scope: feat(apps) → "apps".
	Scope string
	// Subject is the human description after the "type(scope): " prefix (or
	// the whole subject when there is no recognised prefix).
	Subject string
	// PR is the pull-request number parsed from a trailing "(#123)", or 0.
	PR int
}

// conventionalRE matches a Conventional-Commit subject:
//
//	feat: add the widget
//	fix(codegen): handle empty structs
//	feat(apps)!: breaking change
//
// Group 1 = type, group 2 = optional scope, group 3 = optional "!" breaking
// marker, group 4 = description.
var conventionalRE = regexp.MustCompile(`^([a-zA-Z]+)(?:\(([^)]*)\))?(!)?:\s*(.*)$`)

// trailingPRRE matches a trailing "(#123)" PR reference on a subject line —
// the shape a squash-merge subject carries.
var trailingPRRE = regexp.MustCompile(`\s*\(#(\d+)\)\s*$`)

// ParseCommit parses one git commit subject line into a Commit. A subject
// that is not a recognised conventional commit yields a Commit with an empty
// Type and the full (PR-stripped) text in Subject.
func ParseCommit(subject string) Commit {
	subject = strings.TrimSpace(subject)
	var c Commit
	if m := trailingPRRE.FindStringSubmatch(subject); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			c.PR = n
		}
		subject = strings.TrimSpace(trailingPRRE.ReplaceAllString(subject, ""))
	}
	if m := conventionalRE.FindStringSubmatch(subject); m != nil {
		c.Type = strings.ToLower(m[1])
		c.Scope = m[2]
		c.Subject = strings.TrimSpace(m[4])
		return c
	}
	c.Subject = subject
	return c
}

// changelogGroup is a Keep-a-Changelog category the supplement renders under.
type changelogGroup int

const (
	groupNone changelogGroup = iota
	groupAdded
	groupChanged
	groupFixed
)

// classify maps a commit type to its changelog group. Per D-167 (signal-only):
// feat→Added, fix→Fixed; the noise types (docs/chore/test/ci/build/style) are
// dropped entirely; everything else (perf, refactor, an unknown prefix, or a
// non-conventional subject) folds into Changed as a safe catch-all.
func classify(commitType string) changelogGroup {
	switch commitType {
	case "feat":
		return groupAdded
	case "fix":
		return groupFixed
	case "docs", "chore", "test", "ci", "build", "style":
		return groupNone
	default:
		return groupChanged
	}
}

// Supplement renders the Conventional-Commits supplement block for a set of
// commits, grouped into Keep-a-Changelog categories. The output is a
// self-contained block that the release pipeline appends below the
// hand-authored CHANGELOG section — it opens with a horizontal-rule separator
// and a "Commits" heading, then lists each kept commit under a bold category
// label (a bold label, not a "### Added" heading, so it never collides with
// the narrative's own "### Added" headings).
//
// Commits whose type is dropped (the noise types) are omitted. When no commit
// survives the filter, Supplement returns the empty string — the caller
// appends nothing.
//
// Supplement is pure and deterministic: identical input yields byte-identical
// output, and it preserves input order within each group (so a newest-first
// git-log order is preserved).
func Supplement(commits []Commit) string {
	var added, changed, fixed []Commit
	for _, c := range commits {
		switch classify(c.Type) {
		case groupAdded:
			added = append(added, c)
		case groupChanged:
			changed = append(changed, c)
		case groupFixed:
			fixed = append(fixed, c)
		case groupNone:
			// dropped — a noise-type commit
		}
	}
	if len(added) == 0 && len(changed) == 0 && len(fixed) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("---\n\n### Commits\n")
	renderGroup(&b, "Added", added)
	renderGroup(&b, "Changed", changed)
	renderGroup(&b, "Fixed", fixed)
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// renderGroup writes one bold-labelled category block, or nothing when the
// group is empty.
func renderGroup(b *strings.Builder, label string, commits []Commit) {
	if len(commits) == 0 {
		return
	}
	fmt.Fprintf(b, "\n**%s**\n\n", label)
	for _, c := range commits {
		b.WriteString("- ")
		if c.Scope != "" {
			fmt.Fprintf(b, "**%s:** ", c.Scope)
		}
		b.WriteString(c.Subject)
		if c.PR != 0 {
			// A GitHub Release body auto-links "#123" to the PR.
			fmt.Fprintf(b, " (#%d)", c.PR)
		}
		b.WriteString("\n")
	}
}

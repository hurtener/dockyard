package changelogx

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrSectionNotFound is the sentinel returned by ExtractSection when the
// requested version is not present in the CHANGELOG. Callers branch with
// errors.Is(err, ErrSectionNotFound) — the release workflow treats it as a
// fatal config error (a release without a matching CHANGELOG section is
// almost always a missed entry, not a tag-vs-changelog mismatch worth
// silencing).
var ErrSectionNotFound = errors.New("dockyard/internal/changelogx: section not found")

// ErrMalformed is the sentinel returned when the CHANGELOG cannot be
// structurally parsed at all (no H2 headings, or the parser sees an
// unrecoverable shape). It is distinct from ErrSectionNotFound so a caller
// can tell "the file is broken" from "this version isn't published yet".
var ErrMalformed = errors.New("dockyard/internal/changelogx: malformed CHANGELOG")

// versionHeadingRE matches a Keep-a-Changelog H2 release heading:
//
//	## [1.0.0] - 2026-05-25
//	## [v1.0.0] - 2026-05-25
//	## [Unreleased]
//
// The version token between the brackets is captured into group 1. The
// regex is intentionally permissive on the trailing form so a "no date yet"
// `## [Unreleased]` entry parses (group 2 is empty for it). The leading `##`
// anchor distinguishes a release heading from a deeper section.
var versionHeadingRE = regexp.MustCompile(`^##\s+\[([^\]]+)\](?:\s+-\s+(.*))?\s*$`)

// h2RE matches any H2 heading — used to detect the next section's start.
var h2RE = regexp.MustCompile(`^##\s+`)

// ExtractSection extracts the section body for the named version from a
// CHANGELOG document. The returned bytes are the lines between the
// version's H2 heading and the next H2 (or EOF), with surrounding blank
// lines trimmed — the body a GitHub Release renders cleanly.
//
// version matches the bracketed token verbatim; both "1.0.0" and "v1.0.0"
// are accepted on input (the function tries both forms transparently) so a
// caller can pass either the git-tag shape or the bare semver. Matching is
// case-sensitive otherwise — "Unreleased" is the canonical form.
//
// The function does not include the H2 heading itself in the returned body
// (a GitHub Release UI shows the version in its own header; repeating it in
// the body adds noise). The reference-link footer block at the bottom of a
// Keep-a-Changelog file is also excluded — those URL declarations are
// document-scoped, not section-scoped.
func ExtractSection(content []byte, version string) ([]byte, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("%w: empty CHANGELOG", ErrMalformed)
	}
	if version == "" {
		return nil, fmt.Errorf("%w: empty version requested", ErrSectionNotFound)
	}
	// Both "1.0.0" and "v1.0.0" are accepted; canonicalise the trial set
	// so we match against either form in the file.
	trial := []string{version}
	switch {
	case strings.HasPrefix(version, "v"):
		trial = append(trial, strings.TrimPrefix(version, "v"))
	default:
		trial = append(trial, "v"+version)
	}

	scanner := bufio.NewScanner(bytes.NewReader(content))
	// Allow long lines — a one-line note can plausibly exceed bufio's
	// 64KiB default token size if it carries a long URL. 1 MiB is room
	// to spare for any realistic CHANGELOG.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		inSection bool
		sawAnyH2  bool
		buf       bytes.Buffer
	)

	for scanner.Scan() {
		line := scanner.Text()
		if m := versionHeadingRE.FindStringSubmatch(line); m != nil {
			sawAnyH2 = true
			if inSection {
				// Reached the next H2 — section done.
				break
			}
			for _, t := range trial {
				if m[1] == t {
					inSection = true
					break
				}
			}
			continue
		}
		// A non-version H2 also bounds the section (defensive: a
		// CHANGELOG with auxiliary H2s like "## Notes" still works).
		if inSection && h2RE.MatchString(line) {
			sawAnyH2 = true
			break
		}
		if inSection {
			// Skip the reference-link footer block: a line shaped
			// like "[1.0.0]: https://…" at the bottom of the file
			// is a document-level declaration, not section content.
			// It only ever appears after every section, so the
			// effective bound is "first ref-link line stops us".
			if isRefLinkLine(line) {
				break
			}
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: scan CHANGELOG: %w", ErrMalformed, err)
	}
	if !sawAnyH2 {
		return nil, fmt.Errorf("%w: no H2 release headings found", ErrMalformed)
	}
	if !inSection {
		return nil, fmt.Errorf("%w: version %q", ErrSectionNotFound, version)
	}
	return trimSurroundingBlankLines(buf.Bytes()), nil
}

// refLinkRE matches a Keep-a-Changelog reference-link footer line, e.g.
// "[1.0.0]: https://github.com/hurtener/dockyard/releases/tag/v1.0.0".
var refLinkRE = regexp.MustCompile(`^\[[^\]]+\]:\s+\S+`)

// isRefLinkLine reports whether a line is a reference-link footer
// declaration — the markdown shape "[label]: url".
func isRefLinkLine(line string) bool {
	return refLinkRE.MatchString(line)
}

// trimSurroundingBlankLines strips leading and trailing blank lines from a
// byte buffer without otherwise altering content. The result is the
// minimum-noise form a GitHub Release body should carry.
func trimSurroundingBlankLines(b []byte) []byte {
	// Drop leading whitespace-only lines.
	for {
		idx := bytes.IndexByte(b, '\n')
		if idx < 0 {
			break
		}
		if strings.TrimSpace(string(b[:idx])) != "" {
			break
		}
		b = b[idx+1:]
	}
	// Drop trailing whitespace-only lines.
	for len(b) > 0 {
		// Find the last newline that precedes the buffer's end.
		// If the last character is not a newline, append one so the
		// loop's invariant — strip lines, not bytes — is preserved.
		if b[len(b)-1] != '\n' {
			b = append(b, '\n')
			continue
		}
		// Find the previous newline (or start of buffer); the line
		// between it and the trailing newline is what we test.
		prev := bytes.LastIndexByte(b[:len(b)-1], '\n')
		line := b[prev+1 : len(b)-1]
		if strings.TrimSpace(string(line)) != "" {
			break
		}
		b = b[:prev+1]
	}
	return b
}

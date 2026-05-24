package skillcheck

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Defaults from the agentskills.io specification (see doc.go for the spec
// section references). Limits are bytes, not runes — the spec uses
// "characters" colloquially; for an ASCII-only constraint set this is the
// same number.
const (
	maxNameLen          = 64
	maxDescriptionLen   = 1024
	maxCompatibilityLen = 500
)

// nameRe is the spec's `name` constraint, expressed as the union of allowed
// characters (lowercase a-z, 0-9, hyphen). The leading/trailing-hyphen and
// consecutive-hyphen checks are applied separately so the diagnostics carry
// the precise violation.
var nameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// Skill is one parsed SKILL.md file.
type Skill struct {
	// Dir is the absolute path of the skill directory (the one whose name
	// must equal the frontmatter `name`).
	Dir string
	// Path is the absolute path to the SKILL.md file itself.
	Path string
	// Slug is the directory's basename, the spec's required match against
	// the frontmatter `name`.
	Slug string

	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	Metadata      map[string]string
	Body          string
}

// Report is the validator's output: the parsed skills + the diagnostics.
type Report struct {
	Skills []Skill
	// Issues lists every spec violation found. A clean report has Issues
	// empty. Each Issue carries the skill path so a caller can print
	// actionable diagnostics with no extra plumbing.
	Issues []Issue
}

// Ok returns true when no issues were found.
func (r *Report) Ok() bool { return len(r.Issues) == 0 }

// Issue is one spec violation. Path is the SKILL.md the violation belongs
// to (or the directory, for missing-file diagnostics); Field is the
// spec-named field; Message is the diagnostic.
type Issue struct {
	Path    string
	Field   string
	Message string
}

func (i Issue) String() string {
	if i.Field == "" {
		return fmt.Sprintf("%s: %s", i.Path, i.Message)
	}
	return fmt.Sprintf("%s: %s: %s", i.Path, i.Field, i.Message)
}

// ValidateFile parses and validates a single SKILL.md path. It returns the
// parsed Skill (always non-nil so a caller can print partial diagnostics)
// and a Report carrying every spec violation found.
//
// The skill's slug is derived from the parent directory's basename — the
// spec's `name` must match it.
func ValidateFile(path string) (*Skill, Report) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return &Skill{Path: path}, Report{Issues: []Issue{{Path: path, Message: err.Error()}}}
	}
	dir := filepath.Dir(abs)
	slug := filepath.Base(dir)
	s := &Skill{Dir: dir, Path: abs, Slug: slug}

	// The caller-supplied path is intentionally read — this is a validator
	// for in-tree SKILL.md files, invoked by drift-audit / smoke / CI; the
	// only data flow is on-disk markdown into a pure parser, never into a
	// privileged path. gosec G304 is suppressed for that reason.
	raw, err := os.ReadFile(abs) //nolint:gosec // SKILL.md path is the validator's input
	if err != nil {
		return s, Report{Issues: []Issue{{Path: abs, Message: err.Error()}}}
	}

	frontmatter, body, parseErr := splitFrontmatter(raw)
	if parseErr != nil {
		return s, Report{Issues: []Issue{{Path: abs, Field: "frontmatter", Message: parseErr.Error()}}}
	}
	s.Body = body

	var fm rawFrontmatter
	if err := yaml.Unmarshal(frontmatter, &fm); err != nil {
		return s, Report{Issues: []Issue{{Path: abs, Field: "frontmatter", Message: "YAML parse failed: " + err.Error()}}}
	}
	s.Name = strings.TrimSpace(fm.Name)
	s.Description = strings.TrimSpace(fm.Description)
	s.License = strings.TrimSpace(fm.License)
	s.Compatibility = strings.TrimSpace(fm.Compatibility)
	s.AllowedTools = strings.TrimSpace(fm.AllowedTools)
	s.Metadata = fm.Metadata

	issues := validateSkill(s)
	return s, Report{Skills: []Skill{*s}, Issues: issues}
}

// Validate walks a directory tree and validates every SKILL.md file under
// it. Discovery is one level deep by spec convention — a skill is one
// directory containing a SKILL.md. The walk is recursive so nested
// collections (e.g. skills/category/<slug>/SKILL.md) work, but each found
// SKILL.md is validated against its own immediate parent directory.
//
// Validate returns a combined Report and, separately, a wrapped error
// when the walk itself failed (a missing root, an unreadable directory).
// A skill-level violation populates Report.Issues; the error is nil.
func Validate(root string) (Report, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("skillcheck: resolve %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Report{}, fmt.Errorf("skillcheck: stat %q: %w", abs, err)
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("skillcheck: %q is not a directory", abs)
	}

	var report Report
	var paths []string
	walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(p) == "SKILL.md" {
			paths = append(paths, p)
		}
		return nil
	})
	if walkErr != nil {
		return report, fmt.Errorf("skillcheck: walk %q: %w", abs, walkErr)
	}
	sort.Strings(paths)
	for _, p := range paths {
		s, r := ValidateFile(p)
		// Always record the parsed Skill so callers can list "what we
		// found" even when issues are present.
		if s != nil {
			report.Skills = append(report.Skills, *s)
		}
		report.Issues = append(report.Issues, r.Issues...)
	}
	return report, nil
}

// rawFrontmatter is the YAML decoding target. Spec field names are kebab-
// case; the struct mirrors them via yaml tags.
type rawFrontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license"`
	Compatibility string            `yaml:"compatibility"`
	AllowedTools  string            `yaml:"allowed-tools"`
	Metadata      map[string]string `yaml:"metadata"`
}

// splitFrontmatter separates the YAML frontmatter from the Markdown body.
// A valid SKILL.md opens with `---\n` and ends the frontmatter with a line
// containing only `---`. The body is whatever follows the closing fence;
// leading whitespace is trimmed so an empty-line-only body counts as
// empty.
//
// Returns an error when the frontmatter is missing, malformed, or
// unterminated.
func splitFrontmatter(raw []byte) (frontmatter []byte, body string, err error) {
	// Normalise CRLF to LF so the fence detection is platform-agnostic.
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(raw, []byte("---\n")) && !bytes.Equal(raw, []byte("---")) {
		return nil, "", errors.New("SKILL.md must begin with a `---` frontmatter fence")
	}
	rest := raw[len("---\n"):]
	idx := bytes.Index(rest, []byte("\n---"))
	if idx < 0 {
		return nil, "", errors.New("SKILL.md frontmatter fence is not terminated by a `---` line")
	}
	frontmatter = rest[:idx]
	// Skip past `\n---` and the next newline (if present) to land on the
	// body. A trailing-only frontmatter (no body) leaves rest empty.
	after := rest[idx+len("\n---"):]
	// Trim a single leading newline (the closing fence's EOL).
	after = bytes.TrimPrefix(after, []byte("\n"))
	body = strings.TrimSpace(string(after))
	return frontmatter, body, nil
}

// validateSkill applies every spec constraint and returns the issues
// found. The order matches the spec's frontmatter table for readability.
func validateSkill(s *Skill) []Issue {
	var issues []Issue
	push := func(field, msg string) {
		issues = append(issues, Issue{Path: s.Path, Field: field, Message: msg})
	}

	// name — required + shape + slug match.
	switch {
	case s.Name == "":
		push("name", "is required")
	case len(s.Name) > maxNameLen:
		push("name", fmt.Sprintf("must be ≤ %d characters (was %d)", maxNameLen, len(s.Name)))
	case !nameRe.MatchString(s.Name):
		push("name", "must contain only lowercase letters, digits and hyphens")
	case strings.HasPrefix(s.Name, "-"):
		push("name", "must not start with a hyphen")
	case strings.HasSuffix(s.Name, "-"):
		push("name", "must not end with a hyphen")
	case strings.Contains(s.Name, "--"):
		push("name", "must not contain consecutive hyphens")
	}
	if s.Name != "" && s.Slug != "" && s.Name != s.Slug {
		push("name", fmt.Sprintf("must match the parent directory name %q (was %q)", s.Slug, s.Name))
	}

	// description — required + length.
	switch {
	case s.Description == "":
		push("description", "is required")
	case len(s.Description) > maxDescriptionLen:
		push("description", fmt.Sprintf("must be ≤ %d characters (was %d)", maxDescriptionLen, len(s.Description)))
	}

	// compatibility — optional, but length-capped when present.
	if s.Compatibility != "" && len(s.Compatibility) > maxCompatibilityLen {
		push("compatibility", fmt.Sprintf("must be ≤ %d characters (was %d)", maxCompatibilityLen, len(s.Compatibility)))
	}

	// body — must be non-empty Markdown; an empty body is a stub.
	if strings.TrimSpace(s.Body) == "" {
		push("body", "must contain non-empty Markdown content after the frontmatter")
	}

	return issues
}

package manifest

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidManifest is the sentinel every manifest loading or validation
// failure wraps, so callers can branch with errors.Is.
var ErrInvalidManifest = errors.New("dockyard/internal/manifest: invalid manifest")

// Error is a single source-located manifest fault. It names the source file
// and, where the YAML node position is known, the line — so an invalid manifest
// fails with a "file:line" message (RFC §4.2 acceptance). Error wraps
// ErrInvalidManifest.
type Error struct {
	// Source is the manifest file path (or another identifier passed to Load).
	Source string
	// Line is the 1-based YAML line; 0 when no position is available, in which
	// case the rendered message degrades to "file" rather than "file:line".
	Line int
	// Field is the dotted manifest path of the fault, e.g. "tools[0].input".
	Field string
	// Msg is the human-facing description.
	Msg string
}

// Error renders the fault as "source:line: field: msg", omitting any empty
// component.
func (e *Error) Error() string {
	var b strings.Builder
	if e.Source != "" {
		b.WriteString(e.Source)
		if e.Line > 0 {
			fmt.Fprintf(&b, ":%d", e.Line)
		}
		b.WriteString(": ")
	}
	if e.Field != "" {
		b.WriteString(e.Field)
		b.WriteString(": ")
	}
	b.WriteString(e.Msg)
	return b.String()
}

// Unwrap reports ErrInvalidManifest so errors.Is(err, ErrInvalidManifest) holds.
func (e *Error) Unwrap() error { return ErrInvalidManifest }

// ErrorList aggregates several source-located faults found in a single pass, so
// Validate can report every problem at once rather than one per run. It wraps
// ErrInvalidManifest.
type ErrorList []*Error

// Error renders one fault per line.
func (l ErrorList) Error() string {
	switch len(l) {
	case 0:
		return "dockyard/internal/manifest: no errors"
	case 1:
		return l[0].Error()
	}
	parts := make([]string, len(l))
	for i, e := range l {
		parts[i] = e.Error()
	}
	return fmt.Sprintf("%d manifest errors:\n  - %s", len(l), strings.Join(parts, "\n  - "))
}

// Unwrap reports ErrInvalidManifest so errors.Is(err, ErrInvalidManifest) holds.
func (l ErrorList) Unwrap() error { return ErrInvalidManifest }

// errCollector accumulates *Error values during a validation pass and renders
// them as one ErrorList (or nil when none were collected).
type errCollector struct {
	source string
	errs   ErrorList
}

// add records a fault. line 0 means "no position known".
func (c *errCollector) add(line int, field, format string, args ...any) {
	c.errs = append(c.errs, &Error{
		Source: c.source,
		Line:   line,
		Field:  field,
		Msg:    fmt.Sprintf(format, args...),
	})
}

// err returns the collected faults as an error, or nil when the pass was clean.
func (c *errCollector) err() error {
	if len(c.errs) == 0 {
		return nil
	}
	return c.errs
}

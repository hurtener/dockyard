package manifest

import (
	"fmt"
	"regexp"
	"strings"
)

// Validate runs structural validation on a Manifest loaded by some other means
// (Load already validates). It re-checks every rule without YAML positions, so
// reported faults name the source but not a line.
//
// Validation is structural only: it checks required fields, well-formed values,
// known enums, uniqueness, and tool->app cross-references. It never reads Go
// source or the filesystem — resolving the tool contracts is ResolveContracts,
// and enforcing the quality.* gates is dockyard validate (Phase 18, RFC §9.4).
func (m *Manifest) Validate() error {
	return m.validate("", nil)
}

// semverRe matches a strict MAJOR.MINOR.PATCH core with an optional
// "-prerelease" and "+build" suffix — the shape RFC §4.2's example uses.
var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// nameRe matches a tool/app/manifest identifier: a lowercase token of letters,
// digits, hyphens, and underscores, starting with a letter.
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// uiURIRe matches a well-formed ui:// resource URI: ui://<host>/<path...>
// (RFC §7.1). Host and each path segment are non-empty.
var uiURIRe = regexp.MustCompile(`^ui://[a-z0-9][a-z0-9-]*(?:/[A-Za-z0-9._-]+)+$`)

// validate is the shared body of Load and Validate. pos may be nil, in which
// case faults carry no line.
func (m *Manifest) validate(source string, pos *positionIndex) error {
	c := &errCollector{source: source}
	at := func(field string) int { return pos.line(field) }

	// --- identity (RFC §4.2) ---
	if strings.TrimSpace(m.Name) == "" {
		c.add(at("name"), "name", "required")
	} else if !nameRe.MatchString(m.Name) {
		c.add(at("name"), "name", "%q is not a valid identifier (lowercase, [a-z0-9_-], leading letter)", m.Name)
	}
	if strings.TrimSpace(m.Title) == "" {
		c.add(at("title"), "title", "required")
	}
	if strings.TrimSpace(m.Version) == "" {
		c.add(at("version"), "version", "required")
	} else if !semverRe.MatchString(m.Version) {
		c.add(at("version"), "version", "%q is not a semantic version (MAJOR.MINOR.PATCH)", m.Version)
	}

	m.validateRuntime(c, pos)
	m.validateTools(c, pos)
	m.validateApps(c, pos)

	return c.err()
}

// validateRuntime checks the runtime block (RFC §4.2, §5.2, §7.4).
func (m *Manifest) validateRuntime(c *errCollector, pos *positionIndex) {
	at := func(field string) int { return pos.line(field) }
	if len(m.Runtime.Transports) == 0 {
		c.add(at("runtime"), "runtime.transports", "at least one transport is required")
	}
	seen := map[Transport]bool{}
	for i, t := range m.Runtime.Transports {
		if seen[t] {
			c.add(at(fmt.Sprintf("runtime.transports[%d]", i)),
				fmt.Sprintf("runtime.transports[%d]", i), "duplicate transport %q", t)
		}
		seen[t] = true
	}
	// runtime.ui is optional (a plain MCP server omits it); its enum fields are
	// already decode-validated. When present, both fields must be set.
	if m.Runtime.UI != nil {
		if m.Runtime.UI.Framework == "" {
			c.add(at("runtime.ui"), "runtime.ui.framework", "required when runtime.ui is set")
		}
		if m.Runtime.UI.Bundle == "" {
			c.add(at("runtime.ui"), "runtime.ui.bundle", "required when runtime.ui is set")
		}
	}
}

// validateTools checks the tools list: required fields, contract references,
// uniqueness, and the ui: cross-reference into apps[] (RFC §4.2, §6.1).
func (m *Manifest) validateTools(c *errCollector, pos *positionIndex) {
	at := func(field string) int { return pos.line(field) }
	if len(m.Tools) == 0 {
		c.add(at("tools"), "tools", "at least one tool is required")
	}
	appIDs := map[string]bool{}
	for _, a := range m.Apps {
		appIDs[a.ID] = true
	}
	seen := map[string]bool{}
	for i, t := range m.Tools {
		field := fmt.Sprintf("tools[%d]", i)
		if strings.TrimSpace(t.Name) == "" {
			c.add(at(field), field+".name", "required")
		} else {
			if !nameRe.MatchString(t.Name) {
				c.add(at(field+".name"), field+".name", "%q is not a valid tool name", t.Name)
			}
			if seen[t.Name] {
				c.add(at(field+".name"), field+".name", "duplicate tool name %q", t.Name)
			}
			seen[t.Name] = true
		}
		if strings.TrimSpace(t.Description) == "" {
			c.add(at(field), field+".description", "required")
		}
		if err := validateContractRef(t.Input); err != nil {
			c.add(at(field+".input"), field+".input", "%v", err)
		}
		if err := validateContractRef(t.Output); err != nil {
			c.add(at(field+".output"), field+".output", "%v", err)
		}
		if t.UI != "" && !appIDs[t.UI] {
			c.add(at(field+".ui"), field+".ui", "references unknown app id %q (no matching apps[].id)", t.UI)
		}
	}
}

// validateApps checks the apps list: required fields, well-formed ui:// URIs,
// id and uri uniqueness, and the display_modes subset (RFC §4.2, §7).
func (m *Manifest) validateApps(c *errCollector, pos *positionIndex) {
	at := func(field string) int { return pos.line(field) }
	seenID := map[string]bool{}
	seenURI := map[string]bool{}
	for i, a := range m.Apps {
		field := fmt.Sprintf("apps[%d]", i)
		if strings.TrimSpace(a.ID) == "" {
			c.add(at(field), field+".id", "required")
		} else {
			if !nameRe.MatchString(a.ID) {
				c.add(at(field+".id"), field+".id", "%q is not a valid app id", a.ID)
			}
			if seenID[a.ID] {
				c.add(at(field+".id"), field+".id", "duplicate app id %q", a.ID)
			}
			seenID[a.ID] = true
		}
		if strings.TrimSpace(a.URI) == "" {
			c.add(at(field), field+".uri", "required")
		} else {
			if !uiURIRe.MatchString(a.URI) {
				c.add(at(field+".uri"), field+".uri", "%q is not a well-formed ui:// resource URI", a.URI)
			}
			if seenURI[a.URI] {
				c.add(at(field+".uri"), field+".uri", "duplicate app uri %q", a.URI)
			}
			seenURI[a.URI] = true
		}
		if strings.TrimSpace(a.Entry) == "" {
			c.add(at(field), field+".entry", "required")
		}
		if len(a.DisplayModes) == 0 {
			c.add(at(field), field+".display_modes", "at least one display mode is required")
		}
		seenMode := map[DisplayMode]bool{}
		for j, dm := range a.DisplayModes {
			if seenMode[dm] {
				c.add(at(fmt.Sprintf("%s.display_modes[%d]", field, j)),
					fmt.Sprintf("%s.display_modes[%d]", field, j), "duplicate display mode %q", dm)
			}
			seenMode[dm] = true
		}
		seenVis := map[Visibility]bool{}
		for j, v := range a.Visibility {
			if seenVis[v] {
				c.add(at(fmt.Sprintf("%s.visibility[%d]", field, j)),
					fmt.Sprintf("%s.visibility[%d]", field, j), "duplicate visibility %q", v)
			}
			seenVis[v] = true
		}
	}
}

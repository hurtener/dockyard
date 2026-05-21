package manifest

import (
	"fmt"
	"net/url"
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

// validateOrigin checks that raw is a well-formed CSP source origin: a
// scheme://host[:port] value with no path, query, or fragment — the shape a
// CSP connect-src / img-src directive accepts (RFC §7.4). It returns a
// human-readable reason on rejection, or "" when raw is well-formed.
//
// A bare host ("api.company.com") is rejected: a CSP source must carry an
// explicit scheme so a host applies it unambiguously. The wildcard scheme-less
// forms a CSP also accepts ("*", "'self'", "data:") are out of V1 scope — the
// manifest declares concrete external origins.
func validateOrigin(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "empty origin"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Sprintf("%q is not a valid origin: %v", raw, err)
	}
	if u.Scheme == "" {
		return fmt.Sprintf("%q must carry an explicit scheme (e.g. https://host)", raw)
	}
	if u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "wss" && u.Scheme != "ws" {
		return fmt.Sprintf("%q scheme %q is not an allowed CSP origin scheme (https, http, wss, ws)", raw, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Sprintf("%q has no host", raw)
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Sprintf("%q must be a scheme://host[:port] origin with no path, query, or fragment", raw)
	}
	return ""
}

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

	m.validateTaskSupportCoherence(c, pos)
}

// validateTaskSupportCoherence checks that the Tasks declarations across the
// tools list are mutually consistent (RFC §8.4, §8.6). An App's UI is built
// against one task model; when two tools wire the same apps[] entry they must
// agree on task_support, otherwise the App's UI cannot know whether to poll a
// task lifecycle or render a synchronous result. A non-default task_support on
// a tool that does not exist in the validated set is impossible — the enum is
// already decode-validated — so the only cross-field coherence the manifest can
// own is this per-App agreement.
func (m *Manifest) validateTaskSupportCoherence(c *errCollector, pos *positionIndex) {
	at := func(field string) int { return pos.line(field) }
	// For each App id, the first tool that wires it and that tool's index.
	type ref struct {
		support TaskSupport
		field   string
	}
	firstByApp := map[string]ref{}
	for i, t := range m.Tools {
		if t.UI == "" {
			continue
		}
		support := t.TaskSupport
		if support == "" {
			support = TaskSupportForbidden
		}
		field := fmt.Sprintf("tools[%d]", i)
		prev, seen := firstByApp[t.UI]
		if !seen {
			firstByApp[t.UI] = ref{support: support, field: field}
			continue
		}
		if prev.support != support {
			c.add(at(field+".task_support"), field+".task_support",
				"tool wires app %q with task_support %q but %s wires the same app with %q "+
					"— tools sharing a ui:// app must agree on task_support (RFC §8.6)",
				t.UI, support, prev.field, prev.support)
		}
	}
}

// validateApps checks the apps list: required fields, well-formed ui:// URIs,
// id and uri uniqueness, the display_modes subset, well-formed CSP origins,
// CSP/bundle coherence, and that every app is referenced by some tool
// (RFC §4.2, §7).
func (m *Manifest) validateApps(c *errCollector, pos *positionIndex) {
	at := func(field string) int { return pos.line(field) }
	seenID := map[string]bool{}
	seenURI := map[string]bool{}
	// referencedApps collects every apps[].id some tool wires via tools[].ui,
	// so an orphan app — declared but referenced by no tool — can be flagged.
	referencedApps := map[string]bool{}
	for _, t := range m.Tools {
		if t.UI != "" {
			referencedApps[t.UI] = true
		}
	}
	// A single-file bundle inlines every asset and reaches zero external
	// origins, which is exactly why it makes the deny-by-default CSP just work
	// (RFC §7.4). An app that also declares csp.connect/csp.resource origins is
	// internally contradictory: it asks for external origins a single-file
	// bundle never loads. The bundle strategy is a runtime.ui setting; when
	// runtime.ui is absent or not single-file, the check does not apply.
	singleFileBundle := m.Runtime.UI != nil && m.Runtime.UI.Bundle == BundleSingleFile
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

		// CSP origins must be well-formed scheme://host[:port] values (RFC §7.4).
		for j, origin := range a.CSP.Connect {
			if reason := validateOrigin(origin); reason != "" {
				c.add(at(fmt.Sprintf("%s.csp.connect[%d]", field, j)),
					fmt.Sprintf("%s.csp.connect[%d]", field, j), "%s", reason)
			}
		}
		for j, origin := range a.CSP.Resource {
			if reason := validateOrigin(origin); reason != "" {
				c.add(at(fmt.Sprintf("%s.csp.resource[%d]", field, j)),
					fmt.Sprintf("%s.csp.resource[%d]", field, j), "%s", reason)
			}
		}

		// A single-file bundle declaring external CSP origins is internally
		// contradictory (RFC §7.4): a single-file bundle inlines everything and
		// loads no external origin, so opting one in cannot take effect. Flag
		// it as a manifest error rather than letting it ship as dead config.
		if singleFileBundle && (len(a.CSP.Connect) > 0 || len(a.CSP.Resource) > 0) {
			c.add(at(field+".csp"), field+".csp",
				"app declares csp origins but runtime.ui.bundle is %q — a single-file "+
					"bundle loads no external origin (RFC §7.4); use bundle: %s or drop the csp block",
				BundleSingleFile, BundleMultiFile)
		}

		// An orphan app — declared but wired by no tool — is dead manifest
		// config: the tools[].ui -> apps[].id direction is checked in
		// validateTools; this is the reverse direction (RFC §4.2).
		if strings.TrimSpace(a.ID) != "" && !referencedApps[a.ID] {
			c.add(at(field+".id"), field+".id",
				"app %q is referenced by no tool (no tools[].ui points at it)", a.ID)
		}
	}
}

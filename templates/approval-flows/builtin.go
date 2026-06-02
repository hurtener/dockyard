// Package approvalflows ships the approval-flows builtin template
// (Phase 25, RFC §10).
//
// This file is the only buildable .go in templates/approval-flows/. The
// rest of the directory holds the template's source tree — Go source as
// `.go.tmpl` files, Svelte files, fixtures, the manifest — which is
// `//go:embed`ed below and materialised into a working project by the
// scaffold's template-discovery seam (decision D-128).
//
// The blank import in cmd/dockyard wires this builtin into the
// process-wide template Registry via the init() block.
package approvalflows

import (
	"embed"
	"fmt"
	"strings"

	"github.com/hurtener/dockyard/internal/scaffold"
)

// source holds the entire template tree. `all:` keeps dotfiles
// (.gitignore.tmpl) so the materialised project ships them.
//
// The in-tree layout uses `pkg/{contracts,handlers}` rather than
// `internal/{contracts,handlers}` so the framework's own integration test
// can import them — Go's `internal/` barrier prevents an external test
// package from importing under another package's internal/. PathRemap
// (below) re-applies the `internal/` prefix at materialisation time,
// matching the analytics-widgets precedent.
//
//go:embed all:dockyard.app.yaml all:go.mod.tmpl all:main.go.tmpl all:pkg all:web all:fixtures all:README.md.tmpl all:.gitignore.tmpl
var source embed.FS

const (
	templateName    = "approval-flows"
	templateSummary = "Human-in-the-loop approval flows — request_approval + propose_with_edits. The Tasks×Apps showcase (RFC §8.6)."
)

// init registers the approval-flows template with the process-wide
// Registry. The CLI blank-imports this package; consumers of the
// Registry (the CLI's `--template` flag, the integration test) look the
// template up by name.
func init() {
	scaffold.RegisterTemplate(builtin())
}

// builtin returns the approval-flows EmbeddedTemplate. Exposed
// package-level (not just inside init) so an isolated Registry test can
// register it without touching the package-wide singleton.
func builtin() *scaffold.EmbeddedTemplate {
	return &scaffold.EmbeddedTemplate{
		NameValue:        templateName,
		SummaryValue:     templateSummary,
		Source:           source,
		PathPrefix:       "", // the embed.FS root IS the project root
		TextExts:         textualExts(),
		SubstitutionsFor: substitutionsFor,
		// pkg/{contracts,handlers} → internal/{contracts,handlers} on
		// materialisation. The framework keeps the in-tree path
		// non-internal so the Phase 25 integration test can import
		// them; users get the canonical RFC §4.3 layout.
		PathRemap: []scaffold.PathSubstitution{
			{From: "pkg/", To: "internal/"},
		},
	}
}

// textualExts is the set of file extensions whose contents are run
// through the substitution table. Anything not listed is copied
// byte-exact (binary assets, fixture JSON, …). The lists below cover
// every textual artifact the approval-flows template ships.
func textualExts() []string {
	return []string{
		".tmpl", ".yaml", ".yml", ".md", ".go", ".ts", ".js",
		".svelte", ".json", ".html", ".css",
	}
}

// substitutionsFor builds the per-materialisation substitution table.
func substitutionsFor(opts scaffold.Options) []scaffold.Substitution {
	module := opts.ModulePath
	if module == "" {
		module = "example.com/" + opts.Name
	}
	replaceBlock := ""
	if opts.DockyardReplace != "" {
		replaceBlock = fmt.Sprintf(
			"\nreplace github.com/hurtener/dockyard => %s\n",
			opts.DockyardReplace,
		)
	}
	bridgeSpec, uiSpec := scaffold.WebDepSpecs(opts)
	// Same ordering rule as analytics-widgets: the in-tree import-path
	// rewrite runs BEFORE the __MODULE_PATH__ rewrite.
	return []scaffold.Substitution{
		{
			From: "github.com/hurtener/dockyard/templates/approval-flows/pkg",
			To:   module + "/internal",
		},
		{From: "__PROJECT_NAME__", To: opts.Name},
		{From: "__PROJECT_TITLE__", To: titleCase(opts.Name)},
		{From: "__MODULE_PATH__", To: module},
		{From: "__DOCKYARD_VERSION__", To: scaffold.RequireVersion(opts.DockyardVersion)},
		{From: "__DOCKYARD_REPLACE_BLOCK__", To: replaceBlock},
		{From: "__DOCKYARD_BRIDGE_SPEC__", To: bridgeSpec},
		{From: "__DOCKYARD_UI_SPEC__", To: uiSpec},
	}
}

// titleCase mirrors the analytics-widgets local helper — "my-server"
// → "My Server".
func titleCase(name string) string {
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// Package analyticswidgets ships the analytics-widgets builtin template
// (Phase 24, RFC §10, decision D-124).
//
// This file is the only buildable .go in templates/analytics-widgets/. The
// rest of the directory holds the template's source tree — Go source as
// `.go.tmpl` files, Svelte files, fixtures, the manifest — which is
// `//go:embed`ed below and materialised into a working project by the
// scaffold's template-discovery seam (decision D-128).
//
// The blank import in cmd/dockyard wires this builtin into the
// process-wide template Registry via the init() block.
package analyticswidgets

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
// (below) re-applies the `internal/` prefix at materialisation time, so a
// developer who runs `dockyard new --template analytics-widgets` gets the
// canonical `internal/{contracts,handlers}` shape RFC §4.3 documents.
//
//go:embed all:dockyard.app.yaml all:go.mod.tmpl all:main.go.tmpl all:pkg all:web all:fixtures all:README.md.tmpl all:.gitignore.tmpl
var source embed.FS

const (
	templateName    = "analytics-widgets"
	templateSummary = "Inline analytics widgets — chart / table / metric card. Three contract-first tools, one inline-only App."
)

// init registers the analytics-widgets template with the process-wide
// Registry. The CLI blank-imports this package; consumers of the Registry
// (the CLI's `--template` flag, the integration test) look the template up by
// name.
func init() {
	scaffold.RegisterTemplate(builtin())
}

// builtin returns the analytics-widgets EmbeddedTemplate. Exposed
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
		// materialisation. The framework keeps the in-tree path non-
		// internal so the Phase 24 integration test can import them;
		// users get the canonical RFC §4.3 layout.
		PathRemap: []scaffold.PathSubstitution{
			{From: "pkg/", To: "internal/"},
		},
	}
}

// textualExts is the set of file extensions whose contents are run through
// the substitution table. Anything not listed is copied byte-exact (binary
// assets, fixture JSON, …). The lists below cover every textual artifact
// the analytics-widgets template ships.
func textualExts() []string {
	return []string{
		".tmpl", ".yaml", ".yml", ".md", ".go", ".ts", ".js",
		".svelte", ".json", ".html", ".css",
	}
}

// substitutionsFor builds the per-materialisation substitution table from
// the developer's Options. Tokens are double-underscored on each side so a
// real source line cannot collide.
func substitutionsFor(opts scaffold.Options) []scaffold.Substitution {
	module := opts.ModulePath
	if module == "" {
		module = "example.com/" + opts.Name
	}
	replaceBlock := ""
	if opts.DockyardReplace != "" {
		// Two leading newlines so the block is visually separated from the
		// `require` line in the rendered go.mod. The trailing newline is
		// the go.mod terminator.
		replaceBlock = fmt.Sprintf(
			"\nreplace github.com/hurtener/dockyard => %s\n",
			opts.DockyardReplace,
		)
	}
	// Web sibling of the pre-publish go.mod replace: when --dockyard-web-path
	// is set (or derived from --dockyard-path), the template's web/package.json
	// resolves dockyard-bridge and dockyard-ui to absolute file:// paths into
	// the local checkout. Otherwise the scaffold pins the published packages by
	// a caret range derived from the CLI version (v1.3 wave B — D-172), so a
	// scaffold with no --dockyard-path resolves them from npm with no checkout.
	bridgeSpec, uiSpec := scaffold.WebDepSpecs(opts)
	// IMPORTANT — substitution ORDER matters. The in-tree import-path
	// rewrite must run BEFORE the __MODULE_PATH__ rewrite, because the
	// in-tree path is the form the template author writes in real .go
	// files so the framework can compile + test them in-place, and the
	// materialiser rewrites that path to the user's project module path.
	// `__MODULE_PATH__` is the older placeholder form (used in main.go
	// — a `.tmpl` file — for the same purpose). Both forms resolve to
	// the same module-path value.
	return []scaffold.Substitution{
		{
			// The in-tree import path of the template's contracts +
			// handlers packages → the materialised project's path. The
			// `pkg/` ↔ `internal/` directory split is mirrored here so
			// the rewritten path matches the PathRemap above.
			From: "github.com/hurtener/dockyard/templates/analytics-widgets/pkg",
			To:   module + "/internal",
		},
		{From: "__PROJECT_NAME__", To: opts.Name},
		{From: "__PROJECT_TITLE__", To: titleCase(opts.Name)},
		{From: "__MODULE_PATH__", To: module},
		{From: "__DOCKYARD_REPLACE_BLOCK__", To: replaceBlock},
		{From: "__DOCKYARD_BRIDGE_SPEC__", To: bridgeSpec},
		{From: "__DOCKYARD_UI_SPEC__", To: uiSpec},
	}
}

// titleCase mirrors internal/scaffold.titleCase ("my-server" -> "My Server")
// without exporting the latter. It is kept local so a future template can
// use its own casing strategy without coupling to the no-template scaffold.
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

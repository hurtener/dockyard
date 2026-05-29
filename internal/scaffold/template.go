package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

// ErrUnknownTemplate is the sentinel for an unregistered template name. Callers
// branch with errors.Is(err, ErrUnknownTemplate).
var ErrUnknownTemplate = errors.New("dockyard/internal/scaffold: unknown template")

// Template is one product-pattern showcase a developer can scaffold with
// `dockyard new --template <name>`. Implementations are registered with
// RegisterTemplate, typically from an init() block in a small builtin file
// that lives next to the template's source tree (CLAUDE.md §4.4: interface +
// Registry + driver-style init registration).
//
// A Template's Materialise produces the project-relative file set the
// scaffolder writes to disk. Substitutions (project name, module path, the
// pre-release Dockyard replace directive) are the Template's responsibility:
// each Template knows which of its files are textual and which must stay
// byte-exact (a binary asset, a fixture, a checked-in built artifact).
type Template interface {
	// Name is the wire name a developer passes to `dockyard new --template <name>`.
	// It matches the Registry key and is short, kebab-case.
	Name() string
	// Summary is one-line help text the CLI prints next to the template name.
	Summary() string
	// Materialise builds the project file set in memory, keyed by project-
	// relative path. The returned map is the entire scaffolded project — the
	// CLI writes it to disk verbatim. Identical Options must yield identical
	// bytes (the materialiser is deterministic — the golden test depends on it).
	Materialise(opts Options) (map[string][]byte, error)
}

// Registry holds the process-wide Template set. RegisterTemplate /
// LookupTemplate / ListTemplates are the seam Phases 25, 26, and any post-V1
// template plug into; nothing about a specific template's name lives in the
// CLI or in this file.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]Template
}

// defaultRegistry is the package-wide singleton the builtin templates register
// into. Callers use the top-level RegisterTemplate / LookupTemplate /
// ListTemplates helpers; the Registry type is exported for testing (a test can
// build its own isolated Registry to register a stub Template without
// polluting the process-wide one).
var defaultRegistry = &Registry{templates: map[string]Template{}}

// NewRegistry returns an empty Registry. Exposed for tests that want an
// isolated Registry (the package-wide RegisterTemplate is shared).
func NewRegistry() *Registry {
	return &Registry{templates: map[string]Template{}}
}

// Register adds a Template to this Registry. A duplicate name panics — a
// template's name is part of its public CLI surface, so a collision is a
// program error rather than a runtime user error.
func (r *Registry) Register(t Template) {
	if t == nil {
		panic("scaffold.Registry.Register: nil template")
	}
	name := t.Name()
	if name == "" {
		panic("scaffold.Registry.Register: template has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.templates[name]; dup {
		panic(fmt.Sprintf("scaffold.Registry.Register: duplicate template %q", name))
	}
	r.templates[name] = t
}

// Lookup returns the Template registered under name and true, or a zero value
// and false. Concurrent-safe: ConcurrentRegisterAndLookup proves it under
// -race.
func (r *Registry) Lookup(name string) (Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.templates[name]
	return t, ok
}

// List returns the registered Templates ordered by name — a stable order for
// the CLI's `dockyard new --help` listing and the smoke script's grep.
func (r *Registry) List() []Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Template, 0, len(r.templates))
	for _, t := range r.templates {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// RegisterTemplate adds a Template to the package-wide default Registry. Call
// from an init() block in the builtin file next to a template's source tree.
func RegisterTemplate(t Template) { defaultRegistry.Register(t) }

// LookupTemplate is the package-wide default Registry's Lookup.
func LookupTemplate(name string) (Template, bool) { return defaultRegistry.Lookup(name) }

// ListTemplates returns every Template registered in the package-wide default
// Registry, sorted by name.
func ListTemplates() []Template { return defaultRegistry.List() }

// GenerateFromTemplate materialises the named Template into a working project
// (RFC §10). It validates the project name, refuses a non-empty target, looks
// up the Template by name (typed ErrUnknownTemplate on miss), builds the
// project file set in memory via the Template, then writes the tree to disk.
//
// The returned Result lists every file written. GenerateFromTemplate is
// deterministic: identical (opts, templateName) always yields the same bytes.
func GenerateFromTemplate(opts Options, templateName string) (Result, error) {
	if err := validateName(opts.Name); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(templateName) == "" {
		return Result{}, fmt.Errorf("%w: template name is empty", ErrUnknownTemplate)
	}
	tmpl, ok := LookupTemplate(templateName)
	if !ok {
		known := make([]string, 0, len(ListTemplates()))
		for _, t := range ListTemplates() {
			known = append(known, t.Name())
		}
		return Result{}, fmt.Errorf("%w: %q (known: %s)",
			ErrUnknownTemplate, templateName, strings.Join(known, ", "))
	}

	dir := opts.projectDir()
	if err := checkTarget(dir, opts.Here); err != nil {
		return Result{}, err
	}

	files, err := tmpl.Materialise(opts)
	if err != nil {
		return Result{}, fmt.Errorf("dockyard/internal/scaffold: materialise template %q: %w",
			templateName, err)
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf(
			"dockyard/internal/scaffold: template %q materialised an empty file set",
			templateName)
	}

	if err := writeTree(dir, files); err != nil {
		return Result{}, err
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return Result{Dir: dir, Files: paths}, nil
}

// -- Embedded-source template helpers -----------------------------------------
//
// A builtin template is implemented as an `embed.FS` snapshot of its source
// tree plus a textual-substitution table. The helpers below turn an embed.FS
// into the in-memory file map a Template's Materialise returns. Keeping the
// helpers in this file (not duplicated per template) is the seam's purpose:
// every future builtin template uses the same substitution pipeline.

// Substitution is one find-and-replace applied to every textual file in an
// embedded template tree (the substitution table is empty for binary or
// fixture-exact files — they round-trip byte-for-byte).
type Substitution struct {
	// From is the placeholder token a template author writes into the source
	// tree (e.g. "__PROJECT_NAME__"). Tokens are double-underscored on each
	// side so a real source line never accidentally collides with one.
	From string
	// To is the value the From token is replaced with — the user's choice
	// (the project name) or a derived value (the module path).
	To string
}

// EmbeddedTemplate is a reusable Template implementation backed by an
// embed.FS snapshot of a `templates/<name>/` directory. The builtin
// analytics-widgets template uses it; future builtin templates use it too.
// A test stub can implement Template directly without going through this.
type EmbeddedTemplate struct {
	// NameValue is the template's wire name.
	NameValue string
	// SummaryValue is the one-line CLI help.
	SummaryValue string
	// Source is the embed.FS containing the template's source tree.
	// PathPrefix below points at the directory within it that is the
	// project root (so a `//go:embed all:templates/analytics-widgets`
	// FS with PathPrefix `templates/analytics-widgets` materialises
	// starting from the project root).
	Source fs.FS
	// PathPrefix is the directory inside Source whose contents are the
	// project root. The empty string means "the FS root itself".
	PathPrefix string
	// TextExts is the set of file extensions whose contents are treated as
	// text and run through the substitution table. Anything not listed is
	// copied byte-exact. A trailing-dotless ".go", ".yaml" form is expected;
	// matching is case-sensitive.
	TextExts []string
	// SubstitutionsFor returns the substitution table for one materialisation.
	// Called once per Materialise; the returned slice is read-only.
	SubstitutionsFor func(opts Options) []Substitution
	// PathRemap rewrites a project-relative source path to a different
	// destination path in the materialised project. The pairs are applied
	// in order as prefix substitutions: the first matching `From` prefix
	// is replaced with `To`. Used by templates whose in-repo layout
	// differs from the materialised layout — typically because Go's
	// `internal/` barrier prevents the framework's own tests from
	// importing the template's contract package, so the in-repo layout
	// uses a non-internal path and PathRemap re-applies the `internal/`
	// prefix on materialisation. Optional; nil/empty means a 1:1 copy.
	PathRemap []PathSubstitution
}

// PathSubstitution is one entry of an EmbeddedTemplate's PathRemap. From is a
// project-relative path prefix in the embed.FS; To is the prefix in the
// materialised project.
type PathSubstitution struct {
	From string
	To   string
}

// Name implements Template.
func (t *EmbeddedTemplate) Name() string { return t.NameValue }

// Summary implements Template.
func (t *EmbeddedTemplate) Summary() string { return t.SummaryValue }

// Materialise walks the embedded source tree, applies the substitution table
// to every textual file, and returns the in-memory project file set keyed by
// project-relative path. It is deterministic: a given (opts, embed.FS) always
// yields the same bytes — the golden test depends on it.
//
// Filename convention: a file with the trailing `.tmpl` extension is
// materialised under the same path with `.tmpl` stripped (e.g.
// `cmd/server/main.go.tmpl` → `cmd/server/main.go`). The `.tmpl` marker
// exists so the template's Go source does not satisfy `go build ./...` from
// the framework root — the placeholder module path would not parse — while
// the materialised project's files are real `.go` files. The marker
// extension is always added to TextExts so substitution is applied.
func (t *EmbeddedTemplate) Materialise(opts Options) (map[string][]byte, error) {
	subs := t.SubstitutionsFor(opts)
	textExt := make(map[string]struct{}, len(t.TextExts))
	for _, e := range t.TextExts {
		textExt[e] = struct{}{}
	}
	root := t.PathPrefix
	walkRoot := root
	if walkRoot == "" {
		walkRoot = "."
	}
	out := map[string][]byte{}
	err := fs.WalkDir(t.Source, walkRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// Skip the builtin.go that lives at the template root — it is
		// framework source, not project source. A template package's
		// builtin.go is by convention at the embed.FS root and named
		// "builtin.go".
		if path.Base(p) == "builtin.go" && path.Dir(p) == walkRoot {
			return nil
		}
		// Project-relative path is the FS path with the PathPrefix stripped.
		// When PathPrefix is empty the embed.FS root IS the project root,
		// so the path stays as-is — never collapse a leading dotfile (e.g.
		// `.gitignore.tmpl`) by stripping the walk's `.` root.
		rel := p
		if root != "" {
			rel = strings.TrimPrefix(rel, root)
			rel = strings.TrimPrefix(rel, "/")
		}
		if rel == "" {
			return nil
		}
		raw, readErr := fs.ReadFile(t.Source, p)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", p, readErr)
		}
		ext := path.Ext(rel)
		// A textual file gets the substitution pass; a `.tmpl` extension is
		// stripped from the materialised filename so the project ends up
		// with real `.go` / `.yaml` / `.md` files.
		if _, isText := textExt[ext]; isText {
			s := string(raw)
			for _, sub := range subs {
				s = strings.ReplaceAll(s, sub.From, sub.To)
			}
			raw = []byte(s)
			if ext == ".tmpl" {
				rel = strings.TrimSuffix(rel, ".tmpl")
				// After stripping `.tmpl`, the inner extension (e.g. `.go`)
				// may also be textual; substitution has already run, no
				// further pass needed.
			}
		}
		// Path remap is applied after substitution + .tmpl stripping so a
		// template author writes the remap against the materialised path
		// shape (e.g. "internal/" prefix), not the in-tree shape.
		for _, pr := range t.PathRemap {
			if strings.HasPrefix(rel, pr.From) {
				rel = pr.To + strings.TrimPrefix(rel, pr.From)
				break
			}
		}
		out[rel] = raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

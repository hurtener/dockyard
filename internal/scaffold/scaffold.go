package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/internal/manifest"
)

// ErrInvalidName is the sentinel for a rejected project name. Callers branch
// with errors.Is(err, ErrInvalidName).
var ErrInvalidName = errors.New("dockyard/internal/scaffold: invalid project name")

// ErrTargetExists is the sentinel for a non-empty / pre-existing target
// directory. `dockyard new` never overwrites an existing project (pass
// Options.Here to scaffold into a non-empty directory anyway).
var ErrTargetExists = errors.New("dockyard/internal/scaffold: target directory already exists and is not empty")

// ErrFileCollision is the sentinel for a scaffold output that would overwrite
// an existing file in the target directory. It can only occur under
// Options.Here (without it, a non-empty target is refused outright by
// ErrTargetExists). `dockyard new` never silently overwrites a file.
var ErrFileCollision = errors.New("dockyard/internal/scaffold: scaffold output would overwrite an existing file")

// nameRE constrains a project name to a short kebab-case token: it becomes the
// directory name, the manifest `name`, the Go module path tail and the MCP
// server identity, so it must be a safe identifier on every one of those axes.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}[a-z0-9]$`)

// Options configures one `dockyard new` invocation.
type Options struct {
	// Name is the project name — also the directory name, the manifest name,
	// and the MCP server identity. Required; validated against nameRE.
	Name string
	// Dir is the parent directory the project directory is created under.
	// Empty means the current working directory.
	Dir string
	// ModulePath is the Go module path for the scaffolded project's go.mod.
	// Empty falls back to "example.com/<name>" — a placeholder a developer
	// renames; the scaffold compiles either way.
	ModulePath string
	// DockyardReplace, when non-empty, adds a `replace` directive to the
	// scaffolded go.mod pointing the Dockyard runtime import at a local
	// checkout. It is the pre-release workflow: until Dockyard is published to
	// a module registry, a scaffolded project cannot `go get` it, so the CLI
	// and the integration test set this to a local Dockyard path. A released
	// Dockyard leaves it empty and the scaffold depends on the published
	// module version directly.
	DockyardReplace string
	// DockyardWebPath is the web/ sibling of DockyardReplace — an absolute path
	// to the Dockyard checkout's web/ directory. Templates that depend on the
	// in-repo @dockyard/bridge and @dockyard/ui packages substitute it into
	// their package.json `file:` dependencies so a scaffolded project can
	// `npm install` before the packages are published to npm. Empty leaves
	// published-version fallbacks in place (post-publish workflow).
	DockyardWebPath string
	// ExampleToolTaskSupport sets the no-template scaffold's example tool's
	// task_support declaration. The zero value ("") preserves the historical
	// behaviour — the example tool ships as task_support: forbidden, a plain
	// synchronous tool. Setting it to manifest.TaskSupportOptional or
	// TaskSupportRequired makes the scaffold both (a) declare the example
	// tool that way in the rendered manifest and (b) emit the engine-wired
	// shape of main.go — a `tasks.NewInMemoryStore()` + `tasks.NewEngine(...)`
	// block + `server.Options{Tasks: engine}` (D-164).
	//
	// This is the manifest-side knob the scaffold consumes; RequiresTasksEngine
	// is the corresponding read side that `dockyard run` consults at start time
	// against the project's loaded manifest.
	ExampleToolTaskSupport manifest.TaskSupport

	// DockyardVersion is the version of the Dockyard runtime module to pin in
	// the scaffolded go.mod's require directive — normally the version of the
	// `dockyard` CLI doing the scaffolding (cli.ResolvedVersion()). When it is
	// a real release version (vX.Y.Z) the require pins it, so a project that
	// drops the local `replace` resolves the published module from the proxy
	// without a hand edit. An empty value or the dev placeholder leaves the
	// historical `v0.0.0` placeholder (only ever resolved through the replace
	// directive — the build-from-source path).
	DockyardVersion string

	// Here permits scaffolding into a target directory that already has
	// content (the `dockyard new --here` flag). With Here false (the
	// default) a non-empty target is refused with ErrTargetExists. With
	// Here true the existing content is left in place, but a scaffold output
	// that would overwrite an existing file is still refused with
	// ErrFileCollision — `dockyard new` never silently overwrites a file.
	Here bool
}

// taskSupport returns the example tool's effective task_support declaration.
// The empty zero value normalises to forbidden — RFC §8.4 makes "omitted ==
// forbidden" the canonical reading, and the scaffold writes the explicit
// form so the manifest is self-documenting.
func (o Options) taskSupport() manifest.TaskSupport {
	if o.ExampleToolTaskSupport == "" {
		return manifest.TaskSupportForbidden
	}
	return o.ExampleToolTaskSupport
}

// wireTasksEngine reports whether the no-template scaffold's example tool
// requires the engine to be auto-wired into main.go. It is the renderer's
// side of D-164's detection.
func (o Options) wireTasksEngine() bool {
	switch o.taskSupport() {
	case manifest.TaskSupportOptional, manifest.TaskSupportRequired:
		return true
	}
	return false
}

// modulePath returns the effective Go module path.
func (o Options) modulePath() string {
	if o.ModulePath != "" {
		return o.ModulePath
	}
	return "example.com/" + o.Name
}

// projectDir returns the absolute-or-relative path of the project directory.
func (o Options) projectDir() string {
	if o.Dir == "" {
		return o.Name
	}
	return filepath.Join(o.Dir, o.Name)
}

// Result reports what Generate produced.
type Result struct {
	// Dir is the project directory that was created.
	Dir string
	// Files is the project-relative path of every file written, sorted.
	Files []string
}

// validateName checks a project name and returns a typed error on rejection.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidName)
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf(
			"%w: %q — a project name is a short kebab-case token: lowercase letters, "+
				"digits and hyphens, starting with a letter (e.g. my-mcp-server)",
			ErrInvalidName, name)
	}
	return nil
}

// Generate scaffolds a blank, working MCP server project (RFC §9.1, §10). It
// validates the name, refuses a non-empty target, builds the file set in
// memory (so a generation failure leaves nothing half-written), then writes
// the tree to disk.
//
// The returned Result lists every file written. Generate is deterministic: the
// same Options always yields the same bytes.
func Generate(opts Options) (Result, error) {
	if err := validateName(opts.Name); err != nil {
		return Result{}, err
	}

	dir := opts.projectDir()
	if err := checkTarget(dir, opts.Here); err != nil {
		return Result{}, err
	}

	files, err := buildFiles(opts)
	if err != nil {
		return Result{}, err
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

// checkTarget rejects a target directory that already exists with content. A
// missing directory is fine — Generate creates it. An empty directory is fine
// — scaffolding into an empty dir is a common workflow. A directory with any
// entry is refused unless here is set: `dockyard new` never overwrites a
// project. When it refuses, the error names the entries it found (so the
// developer sees that a hidden `.git/` or `.gitignore` is what blocked it) and
// points at --here.
func checkTarget(dir string, here bool) error {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("dockyard/internal/scaffold: stat target %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s exists and is a file", ErrTargetExists, dir)
	}
	if here {
		// A non-empty directory is permitted; file collisions are caught at
		// write time (writeTree) so existing content is never overwritten.
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("dockyard/internal/scaffold: read target %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %s contains %s — pass --here to scaffold into it anyway",
			ErrTargetExists, dir, listEntries(entries))
	}
	return nil
}

// listEntries renders up to the first few directory entries for an
// actionable error message (so "contains .git, .gitignore, README.md" tells
// the developer exactly what blocked the scaffold).
func listEntries(entries []os.DirEntry) string {
	const maxEntries = 5
	names := make([]string, 0, maxEntries)
	for i, e := range entries {
		if i == maxEntries {
			names = append(names, fmt.Sprintf("… (%d more)", len(entries)-maxEntries))
			break
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// buildFiles assembles the full project file set in memory, keyed by
// project-relative path. The contract artifacts (JSON Schema, TypeScript) are
// GENERATED here from the Go contract types via internal/codegen — P1: the
// scaffold ships generated contracts, never hand-written ones.
func buildFiles(opts Options) (map[string][]byte, error) {
	schemaFiles, err := generateContractArtifacts()
	if err != nil {
		return nil, err
	}

	files := map[string][]byte{
		"dockyard.app.yaml":               []byte(renderManifest(opts)),
		"go.mod":                          []byte(renderGoMod(opts)),
		"main.go":                         []byte(renderMainGo(opts)),
		"greet.go":                        []byte(renderGreetTool(opts)),
		"greet_test.go":                   []byte(renderGreetTest(opts)),
		"internal/contracts/contracts.go": []byte(contractsGoSource),
		"README.md":                       []byte(renderReadme(opts)),
		".gitignore":                      []byte(gitignoreContent),
	}
	for path, content := range schemaFiles {
		files[path] = content
	}
	return files, nil
}

// generateContractArtifacts produces the example tool's generated contract
// files: the input/output JSON Schemas and the TypeScript types. They are
// generated from the GreetInput / GreetOutput Go types — the contract-first
// guarantee made concrete (P1, RFC §6.1).
func generateContractArtifacts() (map[string][]byte, error) {
	inSchema, err := codegen.SchemaFor[GreetInput]()
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/scaffold: generate input schema: %w", err)
	}
	outSchema, err := codegen.SchemaFor[GreetOutput]()
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/scaffold: generate output schema: %w", err)
	}
	inJSON, err := codegen.Marshal(inSchema)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/scaffold: marshal input schema: %w", err)
	}
	outJSON, err := codegen.Marshal(outSchema)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/scaffold: marshal output schema: %w", err)
	}
	ts, err := codegen.TypeScriptForSource(contractsGoSource)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/scaffold: generate TypeScript: %w", err)
	}
	return map[string][]byte{
		"internal/contracts/greet_input.schema.json":  inJSON,
		"internal/contracts/greet_output.schema.json": outJSON,
		"internal/contracts/contracts.ts":             ts,
	}, nil
}

// writeTree writes files (keyed by project-relative path) under dir, creating
// parent directories as needed.
func writeTree(dir string, files map[string][]byte) error {
	// Never overwrite an existing file. Without --here this cannot trigger
	// (checkTarget guarantees an empty/new dir); with --here it is the guard
	// that keeps an existing file safe. Checked up front so a collision
	// leaves the tree untouched rather than half-written.
	var collisions []string
	for rel := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err == nil {
			collisions = append(collisions, rel)
		}
	}
	if len(collisions) > 0 {
		sort.Strings(collisions)
		return fmt.Errorf("%w: %s", ErrFileCollision, strings.Join(collisions, ", "))
	}

	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // a project directory is browsable, not a secret
		return fmt.Errorf("dockyard/internal/scaffold: create %s: %w", dir, err)
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil { //nolint:gosec // a project directory is browsable, not a secret
			return fmt.Errorf("dockyard/internal/scaffold: create dir for %s: %w", rel, err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil { //nolint:gosec // generated source, not a secret
			return fmt.Errorf("dockyard/internal/scaffold: write %s: %w", rel, err)
		}
	}
	return nil
}

// titleCase renders a kebab-case project name as a human title:
// "my-mcp-server" -> "My Mcp Server".
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

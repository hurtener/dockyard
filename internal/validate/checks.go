package validate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// checkManifest loads and structurally validates dockyard.app.yaml. It is the
// first, load-bearing check: a manifest that does not load at all is reported
// as a Blocker and false is returned, so Run skips the checks that need a
// coherent manifest.
//
// internal/manifest.LoadFile already enforces the manifest schema, required
// fields, enum values, identifier uniqueness, the tool→app cross-references and
// the Tasks-limit coherence (RFC §4.2, §8.5) — every fault wrapping
// ErrInvalidManifest. checkManifest surfaces that as one CheckManifest Blocker.
func checkManifest(rp *reporter, projectDir string) (loadedManifest, bool) {
	path := filepath.Join(projectDir, manifest.DefaultFilename)
	m, err := manifest.LoadFile(path)
	if err != nil {
		rp.block(CheckManifest, "%v", err)
		return loadedManifest{}, false
	}
	return loadedManifest{m: m, path: path}, true
}

// checkSchemas verifies that every tool's generated input and output JSON
// Schema file is present on disk and is a valid, resolvable JSON Schema. A
// schema that is absent, unparseable, or fails resolution is a Blocker
// (RFC §9.4 — "invalid manifest or schema").
//
// The stale-codegen check (checkStaleCodegen) separately proves the schema
// matches its Go source; this check proves the file is structurally a usable
// schema at all.
func checkSchemas(rp *reporter, projectDir string, lm loadedManifest) {
	for _, t := range lm.m.Tools {
		for _, side := range []string{"input", "output"} {
			rel := generate.SchemaFileName(t.Name, side)
			full := filepath.Join(projectDir, filepath.FromSlash(rel))
			raw, err := os.ReadFile(full) //nolint:gosec // path composed from a caller-supplied project dir
			if err != nil {
				rp.block(CheckSchema,
					"tool %q %s schema %s is missing — run `dockyard generate`", t.Name, side, rel)
				continue
			}
			var s jsonschema.Schema
			if err := json.Unmarshal(raw, &s); err != nil {
				rp.block(CheckSchema, "tool %q %s schema %s is not valid JSON Schema: %v",
					t.Name, side, rel, err)
				continue
			}
			if _, err := s.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true}); err != nil {
				rp.block(CheckSchema, "tool %q %s schema %s does not resolve: %v",
					t.Name, side, rel, err)
			}
		}
	}
}

// checkToolUIMappings verifies the tool↔UI resource wiring (RFC §4.2, §7.1).
//
// internal/manifest's structural validation already rejects a tools[].ui that
// references no apps[].id, and an apps[] entry referenced by no tool — those
// faults surface in checkManifest. This check adds the on-disk dimension the
// manifest cannot see: an App's `entry` .svelte source file must actually
// exist, otherwise the tool↔UI mapping points at nothing buildable.
func checkToolUIMappings(rp *reporter, projectDir string, lm loadedManifest) {
	for _, a := range lm.m.Apps {
		if strings.TrimSpace(a.Entry) == "" {
			continue // an empty entry is already a manifest Blocker.
		}
		entryPath := filepath.Join(projectDir, filepath.FromSlash(a.Entry))
		if info, err := os.Stat(entryPath); err != nil || info.IsDir() {
			rp.block(CheckMapping,
				"app %q entry %q does not exist — the tool↔UI mapping points at a missing file",
				a.ID, a.Entry)
			continue
		}
		if !strings.HasSuffix(a.Entry, ".svelte") {
			rp.block(CheckMapping,
				"app %q entry %q is not a .svelte file — V1 supports the Svelte UI framework only",
				a.ID, a.Entry)
		}
	}
}

// checkMIME verifies every App's resource MIME shape (RFC §7.1). The MVP Apps
// spec defines exactly one resource MIME type — text/html;profile=mcp-app — and
// an App is served under a well-formed ui:// URI. A malformed ui:// URI is
// already a manifest Blocker; checkMIME re-asserts the single-MIME invariant so
// a future manifest field carrying a MIME override would be caught here.
func checkMIME(rp *reporter, lm loadedManifest) {
	for _, a := range lm.m.Apps {
		if !strings.HasPrefix(a.URI, "ui://") {
			// A non-ui:// resource URI is already a manifest Blocker; nothing to
			// add MIME-wise.
			continue
		}
		// The MVP spec pins one MIME type. There is no manifest field to carry a
		// different one, so the only way this fails is a future regression — the
		// assertion documents and guards the invariant (brief 01 §5).
		if protocolcodec.MIMETypeApp != "text/html;profile=mcp-app" {
			rp.block(CheckMIME,
				"app %q: the MVP Apps MIME type changed to %q — regenerate against the vendored spec",
				a.ID, protocolcodec.MIMETypeApp)
		}
	}
}

// vendoredSpecs is the set of MCP spec snapshots `dockyard validate` checks
// compliance against. They are vendored, pinned files (CLAUDE.md §10): spec
// compliance is checked against these, never against a live host (CLAUDE.md §11).
var vendoredSpecs = []string{
	"docs/specifications/mcp-apps-2026-01-26.mdx",
	"docs/specifications/mcp-tasks-experimental.mdx",
}

// checkSpecCompliance checks the project's Apps and Tasks manifest constructs
// against the vendored MCP specs (RFC §9.4, CLAUDE.md §11). It never contacts a
// live host.
//
// The structural shape of an Apps/Tasks construct — ui:// URI grammar, the
// display-mode set, single-file CSP coherence, Tasks TTL coherence — is already
// enforced by internal/manifest against the same vendored specs. checkSpec adds
// the meta-check that the vendored spec snapshots are present at all (a project
// validated against an absent spec is validated against nothing) and the
// Tasks-construct cross-check the manifest cannot localise.
func checkSpecCompliance(rp *reporter, projectDir string, lm loadedManifest) {
	// The vendored specs live in the Dockyard repo, not a scaffolded project;
	// only flag their absence when validating from within the Dockyard tree.
	for _, rel := range vendoredSpecs {
		full := filepath.Join(projectDir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err == nil {
			continue
		}
		// Absent in a scaffolded project is expected and not a fault; absent in
		// the Dockyard repo itself would be a real regression. Distinguish by
		// whether any vendored spec dir exists.
		if _, dirErr := os.Stat(filepath.Join(projectDir, "docs", "specifications")); dirErr == nil {
			rp.block(CheckSpec, "vendored spec %s is missing — spec compliance cannot be checked", rel)
		}
	}

	// A tool declaring Tasks support must not also be a forbidden-task tool: the
	// manifest enum already rejects an unknown value, but a tool wired to a UI
	// App with task_support: required is a Tasks construct the vendored Tasks
	// spec constrains (brief 02 §4). The manifest's task-support coherence check
	// proves tools sharing an App agree; this asserts a UI-bearing required-task
	// tool is coherent with the manifest carrying a tasks block.
	for _, t := range lm.m.Tools {
		if t.TaskSupport == manifest.TaskSupportRequired && lm.m.Tasks == (manifest.Tasks{}) {
			rp.warn(CheckSpec,
				"tool %q declares task_support: required but the manifest sets no tasks limits "+
					"— a durable task surface should pin max_ttl_millis and a concurrency cap (RFC §8.5)",
				t.Name)
		}
	}
}

// stateMarkers maps a quality.* UI-state gate to the substring checkUIStates
// looks for in an App's .svelte entry source. The four-state page rule
// (CLAUDE.md §20) mandates loading / empty / error / permission states; the
// mechanically-checkable proxy is the presence of the state's name in the
// component source. It is a coarse check by design — a true render-time check
// is `dockyard test` (Phase 21) — but it catches the common defect: a page
// shipped with no empty or error branch at all.
var stateMarkers = []struct {
	name   string
	marker string
	gate   func(manifest.Quality) bool
}{
	{"loading", "loading", func(q manifest.Quality) bool { return q.RequireLoadingState }},
	{"empty", "empty", func(q manifest.Quality) bool { return q.RequireEmptyState }},
	{"error", "error", func(q manifest.Quality) bool { return q.RequireErrorState }},
	{"permission", "permission", func(q manifest.Quality) bool { return q.RequirePermissionState }},
}

// checkUIStates enforces the four-state page rule (CLAUDE.md §20, RFC §9.4
// "required defaults"). For each quality.* UI-state gate the manifest opts in,
// every App's .svelte entry source must mention the corresponding state. A
// missing required state is a Blocker (RFC §9.4 — the empty and error states
// are mandatory when the gate is on).
func checkUIStates(rp *reporter, projectDir string, lm loadedManifest) {
	q := lm.m.Quality
	anyGate := false
	for _, sm := range stateMarkers {
		if sm.gate(q) {
			anyGate = true
		}
	}
	if !anyGate {
		return // no UI-state gate opted in — nothing to enforce.
	}
	for _, a := range lm.m.Apps {
		if strings.TrimSpace(a.Entry) == "" {
			continue
		}
		entryPath := filepath.Join(projectDir, filepath.FromSlash(a.Entry))
		raw, err := os.ReadFile(entryPath) //nolint:gosec // path composed from a caller-supplied project dir
		if err != nil {
			// A missing entry file is already reported by checkToolUIMappings.
			continue
		}
		src := strings.ToLower(string(raw))
		for _, sm := range stateMarkers {
			if sm.gate(q) && !strings.Contains(src, sm.marker) {
				rp.block(CheckUIStates,
					"app %q entry %q has no %s state — quality.require_%s_state is on (the four-state "+
						"page rule, CLAUDE.md §20)",
					a.ID, a.Entry, sm.name, sm.name)
			}
		}
	}
}

// checkStaleCodegen is the P1 enforcement: it proves the generated JSON Schema
// and TypeScript on disk still match a fresh regeneration from the Go contract
// structs (RFC §6.2). Stale or drifted generated output is a Blocker, never a
// warning.
//
// It regenerates the artifacts with internal/generate.Plan (a dry run — no
// project file is touched) and compares each byte-for-byte against what is
// committed, via internal/codegen.CheckStale. A difference means a contract
// struct changed without `dockyard generate` being rerun.
func checkStaleCodegen(rp *reporter, projectDir string, lm loadedManifest) {
	planned, err := generate.Plan(generate.Options{ProjectDir: projectDir, Manifest: lm.m})
	if err != nil {
		// A codegen pipeline that cannot run at all is itself a Blocker — the
		// contract source does not compile, or a contract type is invalid.
		rp.block(CheckStaleCodegen, "codegen could not run: %v", err)
		return
	}
	for rel, fresh := range planned {
		full := filepath.Join(projectDir, filepath.FromSlash(rel))
		onDisk, err := os.ReadFile(full) //nolint:gosec // path composed from a caller-supplied project dir
		if err != nil {
			rp.block(CheckStaleCodegen,
				"generated file %s is missing — run `dockyard generate`", rel)
			continue
		}
		if staleErr := codegen.CheckStale(onDisk, fresh); staleErr != nil {
			if errors.Is(staleErr, codegen.ErrStaleGenerated) {
				rp.block(CheckStaleCodegen,
					"%s is stale — it does not match its Go contract source; run `dockyard generate`",
					rel)
				continue
			}
			rp.block(CheckStaleCodegen, "stale-codegen check on %s failed: %v", rel, staleErr)
		}
	}
}

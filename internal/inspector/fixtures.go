// Package inspector — on-disk fixture loader (Phase 24, D-126).
//
// The Phase 23 fixture switcher synthesises structuredContent from the tool's
// generated output schema. That is structurally correct (P1) but the synthesised
// values are placeholders — "sample-value" strings, 42 numbers — so the App's
// dispatcher does not see a kind it knows ("chart" / "table" / "metric_card")
// and the rendered widget can never be the realistic data the template ships.
//
// The fixture-loader below complements the switcher: when a project on disk
// carries a `fixtures/<tool>/<kind>.json` tree (the analytics-widgets template
// ships eighteen), the inspector loads them, and the frontend switcher prefers
// the on-disk payload over the synthesised one. The schema-derived path
// remains the fallback for projects that ship no fixtures, so the existing
// behaviour is preserved.
//
// The loader is read-only. It performs no `tools/call`; it only reads files
// the developer's own project carries. RFC §12 / P4 (the inspector is a
// dev surface, never a production MCP client) is preserved.
package inspector

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FixtureKind is one of the six fixture kinds (RFC §12, brief 04 §2.2).
type FixtureKind string

// The six fixture kinds the switcher offers.
const (
	FixtureHappy      FixtureKind = "happy"
	FixtureEmpty      FixtureKind = "empty"
	FixtureError      FixtureKind = "error"
	FixturePermission FixtureKind = "permission"
	FixtureSlow       FixtureKind = "slow"
	FixtureLarge      FixtureKind = "large"
)

// fixtureKinds lists the kinds in a stable order — the loader scans them in
// that order so the inspector picker is deterministic.
var fixtureKinds = []FixtureKind{
	FixtureHappy,
	FixtureEmpty,
	FixtureError,
	FixturePermission,
	FixtureSlow,
	FixtureLarge,
}

// rawFixture is the on-disk JSON shape the analytics-widgets template uses
// (and any future template ships). It carries the tool input the model would
// pass, the UI state the fixture represents, and an optional output_override
// the App's dispatcher renders directly. Unknown fields are tolerated.
type rawFixture struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	State          string         `json:"state"`
	Input          map[string]any `json:"input"`
	OutputOverride map[string]any `json:"output_override,omitempty"`
}

// ProjectFixture is one fixture surfaced to the frontend switcher. It is the
// inspector's own type — no JSON wire shape from a template leaks through
// unchanged (the loader normalises every field).
type ProjectFixture struct {
	// Tool is the tool name this fixture belongs to (e.g. "create_chart").
	Tool string `json:"tool"`
	// Kind is one of the six FixtureKind values.
	Kind FixtureKind `json:"kind"`
	// Description is the human-readable summary from the JSON.
	Description string `json:"description,omitempty"`
	// State is the UI state the fixture stands for ("ready"/"empty"/...).
	State string `json:"state"`
	// Input is the model-side input — surfaced for the inspector's RPC log
	// and the integration test, never used by the App's renderer.
	Input map[string]any `json:"input,omitempty"`
	// StructuredContent is the payload the App's dispatcher receives. When
	// the on-disk fixture carries `output_override`, it is used verbatim;
	// otherwise the loader derives it from `input` by pinning `kind` and
	// `state` so the App's discriminator routes correctly.
	StructuredContent map[string]any `json:"structuredContent"`
}

// FixtureSource produces the project's on-disk fixtures, by tool. It is
// invoked per `GET /api/fixtures` request — the inspector re-reads on every
// request so a developer can edit a fixture without restarting.
type FixtureSource func() ([]ProjectFixture, error)

// FixturesFromDir builds a FixtureSource that reads from `<dir>/fixtures/`.
// An empty dir, or a missing fixtures/ subtree, returns an empty slice and a
// nil error — the inspector degrades to the schema-derived synthetic
// fixtures. A malformed JSON file is a typed error so the developer notices.
func FixturesFromDir(dir string) FixtureSource {
	return func() ([]ProjectFixture, error) {
		if strings.TrimSpace(dir) == "" {
			return []ProjectFixture{}, nil
		}
		root := filepath.Join(dir, "fixtures")
		info, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return []ProjectFixture{}, nil
			}
			return nil, fmt.Errorf("dockyard/internal/inspector: stat %s: %w", root, err)
		}
		if !info.IsDir() {
			return []ProjectFixture{}, nil
		}
		return readFixtureTree(root)
	}
}

// readFixtureTree walks the fixtures/ directory and loads every `<tool>/<kind>.json`
// it finds. Only the six known kinds are loaded; an unrecognised filename is
// skipped (forward-compat: a template may ship more under a different
// switcher in the future). The returned slice is sorted by tool then by the
// fixed kind order so the frontend picker is deterministic.
func readFixtureTree(root string) ([]ProjectFixture, error) {
	tools, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: read %s: %w", root, err)
	}
	out := make([]ProjectFixture, 0, len(tools)*len(fixtureKinds))
	for _, t := range tools {
		if !t.IsDir() {
			continue
		}
		tool := t.Name()
		toolDir := filepath.Join(root, tool)
		for _, kind := range fixtureKinds {
			path := filepath.Join(toolDir, string(kind)+".json")
			info, statErr := os.Stat(path)
			if statErr != nil || info.IsDir() {
				continue
			}
			fixture, loadErr := loadFixture(tool, kind, path)
			if loadErr != nil {
				return nil, loadErr
			}
			out = append(out, fixture)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tool != out[j].Tool {
			return out[i].Tool < out[j].Tool
		}
		return kindOrder(out[i].Kind) < kindOrder(out[j].Kind)
	})
	return out, nil
}

// loadFixture reads one fixture JSON, normalises it into a ProjectFixture,
// and derives a structuredContent payload when the JSON ships no
// `output_override`. The `path` is constructed from a controlled
// `fixtures/<tool>/<kind>.json` shape where `tool` comes from a
// `ReadDir` of the project's fixtures/ subtree and `kind` is one of the
// six well-known FixtureKind values — there is no user-supplied input on
// this path. The inspector itself is dev-mode-gated and localhost-only
// (CLAUDE.md §7).
func loadFixture(tool string, kind FixtureKind, path string) (ProjectFixture, error) {
	body, err := os.ReadFile(path) //nolint:gosec // G304: path is bounded to fixtures/<tool>/<kind>.json by the walker above.
	if err != nil {
		return ProjectFixture{}, fmt.Errorf("dockyard/internal/inspector: read %s: %w", path, err)
	}
	var raw rawFixture
	if err := json.Unmarshal(body, &raw); err != nil {
		return ProjectFixture{}, fmt.Errorf("dockyard/internal/inspector: parse %s: %w", path, err)
	}
	state := strings.TrimSpace(raw.State)
	if state == "" {
		state = string(kind)
	}
	sc := raw.OutputOverride
	if len(sc) == 0 {
		sc = deriveStructuredContent(tool, state, raw.Input)
	}
	return ProjectFixture{
		Tool:              tool,
		Kind:              kind,
		Description:       raw.Description,
		State:             state,
		Input:             raw.Input,
		StructuredContent: sc,
	}, nil
}

// deriveStructuredContent builds a structuredContent payload from a fixture's
// input by pinning the well-known dispatcher fields (`kind`, `state`,
// `theme`). This mirrors the analytics-widgets handlers: every handler
// constructs its output by copying its input and adding `kind` + `state`.
// Templates that need richer derivation should ship `output_override`.
//
// The toolToKind mapping is intentionally explicit — RFC §6 / brief 04: the
// inspector knows the V1 template set's dispatcher discriminators, and any
// future template either ships an `output_override` (recommended) or chooses
// one of the known kinds. An unrecognised tool name produces a `state`-only
// payload, which the App will route to its loading/unknown branch — a clean,
// loud fallback the developer can spot rather than a silent placeholder.
func deriveStructuredContent(tool, state string, input map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range input {
		out[k] = v
	}
	if kind, ok := toolToKind[tool]; ok {
		out["kind"] = kind
	}
	out["state"] = state
	if _, hasTheme := out["theme"]; !hasTheme {
		out["theme"] = "auto"
	}
	return out
}

// toolToKind is the discriminator mapping for the V1 template set. The
// analytics-widgets template ships exactly these three tools (Phase 24).
var toolToKind = map[string]string{
	"create_chart":       "chart",
	"create_table":       "table",
	"create_metric_card": "metric_card",
}

// kindOrder returns the stable index of a FixtureKind for sorting.
func kindOrder(k FixtureKind) int {
	for i, kk := range fixtureKinds {
		if kk == k {
			return i
		}
	}
	return len(fixtureKinds)
}

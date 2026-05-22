package inspector

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hurtener/dockyard/internal/generate"
	"github.com/hurtener/dockyard/internal/manifest"
)

// contractEntry is one row of the `/api/contracts` payload — the JSON shape the
// inspector frontend's contract model (`web/inspector/src/lib/contracts.ts`)
// decodes. It is the inspector's own type: no raw codegen or manifest struct
// leaks through it (P3). The fixture switcher derives its fixtures from the
// generated `outputSchema` (P1 — contract-first), so the schemas are surfaced
// verbatim from the project's generated files, never hand-written here.
type contractEntry struct {
	// Name is the tool's MCP wire name.
	Name string `json:"name"`
	// Description is the tool's model-facing hint.
	Description string `json:"description,omitempty"`
	// InputSchema is the generated JSON Schema for the tool's input struct.
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	// OutputSchema is the generated JSON Schema for the tool's output struct.
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

// ContractsFromProject adapts a Dockyard project's manifest and its generated
// JSON Schema files into a [ContractsSource] rooted at projectDir. It is the
// seam RFC §12 names: the inspector's fixture switcher derives its fixtures
// from the project's generated tool contracts (P1 — contract-first), never
// from a hand-written schema.
//
// The returned source reads, per request, the project's `dockyard.app.yaml`
// for the tool list and each tool's generated `<tool>_input.schema.json` /
// `<tool>_output.schema.json` from internal/contracts/ (the files
// `dockyard generate` writes). A tool whose schema file is missing — the
// project was never `dockyard generate`d — still yields a contract row with an
// empty schema; the fixture switcher renders its four-state empty state for
// that tool rather than crashing.
//
// When the project has no manifest at all (the inspector is attached to a
// remote `--url` with no local project), the source returns an empty array and
// the Fixtures / Tools panels degrade to their honest empty state. The source
// never panics and never returns a nil [json.RawMessage].
func ContractsFromProject(projectDir string) ContractsSource {
	return func() json.RawMessage {
		entries := contractsForProject(projectDir)
		raw, err := json.Marshal(entries)
		if err != nil || len(raw) == 0 {
			return json.RawMessage("[]")
		}
		return raw
	}
}

// contractsForProject builds the contract rows for a project. A missing or
// unparseable manifest yields an empty slice — the inspector degrades to its
// empty state rather than failing.
func contractsForProject(projectDir string) []contractEntry {
	if projectDir == "" {
		return []contractEntry{}
	}
	m, err := manifest.LoadFile(filepath.Join(projectDir, manifest.DefaultFilename))
	if err != nil {
		return []contractEntry{}
	}
	entries := make([]contractEntry, 0, len(m.Tools))
	for _, tool := range m.Tools {
		entries = append(entries, contractEntry{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  readSchemaFile(projectDir, tool.Name, "input"),
			OutputSchema: readSchemaFile(projectDir, tool.Name, "output"),
		})
	}
	return entries
}

// readSchemaFile reads a tool contract's generated JSON Schema file from the
// project's internal/contracts/ directory. side is "input" or "output". A
// missing or unreadable file (the project was never generated) yields a nil
// RawMessage — the contract row still renders, the fixture switcher shows the
// tool's empty state.
func readSchemaFile(projectDir, toolName, side string) json.RawMessage {
	rel := generate.SchemaFileName(toolName, side)
	path := filepath.Join(projectDir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path) //nolint:gosec // path is composed from a caller-supplied project dir
	if err != nil || !json.Valid(data) {
		return nil
	}
	return json.RawMessage(data)
}

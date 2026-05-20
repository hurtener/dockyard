// This file is the Phase 05 cross-subsystem integration test (AGENTS.md §17).
// Phase 05's Deps name Phase 04's internal/codegen, and Phase 05 closes the
// Design A drift seam (RFC §6.2): the JSON Schema half (Phase 04) and the
// TypeScript half (Phase 05) are generated independently from one Go contract,
// and the drift cross-check guarantees they agree. The test drives a contract
// end to end with no mocks — a real Go contract source → codegen.SchemaFor +
// codegen.TypeScriptForSource → codegen.CrossCheck — and covers the failure
// modes: a mutated TypeScript file fails CrossCheck, and a stale on-disk
// artifact fails CheckStale.
package integration

import (
	"errors"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/codegen"
)

// driftContractInput / driftContractOutput are the Phase 05 integration
// contract pair. The Go struct is the single source of truth (P1); both the
// schema and the TypeScript are generated from it.
type driftContractInput struct {
	Query string `json:"query" jsonschema:"the search query"`
	Limit int    `json:"limit,omitempty"`
}

type driftContractHit struct {
	Title string  `json:"title"`
	Score float64 `json:"score"`
}

type driftContractOutput struct {
	Total int                `json:"total"`
	Hits  []driftContractHit `json:"hits"`
	Note  string             `json:"note,omitempty"`
}

// driftContractTS is the Go contract source for the same pair, in the shape the
// TypeScript generator consumes. Field names and json tags match the structs
// above exactly, so the two independently generated artifacts agree.
const driftContractTS = "// SearchInput is the input contract.\n" +
	"type SearchInput struct {\n" +
	"\tQuery string `json:\"query\"`\n" +
	"\tLimit int    `json:\"limit,omitempty\"`\n" +
	"}\n\n" +
	"type SearchHit struct {\n" +
	"\tTitle string  `json:\"title\"`\n" +
	"\tScore float64 `json:\"score\"`\n" +
	"}\n\n" +
	"// SearchOutput is the output contract.\n" +
	"type SearchOutput struct {\n" +
	"\tTotal int         `json:\"total\"`\n" +
	"\tHits  []SearchHit `json:\"hits\"`\n" +
	"\tNote  string      `json:\"note,omitempty\"`\n" +
	"}\n"

// TestPhase05_SchemaAndTSCrossCheck drives a contract through both generators
// and confirms the drift cross-check passes when schema and TypeScript agree.
func TestPhase05_SchemaAndTSCrossCheck(t *testing.T) {
	inSchema, err := codegen.SchemaFor[driftContractInput]()
	if err != nil {
		t.Fatalf("SchemaFor input: %v", err)
	}
	outSchema, err := codegen.SchemaFor[driftContractOutput]()
	if err != nil {
		t.Fatalf("SchemaFor output: %v", err)
	}
	ts, err := codegen.TypeScriptForSource(driftContractTS)
	if err != nil {
		t.Fatalf("TypeScriptForSource: %v", err)
	}
	if !strings.Contains(string(ts), "export interface SearchOutput {") {
		t.Fatalf("generated TS missing SearchOutput interface:\n%s", ts)
	}
	if err := codegen.CrossCheck(inSchema, "SearchInput", ts); err != nil {
		t.Errorf("CrossCheck(input) on a matched pair should pass: %v", err)
	}
	if err := codegen.CrossCheck(outSchema, "SearchOutput", ts); err != nil {
		t.Errorf("CrossCheck(output) on a matched pair should pass: %v", err)
	}
}

// TestPhase05_DriftDetected confirms the cross-check hard-fails when the
// TypeScript desyncs from the schema — the silent server↔UI drift that mcp-use
// could not catch (brief 04 §2.6).
func TestPhase05_DriftDetected(t *testing.T) {
	outSchema, err := codegen.SchemaFor[driftContractOutput]()
	if err != nil {
		t.Fatalf("SchemaFor output: %v", err)
	}
	// A hand-mutated TypeScript file that drops the `hits` property.
	mutated := []byte("export interface SearchOutput {\n" +
		"  total: number;\n" +
		"  note?: string;\n" +
		"}\n")
	err = codegen.CrossCheck(outSchema, "SearchOutput", mutated)
	if err == nil {
		t.Fatal("CrossCheck should fail when TypeScript desyncs from the schema")
	}
	if !errors.Is(err, codegen.ErrSchemaTSDrift) {
		t.Errorf("error should wrap ErrSchemaTSDrift, got %v", err)
	}
	if !strings.Contains(err.Error(), "hits") {
		t.Errorf("error should name the drifted property: %v", err)
	}
}

// TestPhase05_StaleGeneratedDetected confirms CheckStale hard-fails when an
// on-disk generated artifact no longer matches a fresh regeneration of its Go
// source — the "generated types out of date = build blocker" rule (brief 06 R1).
func TestPhase05_StaleGeneratedDetected(t *testing.T) {
	stale, err := codegen.TypeScriptForSource(driftContractTS)
	if err != nil {
		t.Fatalf("TypeScriptForSource (stale): %v", err)
	}
	// The Go contract gained a field; the on-disk TS was not regenerated.
	freshSource := strings.Replace(driftContractTS,
		"\tNote  string      `json:\"note,omitempty\"`\n",
		"\tNote     string `json:\"note,omitempty\"`\n"+
			"\tRegion   string `json:\"region\"`\n",
		1)
	fresh, err := codegen.TypeScriptForSource(freshSource)
	if err != nil {
		t.Fatalf("TypeScriptForSource (fresh): %v", err)
	}
	if err := codegen.CheckStale(stale, fresh); err == nil {
		t.Fatal("CheckStale should flag a stale generated artifact")
	} else if !errors.Is(err, codegen.ErrStaleGenerated) {
		t.Errorf("error should wrap ErrStaleGenerated, got %v", err)
	}
	// Sanity: a fresh artifact compared against itself is not stale.
	if err := codegen.CheckStale(fresh, fresh); err != nil {
		t.Errorf("CheckStale on identical bytes should pass: %v", err)
	}
}

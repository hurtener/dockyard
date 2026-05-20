package codegen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hurtener/dockyard/internal/codegen"
)

// Golden tests pin the generated TypeScript (fixed contract source → fixed TS)
// so a drift in codegen, or a regression in the upstream tygo generator,
// surfaces as a visible diff rather than a silent desync (AGENTS.md §11, brief
// 06 R1). Regenerate the fixtures with:
//
//	go test ./internal/codegen/ -run TestGoldenTypeScript -update
//
// then review the diff. The -update flag is shared with the schema golden test
// (golden_test.go).

// tsGoldenContract names a TypeScript golden fixture and the Go contract source
// it is generated from.
type tsGoldenContract struct {
	file    string
	source  string
	options []codegen.TSOption
}

// scalarsTSSource exercises every scalar TypeScript type plus optionality.
const scalarsTSSource = `// ScalarsInput exercises every scalar type plus optionality.
type ScalarsInput struct {
	Name    string  ` + "`json:\"name\" jsonschema:\"the customer name\"`" + `
	Count   int     ` + "`json:\"count\"`" + `
	Ratio   float64 ` + "`json:\"ratio,omitempty\"`" + `
	Enabled bool    ` + "`json:\"enabled\"`" + `
	Note    string  ` + "`json:\"note,omitzero\"`" + `
}
`

// nestedTSSource exercises nested structs and slices.
const nestedTSSource = `// HealthSignal is one weighted signal.
type HealthSignal struct {
	Label  string ` + "`json:\"label\"`" + `
	Weight int    ` + "`json:\"weight,omitempty\"`" + `
}

// NestedOutput exercises nested structs and slices.
type NestedOutput struct {
	Summary string         ` + "`json:\"summary\" jsonschema:\"a short headline\"`" + `
	Score   int            ` + "`json:\"score\"`" + `
	Signals []HealthSignal ` + "`json:\"signals\"`" + `
}
`

// revenueTSSource is the brief 04 §3 worked example — the full contract pair.
const revenueTSSource = `// ShowRevenueInput is the input contract for the show_revenue tool.
type ShowRevenueInput struct {
	Period string ` + "`json:\"period\" jsonschema:\"the reporting period, e.g. 2026-Q1\"`" + `
	Region string ` + "`json:\"region,omitempty\" jsonschema:\"optional region filter\"`" + `
}

// RevenueLine is one line of a revenue breakdown.
type RevenueLine struct {
	Label  string  ` + "`json:\"label\"`" + `
	Amount float64 ` + "`json:\"amount\"`" + `
}

// ShowRevenueOutput is the output contract for the show_revenue tool.
type ShowRevenueOutput struct {
	Headline string        ` + "`json:\"headline\" jsonschema:\"the model-facing summary line\"`" + `
	Total    float64       ` + "`json:\"total\"`" + `
	Lines    []RevenueLine ` + "`json:\"lines\"`" + `
	Currency string        ` + "`json:\"currency,omitempty\"`" + `
}
`

// enumTSSource exercises a string enum, a map field, and a pointer field —
// tygo preserves constants/enums where reflection-based generators lose them.
const enumTSSource = `// Severity is the severity of an event.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// EventRecord exercises an enum, a map, and a pointer field.
type EventRecord struct {
	ID      string            ` + "`json:\"id\"`" + `
	Level   Severity          ` + "`json:\"level\"`" + `
	Labels  map[string]string ` + "`json:\"labels\"`" + `
	Replace *EventRecord      ` + "`json:\"replace,omitempty\"`" + `
}
`

func tsGoldenContracts() []tsGoldenContract {
	return []tsGoldenContract{
		{file: "scalars_input.ts.golden", source: scalarsTSSource},
		{file: "nested_output.ts.golden", source: nestedTSSource},
		{file: "show_revenue.ts.golden", source: revenueTSSource},
		{file: "enum_record.ts.golden", source: enumTSSource},
		{
			file:    "show_revenue_null.ts.golden",
			source:  revenueTSSource,
			options: []codegen.TSOption{codegen.WithNullOptional()},
		},
	}
}

func TestGoldenTypeScript(t *testing.T) {
	for _, gc := range tsGoldenContracts() {
		t.Run(gc.file, func(t *testing.T) {
			t.Parallel() // generation is a pure function — safe to run concurrently
			got, err := codegen.TypeScriptForSource(gc.source, gc.options...)
			if err != nil {
				t.Fatalf("TypeScriptForSource: %v", err)
			}
			path := filepath.Join("testdata", gc.file)
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o750); err != nil {
					t.Fatalf("mkdir testdata: %v", err)
				}
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path) //nolint:gosec // path is a fixed testdata file
			if err != nil {
				t.Fatalf("read golden %s (run with -update to create it): %v", path, err)
			}
			if string(got) != string(want) {
				t.Errorf("generated TypeScript for %s drifted from golden.\n--- got ---\n%s\n--- want ---\n%s",
					gc.file, got, want)
			}
		})
	}
}

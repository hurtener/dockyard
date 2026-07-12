package codegen_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/hurtener/dockyard/internal/codegen"
)

// updateGolden regenerates the .golden fixtures. Run:
//
//	go test ./internal/codegen/ -run TestGolden -update
//
// then review the diff. Golden tests pin the generated schema (fixed contract →
// fixed JSON) so a drift in codegen or a regression in the upstream inference
// engine surfaces as a visible diff, never a silent desync (AGENTS.md §11,
// brief 06 R1/R3).
var updateGolden = flag.Bool("update", false, "regenerate codegen golden files")

// goldenContract names a fixture and supplies its generated, marshalled schema.
type goldenContract struct {
	file   string
	schema func() ([]byte, error)
}

func goldenContracts() []goldenContract {
	return []goldenContract{
		{"scalars_input.golden", func() ([]byte, error) {
			s, err := codegen.SchemaFor[scalarsInput]()
			if err != nil {
				return nil, err
			}
			return codegen.Marshal(s)
		}},
		{"nested_output.golden", func() ([]byte, error) {
			s, err := codegen.SchemaFor[nestedOutput]()
			if err != nil {
				return nil, err
			}
			return codegen.Marshal(s)
		}},
		{"show_revenue_input.golden", func() ([]byte, error) {
			s, err := codegen.SchemaFor[showRevenueInput]()
			if err != nil {
				return nil, err
			}
			return codegen.Marshal(s)
		}},
		{"show_revenue_output.golden", func() ([]byte, error) {
			s, err := codegen.SchemaFor[showRevenueOutput]()
			if err != nil {
				return nil, err
			}
			return codegen.Marshal(s)
		}},
		// Depth-audit fixtures: time.Time + json.RawMessage + a registered enum
		// (findings 1–3) and an embedded struct inlined into the schema
		// (finding 4). A regression in any of these now surfaces as a diff.
		{"shapes_contract.golden", func() ([]byte, error) {
			s, err := codegen.SchemaFor[shapesContract](codegen.WithEnum("auditSeverity",
				string(auditSeverityInfo), string(auditSeverityWarn), string(auditSeverityError)))
			if err != nil {
				return nil, err
			}
			return codegen.Marshal(s)
		}},
		{"embedded_event.golden", func() ([]byte, error) {
			s, err := codegen.SchemaFor[auditEvent]()
			if err != nil {
				return nil, err
			}
			return codegen.Marshal(s)
		}},
		{"recursive_node.golden", func() ([]byte, error) {
			s, err := codegen.SchemaFor[auditNode]()
			if err != nil {
				return nil, err
			}
			return codegen.Marshal(s)
		}},
	}
}

// showRevenueInput / showRevenueOutput are the brief 04 §3 worked example —
// the contract pair the typed tool builder is demonstrated against.
type showRevenueInput struct {
	Period string `json:"period" jsonschema:"the reporting period, e.g. 2026-Q1"`
	Region string `json:"region,omitempty" jsonschema:"optional region filter"`
}

type revenueLine struct {
	Label  string  `json:"label"`
	Amount float64 `json:"amount"`
}

type showRevenueOutput struct {
	Headline string        `json:"headline" jsonschema:"the model-facing summary line"`
	Total    float64       `json:"total"`
	Lines    []revenueLine `json:"lines"`
	Currency string        `json:"currency,omitempty"`
}

func TestGoldenSchemas(t *testing.T) {
	for _, gc := range goldenContracts() {
		t.Run(gc.file, func(t *testing.T) {
			got, err := gc.schema()
			if err != nil {
				t.Fatalf("generate schema: %v", err)
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
				t.Errorf("generated schema for %s drifted from golden.\n--- got ---\n%s\n--- want ---\n%s",
					gc.file, got, want)
			}
		})
	}
}

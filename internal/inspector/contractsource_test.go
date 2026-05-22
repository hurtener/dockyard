package inspector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// r1Manifest is a real dockyard.app.yaml — the manifest ContractsFromProject
// loads to enumerate a project's tools.
const r1TestManifest = `name: contract-source-test
title: Contract Source Test
version: 0.1.0
runtime:
  transports: [http]
tools:
  - name: report
    description: region report
    input: internal/contracts.ReportInput
    output: internal/contracts.ReportOutput
`

// writeContractProject lays down a real Dockyard project directory: a real
// manifest and, when withSchemas is true, real generated JSON Schema files.
func writeContractProject(t *testing.T, withSchemas bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dockyard.app.yaml"),
		[]byte(r1TestManifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if withSchemas {
		contractsDir := filepath.Join(dir, "internal", "contracts")
		if err := os.MkdirAll(contractsDir, 0o750); err != nil {
			t.Fatalf("mkdir contracts: %v", err)
		}
		schemas := map[string]string{
			"report_input.schema.json":  `{"type":"object","properties":{"region":{"type":"string"}}}`,
			"report_output.schema.json": `{"type":"object","properties":{"total":{"type":"integer"}}}`,
		}
		for name, body := range schemas {
			if err := os.WriteFile(filepath.Join(contractsDir, name),
				[]byte(body), 0o600); err != nil {
				t.Fatalf("write schema %s: %v", name, err)
			}
		}
	}
	return dir
}

// TestContractsFromProject covers the project-rooted contracts source: a
// project with generated schemas yields a full contract array; a project that
// was never generated still yields a contract row (with no schema); a missing
// or empty project degrades to an empty array.
func TestContractsFromProject(t *testing.T) {
	t.Parallel()

	t.Run("a generated project yields full contracts", func(t *testing.T) {
		t.Parallel()
		src := ContractsFromProject(writeContractProject(t, true))
		var entries []contractEntry
		if err := json.Unmarshal(src(), &entries); err != nil {
			t.Fatalf("unmarshal contracts: %v", err)
		}
		if len(entries) != 1 || entries[0].Name != "report" {
			t.Fatalf("contracts = %+v, want one 'report' contract", entries)
		}
		if entries[0].Description != "region report" {
			t.Errorf("description = %q, want 'region report'", entries[0].Description)
		}
		if len(entries[0].InputSchema) == 0 || len(entries[0].OutputSchema) == 0 {
			t.Errorf("contract carried no generated schema: %+v", entries[0])
		}
	})

	t.Run("an ungenerated project yields a schemaless contract row", func(t *testing.T) {
		t.Parallel()
		src := ContractsFromProject(writeContractProject(t, false))
		var entries []contractEntry
		if err := json.Unmarshal(src(), &entries); err != nil {
			t.Fatalf("unmarshal contracts: %v", err)
		}
		if len(entries) != 1 || entries[0].Name != "report" {
			t.Fatalf("contracts = %+v, want one 'report' contract row", entries)
		}
		if len(entries[0].OutputSchema) != 0 {
			t.Errorf("ungenerated project surfaced a schema: %+v", entries[0])
		}
	})

	t.Run("no project degrades to an empty array", func(t *testing.T) {
		t.Parallel()
		src := ContractsFromProject(t.TempDir()) // no manifest
		if got := string(src()); got != "[]" {
			t.Fatalf("contracts for a non-project: got %q, want []", got)
		}
		empty := ContractsFromProject("")
		if got := string(empty()); got != "[]" {
			t.Fatalf("contracts for an empty dir: got %q, want []", got)
		}
	})
}

// TestContractsEndpoint_FromProject wires ContractsFromProject through the
// real `/api/contracts` HTTP endpoint — the wiring `dockyard inspect` uses.
func TestContractsEndpoint_FromProject(t *testing.T) {
	t.Parallel()
	insp, err := New(Options{
		Contracts: ContractsFromProject(writeContractProject(t, true)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = insp.Serve(ctx) }()
	waitReady(t, insp.URL()+"/api/info")
	body := httpGet(t, insp.URL()+"/api/contracts")
	if !contains(body, "report") || !contains(body, "outputSchema") {
		t.Fatalf("/api/contracts did not surface the project's generated contract: %q", body)
	}
}

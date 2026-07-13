package manifest

import (
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// stringType returns reflect.Type for string — a non-object contract type the
// codegen engine must reject (RFC §6.3).
func stringType() reflect.Type { return reflect.TypeFor[string]() }

// Contract types used by the resolver tests. They stand in for the
// internal/contracts.* types a real app declares; the resolver runs them
// through internal/codegen exactly as dockyard generate will.

// echoInput is a tool input contract fixture.
type echoInput struct {
	Message string `json:"message" jsonschema:"the text to echo"`
}

// echoOutput is a tool output contract fixture.
type echoOutput struct {
	Echoed string `json:"echoed"`
	Length int    `json:"length"`
}

func TestParseContractReference(t *testing.T) {
	tests := []struct {
		ref      string
		wantPkg  string
		wantType string
		wantErr  bool
	}{
		{"internal/contracts.EchoInput", "internal/contracts", "EchoInput", false},
		{"pkg.Type", "pkg", "Type", false},
		{"a/b/c.DeepType", "a/b/c", "DeepType", false},
		{"NoPackage", "", "", true},
		{"pkg.lowercase", "", "", true},
		{"", "", "", true},
		{"pkg.", "", "", true},
		{".Type", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			got, err := ParseContractReference(tc.ref)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseContractReference(%q): want error, got %+v", tc.ref, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseContractReference(%q): unexpected error: %v", tc.ref, err)
			}
			if got.Package != tc.wantPkg || got.TypeName != tc.wantType {
				t.Errorf("got %+v, want pkg=%q type=%q", got, tc.wantPkg, tc.wantType)
			}
			if got.String() != tc.ref {
				t.Errorf("round-trip: String() = %q, want %q", got.String(), tc.ref)
			}
		})
	}
}

// TestResolveContracts_RealResolver closes the manifest<->codegen seam with the
// real internal/codegen-backed resolver (AGENTS.md §17 integration test): the
// Phase 06 Deps name Phase 04, so this proves the wiring, not a mock.
func TestResolveContracts_RealResolver(t *testing.T) {
	m := &Manifest{
		Name: "echo-app", Title: "Echo App", Version: "1.0.0",
		Runtime: Runtime{Transports: []Transport{TransportStdio}},
		Tools: []Tool{{
			Name:        "echo",
			Description: "Echo.",
			Input:       "internal/contracts.EchoInput",
			Output:      "internal/contracts.EchoOutput",
		}},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("fixture manifest is invalid: %v", err)
	}

	r := NewRegistryResolver()
	Register[echoInput](r, "internal/contracts.EchoInput")
	Register[echoOutput](r, "internal/contracts.EchoOutput")

	got, err := m.ResolveContracts(r)
	if err != nil {
		t.Fatalf("ResolveContracts: %v", err)
	}
	tc, ok := got["echo"]
	if !ok {
		t.Fatal("ResolveContracts: no entry for tool echo")
	}
	if tc.Input == nil || tc.Output == nil {
		t.Fatalf("resolved schemas are nil: %+v", tc)
	}
	if tc.Input.Type != "object" {
		t.Errorf("input schema type = %q, want object", tc.Input.Type)
	}
	if _, ok := tc.Input.Properties["message"]; !ok {
		t.Errorf("input schema missing 'message' property: %+v", tc.Input.Properties)
	}
	if _, ok := tc.Output.Properties["echoed"]; !ok {
		t.Errorf("output schema missing 'echoed' property: %+v", tc.Output.Properties)
	}
}

func TestResolveContracts_Unresolved(t *testing.T) {
	m := &Manifest{
		Tools: []Tool{{Name: "t", Input: "internal/contracts.Missing", Output: "internal/contracts.AlsoMissing"}},
	}
	r := NewRegistryResolver()
	_, err := m.ResolveContracts(r)
	if err == nil {
		t.Fatal("ResolveContracts with no registered types: want error, got nil")
	}
	if !errors.Is(err, ErrContractUnresolved) {
		t.Errorf("error does not wrap ErrContractUnresolved: %v", err)
	}
}

func TestResolveContracts_NilResolver(t *testing.T) {
	m := &Manifest{Tools: []Tool{{Name: "t", Input: "internal/contracts.In", Output: "internal/contracts.Out"}}}
	if _, err := m.ResolveContracts(nil); err == nil {
		t.Fatal("ResolveContracts(nil): want error, got nil")
	}
}

// TestRegistryResolver_RejectsNonObject proves the resolver surfaces a codegen
// rejection: a scalar contract type cannot be a tool contract (RFC §6.3).
func TestRegistryResolver_RejectsNonObject(t *testing.T) {
	r := NewRegistryResolver()
	r.types["pkg.Scalar"] = stringType()
	_, err := r.Resolve("pkg.Scalar")
	if err == nil {
		t.Fatal("Resolve of a non-object contract type: want error, got nil")
	}
}

// TestResolveContracts_ConcurrentReads confirms ResolveContracts is safe on a
// loaded Manifest with a built (read-only) RegistryResolver under -race.
func TestResolveContracts_ConcurrentReads(t *testing.T) {
	m, err := LoadFile(filepath.Join("testdata", "valid-full.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistryResolver()
	Register[echoInput](r, "internal/contracts.ShowCustomerHealthInput")
	Register[echoOutput](r, "internal/contracts.ShowCustomerHealthOutput")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.ResolveContracts(r); err != nil {
				t.Errorf("concurrent ResolveContracts: %v", err)
			}
		}()
	}
	wg.Wait()
}

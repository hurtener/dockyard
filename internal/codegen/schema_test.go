package codegen_test

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hurtener/dockyard/internal/codegen"
)

// --- helpers ---------------------------------------------------------------

// schemaHasType reports whether s declares the given JSON Schema type, via
// either the single Type field or the multi-valued Types field (a nilable Go
// type renders as ["null", <type>]).
func schemaHasType(s *jsonschema.Schema, want string) bool {
	if s.Type == want {
		return true
	}
	return slices.Contains(s.Types, want)
}

// --- contract fixtures -----------------------------------------------------

// scalarsInput exercises every scalar JSON Schema type plus optionality.
type scalarsInput struct {
	Name    string  `json:"name" jsonschema:"the customer name"`
	Count   int     `json:"count"`
	Ratio   float64 `json:"ratio,omitempty"`
	Enabled bool    `json:"enabled"`
	Note    string  `json:"note,omitzero"`
}

type healthSignal struct {
	Label  string `json:"label"`
	Weight int    `json:"weight,omitempty"`
}

// nestedOutput exercises nested structs and slices.
type nestedOutput struct {
	Summary string         `json:"summary" jsonschema:"a short headline"`
	Score   int            `json:"score"`
	Signals []healthSignal `json:"signals"`
}

// mapContract exercises a string-keyed map at the top level (a valid object).
type mapContract map[string]int

func TestSchemaFor_Scalars(t *testing.T) {
	s, err := codegen.SchemaFor[scalarsInput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	if s.Type != "object" {
		t.Fatalf("top-level type = %q, want object", s.Type)
	}
	if s.AdditionalProperties == nil {
		t.Errorf("struct schema should disallow additionalProperties")
	}
	for _, name := range []string{"name", "count", "ratio", "enabled", "note"} {
		if _, ok := s.Properties[name]; !ok {
			t.Errorf("missing property %q", name)
		}
	}
	// omitempty / omitzero fields are optional; the rest required.
	req := map[string]bool{}
	for _, r := range s.Required {
		req[r] = true
	}
	for _, want := range []string{"name", "count", "enabled"} {
		if !req[want] {
			t.Errorf("%q should be required", want)
		}
	}
	for _, opt := range []string{"ratio", "note"} {
		if req[opt] {
			t.Errorf("%q should be optional", opt)
		}
	}
	if got := s.Properties["name"].Description; got != "the customer name" {
		t.Errorf("name description = %q, want jsonschema tag value", got)
	}
	if got := s.Properties["count"].Type; got != "integer" {
		t.Errorf("count type = %q, want integer", got)
	}
	if got := s.Properties["ratio"].Type; got != "number" {
		t.Errorf("ratio type = %q, want number", got)
	}
	if got := s.Properties["enabled"].Type; got != "boolean" {
		t.Errorf("enabled type = %q, want boolean", got)
	}
}

func TestSchemaFor_NestedAndSlices(t *testing.T) {
	s, err := codegen.SchemaFor[nestedOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	signals, ok := s.Properties["signals"]
	if !ok {
		t.Fatal("missing signals property")
	}
	// A Go slice is nilable, so the engine renders type ["null","array"].
	if !schemaHasType(signals, "array") {
		t.Fatalf("signals type = %v/%q, want array", signals.Types, signals.Type)
	}
	if signals.Items == nil {
		t.Fatal("signals.Items is nil")
	}
	if signals.Items.Type != "object" {
		t.Errorf("signals item type = %q, want object", signals.Items.Type)
	}
	if _, ok := signals.Items.Properties["label"]; !ok {
		t.Errorf("nested signal struct missing label property")
	}
}

func TestSchemaFor_StringKeyedMapIsValid(t *testing.T) {
	s, err := codegen.SchemaFor[mapContract]()
	if err != nil {
		t.Fatalf("SchemaFor[map]: %v", err)
	}
	if s.Type != "object" {
		t.Errorf("map contract type = %q, want object", s.Type)
	}
}

func TestSchemaFor_NonObjectRejected(t *testing.T) {
	cases := []struct {
		name string
		fn   func() error
	}{
		{"string", func() error { _, err := codegen.SchemaFor[string](); return err }},
		{"int", func() error { _, err := codegen.SchemaFor[int](); return err }},
		{"slice", func() error { _, err := codegen.SchemaFor[[]string](); return err }},
		{"intMap", func() error { _, err := codegen.SchemaFor[map[int]string](); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.fn()
			if err == nil {
				t.Fatalf("expected an error for non-object contract %s", c.name)
			}
			if !errors.Is(err, codegen.ErrInvalidContract) {
				t.Errorf("error %v should wrap ErrInvalidContract", err)
			}
		})
	}
}

type badContract struct {
	Ch chan int `json:"ch"`
}

func TestSchemaFor_InvalidFieldTypeRejected(t *testing.T) {
	_, err := codegen.SchemaFor[badContract]()
	if err == nil {
		t.Fatal("expected an error for a channel-typed field")
	}
	if !errors.Is(err, codegen.ErrInvalidContract) {
		t.Errorf("error %v should wrap ErrInvalidContract", err)
	}
}

func TestSchemaForType_NilType(t *testing.T) {
	_, err := codegen.SchemaForType(nil)
	if err == nil || !errors.Is(err, codegen.ErrInvalidContract) {
		t.Fatalf("nil type: got %v, want ErrInvalidContract", err)
	}
}

func TestMarshal_Deterministic(t *testing.T) {
	s, err := codegen.SchemaFor[nestedOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	first, err := codegen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for i := range 20 {
		again, err := codegen.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal iteration %d: %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("Marshal is not deterministic on iteration %d", i)
		}
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Errorf("Marshal output should end with a newline")
	}
	// Property order follows struct field order, not map iteration order.
	out := string(first)
	si, sc := strings.Index(out, `"summary"`), strings.Index(out, `"score"`)
	sg := strings.Index(out, `"signals"`)
	if si >= sc || sc >= sg {
		t.Errorf("property order not stable: summary=%d score=%d signals=%d", si, sc, sg)
	}
}

func TestMarshal_NilSchema(t *testing.T) {
	if _, err := codegen.Marshal(nil); err == nil {
		t.Fatal("Marshal(nil) should error")
	}
}

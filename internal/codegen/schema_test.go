package codegen_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

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

// --- depth-audit fixtures: real Go contract shapes -------------------------

// auditSeverity is a named-constant enum type (finding 3 / D-051).
type auditSeverity string

const (
	auditSeverityInfo  auditSeverity = "info"
	auditSeverityWarn  auditSeverity = "warn"
	auditSeverityError auditSeverity = "error"
)

// shapesContract exercises the Go shapes the depth audit found mishandled:
// time.Time (finding 1), json.RawMessage (finding 2), and a named-constant enum
// (finding 3). It is the schema-side fixture for those findings.
type shapesContract struct {
	When    time.Time       `json:"when" jsonschema:"the event timestamp"`
	Payload json.RawMessage `json:"payload" jsonschema:"arbitrary embedded JSON"`
	Level   auditSeverity   `json:"level" jsonschema:"the event severity"`
	Levels  []auditSeverity `json:"levels,omitempty"`
}

// auditBase is the embedded half of the embedding fixture (finding 4 / D-051).
type auditBase struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// auditEvent embeds auditBase; encoding/json and the schema both inline the
// promoted fields, so the schema fixture has id/kind at the top level.
type auditEvent struct {
	auditBase
	Title string `json:"title"`
}

// auditNode is a recursive (self-referential) contract (finding 5 / D-052).
type auditNode struct {
	Name     string       `json:"name"`
	Children []*auditNode `json:"children,omitempty"`
}

// auditTree is recursive through a non-pointer struct field.
type auditTree struct {
	Label string     `json:"label"`
	Left  *auditTree `json:"left,omitempty"`
	Right *auditTree `json:"right,omitempty"`
}

type recursiveMeta struct {
	ID string `json:"id"`
}
type recursiveSpecial struct {
	*recursiveMeta
	When    *time.Time        `json:"when"`
	Payload *json.RawMessage  `json:"payload"`
	Next    *recursiveSpecial `json:"next,omitempty"`
}

func TestSchemaFor_TimeAndRawMessage(t *testing.T) {
	s, err := codegen.SchemaFor[shapesContract]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	// Finding 1: time.Time keeps its format: date-time qualifier.
	when, ok := s.Properties["when"]
	if !ok {
		t.Fatal("missing when property")
	}
	if when.Type != "string" {
		t.Errorf("when type = %q, want string", when.Type)
	}
	if when.Format != "date-time" {
		t.Errorf("when format = %q, want date-time (finding 1)", when.Format)
	}
	// Finding 2: json.RawMessage is an unconstrained schema, not a byte array.
	payload, ok := s.Properties["payload"]
	if !ok {
		t.Fatal("missing payload property")
	}
	if payload.Type != "" || len(payload.Types) != 0 {
		t.Errorf("payload should be an unconstrained schema, got type %q/%v (finding 2)",
			payload.Type, payload.Types)
	}
	if payload.Items != nil {
		t.Errorf("payload must not render as an array of bytes (finding 2)")
	}
	if payload.Minimum != nil || payload.Maximum != nil {
		t.Errorf("payload must not carry numeric byte bounds (finding 2)")
	}
	// A json.RawMessage field with no `jsonschema` tag marshals to the bare
	// unconstrained `true` schema — proving the value itself is unconstrained.
	type rawOnly struct {
		Payload json.RawMessage `json:"payload"`
	}
	bare, err := codegen.SchemaFor[rawOnly]()
	if err != nil {
		t.Fatalf("SchemaFor[rawOnly]: %v", err)
	}
	out, err := codegen.Marshal(bare)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), `"payload": true`) {
		t.Errorf("an untagged json.RawMessage should marshal as `\"payload\": true`, got:\n%s", out)
	}
}

func TestSchemaFor_EnumWithoutRegistration(t *testing.T) {
	// Without WithEnum the engine cannot see the const set — the property is a
	// plain string with no enum. This pins the gap WithEnum closes.
	s, err := codegen.SchemaFor[shapesContract]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	if level := s.Properties["level"]; len(level.Enum) != 0 {
		t.Errorf("without WithEnum the level property should carry no enum, got %v", level.Enum)
	}
}

func TestSchemaFor_EnumWithRegistration(t *testing.T) {
	// Finding 3: WithEnum stamps the enum array on every property of the type —
	// the scalar field, and the slice's item schema. The enum values are the
	// type's own constant set.
	s, err := codegen.SchemaFor[shapesContract](codegen.WithEnum("auditSeverity",
		string(auditSeverityInfo), string(auditSeverityWarn), string(auditSeverityError)))
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	level, ok := s.Properties["level"]
	if !ok {
		t.Fatal("missing level property")
	}
	if got := enumStrings(level.Enum); !slices.Equal(got, []string{"info", "warn", "error"}) {
		t.Errorf("level enum = %v, want [info warn error] (finding 3)", got)
	}
	if level.Type != "string" {
		t.Errorf("level type = %q, want string (enum is additive)", level.Type)
	}
	levels, ok := s.Properties["levels"]
	if !ok || levels.Items == nil {
		t.Fatal("missing levels slice property or its item schema")
	}
	if got := enumStrings(levels.Items.Enum); !slices.Equal(got, []string{"info", "warn", "error"}) {
		t.Errorf("levels item enum = %v, want the enum stamped on slice items too", got)
	}
}

func TestEnumsFromSource(t *testing.T) {
	// EnumsFromSource discovers the const set from contract source — the seam
	// the generate pipeline uses, since reflection cannot see a const block.
	src := `type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

type EventRecord struct {
	Level Severity ` + "`json:\"level\"`" + `
}
`
	opts, err := codegen.EnumsFromSource(src)
	if err != nil {
		t.Fatalf("EnumsFromSource: %v", err)
	}
	if len(opts) != 1 {
		t.Fatalf("expected 1 discovered enum option, got %d", len(opts))
	}
	// The discovered option applied to a matching contract stamps the enum.
	s, err := codegen.SchemaFor[severityCarrier](opts...)
	if err != nil {
		t.Fatalf("SchemaFor with discovered enum: %v", err)
	}
	if got := enumStrings(s.Properties["level"].Enum); !slices.Equal(got, []string{"info", "warn", "error"}) {
		t.Errorf("discovered enum should be stamped onto the schema, got %v", got)
	}
	// Discovery is deterministic — the option count is stable across calls.
	again, err := codegen.EnumsFromSource(src)
	if err != nil {
		t.Fatalf("EnumsFromSource (again): %v", err)
	}
	if len(again) != len(opts) {
		t.Errorf("EnumsFromSource is not deterministic: %d vs %d", len(opts), len(again))
	}
}

// Severity / severityCarrier mirror the type names parsed by TestEnumsFromSource
// so a discovered enum option matches by type name.
type Severity string

type severityCarrier struct {
	Level Severity `json:"level"`
}

func TestSchemaFor_Embedded(t *testing.T) {
	// Finding 4: an embedded struct's fields are inlined into the schema, just
	// as encoding/json promotes them — no nested `auditBase` property.
	s, err := codegen.SchemaFor[auditEvent]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	for _, name := range []string{"id", "kind", "title"} {
		if _, ok := s.Properties[name]; !ok {
			t.Errorf("embedded fixture missing inlined property %q", name)
		}
	}
	if _, ok := s.Properties["auditBase"]; ok {
		t.Errorf("embedded struct must be inlined, not a named `auditBase` property")
	}
}

func TestSchemaFor_RecursiveReferences(t *testing.T) {
	cases := []struct {
		name string
		fn   func() (*jsonschema.Schema, error)
	}{
		{"pointer-slice recursion", func() (*jsonschema.Schema, error) {
			return codegen.SchemaFor[auditNode]()
		}},
		{"pointer-field recursion", func() (*jsonschema.Schema, error) {
			return codegen.SchemaFor[auditTree]()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := c.fn()
			if err != nil {
				t.Fatalf("recursive schema: %v", err)
			}
			raw, err := codegen.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"$ref": "#/$defs/`) {
				t.Fatalf("schema has no local recursive ref:\n%s", raw)
			}
			if _, err := codegen.ValidateSchema(raw, true); err != nil {
				t.Fatalf("recursive schema is nonconformant: %v", err)
			}
		})
	}
}

func TestOutputSchemaFor_AllJSONKinds(t *testing.T) {
	for _, test := range []struct {
		name string
		fn   func() (*jsonschema.Schema, error)
	}{
		{"string", func() (*jsonschema.Schema, error) { return codegen.OutputSchemaFor[string]() }},
		{"array", func() (*jsonschema.Schema, error) { return codegen.OutputSchemaFor[[]int]() }},
		{"boolean", func() (*jsonschema.Schema, error) { return codegen.OutputSchemaFor[bool]() }},
		{"raw", func() (*jsonschema.Schema, error) { return codegen.OutputSchemaFor[json.RawMessage]() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := codegen.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codegen.ValidateSchema(raw, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecursiveSchemaPreservesSpecialPointersEmbeddingAndInstances(t *testing.T) {
	s, err := codegen.SchemaFor[recursiveSpecial]()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codegen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := codegen.ValidateSchema(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	when := parsed.Properties["when"]
	if !slices.Contains(when.Types, "null") || !slices.Contains(when.Types, "string") || when.Format != "date-time" {
		t.Fatalf("when schema = %#v", when)
	}
	payload := parsed.Properties["payload"]
	if len(payload.AnyOf) != 2 || payload.AnyOf[1].Type != "" || payload.AnyOf[1].Items != nil {
		t.Fatalf("payload schema = %#v", payload)
	}
	for _, required := range parsed.Required {
		if required == "id" {
			t.Fatal("promoted field under nil embedded pointer must be optional")
		}
	}
	resolved, err := parsed.Resolve(&jsonschema.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	good := map[string]any{"when": "2026-07-12T12:00:00Z", "payload": map[string]any{"x": true}, "next": nil}
	if err := resolved.Validate(good); err != nil {
		t.Fatalf("valid instance: %v", err)
	}
	bad := map[string]any{"when": 123, "payload": nil}
	if err := resolved.Validate(bad); err == nil {
		t.Fatal("invalid instance passed")
	}
}

func TestRecursiveEmbeddedFieldConflictMatchesEncodingJSON(t *testing.T) {
	type left struct {
		Value string
	}
	type right struct {
		Value string
	}
	type conflict struct {
		left
		right
		Next *conflict `json:"next,omitempty"`
	}
	s, err := codegen.SchemaFor[conflict]()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Properties["Value"]; ok {
		t.Fatal("ambiguous equally dominant embedded JSON field must be omitted")
	}
}

func TestRecursiveEmbeddedTaggedFieldDominates(t *testing.T) {
	type tagged struct {
		Value string `json:"Value"`
	}
	type plain struct{ Value string }
	type contract struct {
		tagged
		plain
		Next *contract `json:"next,omitempty"`
	}
	s, err := codegen.SchemaFor[contract]()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Properties["Value"]; !ok {
		t.Fatal("tagged same-depth embedded field should dominate untagged field")
	}
}

// enumStrings renders an enum's []any values as []string for comparison.
func enumStrings(vals []any) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		s, ok := v.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}

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

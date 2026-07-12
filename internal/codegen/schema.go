package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// ErrInvalidContract is returned when a Go type cannot be expressed as a
// tool-contract JSON Schema. It wraps the underlying inference failure so
// callers can branch with errors.Is.
var ErrInvalidContract = errors.New("dockyard/internal/codegen: invalid contract type")

// ErrRecursiveContract is retained for callers compiled against the former
// recursion limitation. Recursive contracts now generate local $defs/$ref
// graphs and no new operation returns this error.
var ErrRecursiveContract = fmt.Errorf("%w: recursive (self-referential) contract type", ErrInvalidContract)

// Draft202012 is the only JSON Schema dialect emitted and accepted by Dockyard.
const Draft202012 = "https://json-schema.org/draft/2020-12/schema"

// contractTypeSchemas overrides the inference engine's default translation for
// standard-library types whose default schema is wrong or lossy for a tool
// contract:
//
//   - time.Time — the engine renders it as a bare {"type":"string"}, dropping
//     the `format: date-time` qualifier (D-050). time.Time marshals to an
//     RFC 3339 string, so the contract schema must carry format: date-time.
//   - json.RawMessage — the engine renders it as []byte: a byte array
//     {"type":["null","array"],"items":{integer 0-255}}, an outright wrong
//     schema. json.RawMessage is arbitrary embedded JSON, so the contract
//     schema must be the unconstrained schema `true` (an empty Schema marshals
//     to `true`), which accepts any JSON value (D-050).
//
// The map is rebuilt per call (ForOptions clones the schemas it is given);
// callers must not mutate the returned schemas.
func contractTypeSchemas() map[reflect.Type]*jsonschema.Schema {
	return map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[time.Time]():       {Type: "string", Format: "date-time"},
		reflect.TypeFor[json.RawMessage](): {},
	}
}

// schemaConfig holds the resolved schema-generation options.
type schemaConfig struct {
	// enums maps a named contract type, by its bare Go type name (e.g.
	// "Severity"), to the JSON values of its constant set, so the generator can
	// stamp an `enum` array onto every property of that type.
	enums map[string][]any
}

var registeredEnums sync.Map

// RegisterEnumMetadata installs generated source metadata for runtime schema
// inference. Values are copied and keyed by package-qualified Go type name.
func RegisterEnumMetadata(typeName string, values ...any) {
	if typeName == "" || len(values) == 0 {
		return
	}
	registeredEnums.Store(typeName, append([]any(nil), values...))
}

// SchemaOption configures schema generation.
type SchemaOption func(*schemaConfig)

// WithEnum registers the constant set of a named contract type so the generated
// schema carries an `enum` array for every property of that type (D-051).
//
// typeName is the bare Go type name — "Severity", not "contracts.Severity".
// values are the JSON values of that type's constants.
//
// The inference engine — github.com/google/jsonschema-go — infers a property's
// schema from its Go *type* only: a `type Severity string` field renders as a
// plain {"type":"string"}, and the named type's `const` set is invisible to
// reflection, so the `enum` array is lost. That makes the schema diverge from
// the TypeScript artifact, which tygo *does* emit as a union. WithEnum closes
// the gap: SchemaFor post-processes the schema to attach `enum` to every
// matching property (top-level, nested, slice items, and map values).
//
// The `generate` pipeline discovers these from contract source with
// EnumsFromSource; callers with a static contract may pass them directly:
//
//	codegen.SchemaFor[EventRecord](
//	    codegen.WithEnum("Severity", "info", "warn", "error"))
func WithEnum(typeName string, values ...any) SchemaOption {
	return func(c *schemaConfig) {
		if typeName == "" || len(values) == 0 {
			return
		}
		if c.enums == nil {
			c.enums = make(map[string][]any)
		}
		vs := make([]any, len(values))
		copy(vs, values)
		c.enums[typeName] = vs
	}
}

func resolveSchemaConfig(opts []SchemaOption) schemaConfig {
	var c schemaConfig
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// SchemaFor infers a JSON Schema for the contract type T (RFC §6.1, P1).
//
// T is normally a tool's input or output struct. A tool contract's top-level
// type must be an object — a struct or a string-keyed map — because the MCP
// spec requires tool input/output schemas to have JSON type "object" (RFC §6.3;
// SDK behaviour, runtime/server.AddTool). SchemaFor enforces that and returns an
// error wrapping ErrInvalidContract otherwise, so a misdeclared contract fails
// in Dockyard's own validation rather than at runtime inside a host.
//
// Inference is delegated to github.com/google/jsonschema-go — the same engine
// the official MCP SDK uses (brief 06 §2.3) — so Dockyard emits exactly one
// schema dialect. Property names come from `json` tags; `omitempty`/`omitzero`
// fields are optional, all others required; a `jsonschema` struct tag becomes a
// property description. time.Time carries format: date-time and json.RawMessage
// is an unconstrained schema (D-050). Pass WithEnum to attach an `enum` array
// for a named-constant type (D-051). Recursive contracts use local $defs/$ref.
func SchemaFor[T any](opts ...SchemaOption) (*jsonschema.Schema, error) {
	return SchemaForType(reflect.TypeFor[T](), opts...)
}

// OutputSchemaFor infers an output contract. Unlike tool input, MCP structured
// output may be any JSON value.
func OutputSchemaFor[T any](opts ...SchemaOption) (*jsonschema.Schema, error) {
	return outputSchemaForType(reflect.TypeFor[T](), opts...)
}

// SchemaForType is SchemaFor for a reflect.Type rather than a type parameter.
// It is the seam Phase 06's manifest loader uses to resolve a Go type reference
// named in dockyard.app.yaml to its schema without a compile-time type argument.
func SchemaForType(t reflect.Type, opts ...SchemaOption) (*jsonschema.Schema, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil type", ErrInvalidContract)
	}
	if !isObjectType(t) {
		return nil, fmt.Errorf(
			"%w: %s has JSON type %q, tool contracts must be objects (a struct or string-keyed map)",
			ErrInvalidContract, t, jsonKind(t))
	}
	return outputSchemaForType(t, opts...)
}

func outputSchemaForType(t reflect.Type, opts ...SchemaOption) (*jsonschema.Schema, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil type", ErrInvalidContract)
	}
	cfg := resolveSchemaConfig(opts)
	if recursivePath(t) != "" {
		s, err := recursiveSchema(t, cfg.enums)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	s, err := jsonschema.ForType(t, &jsonschema.ForOptions{TypeSchemas: contractTypeSchemas()})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("%w: %s", ErrInvalidContract, t), err)
	}
	if len(cfg.enums) > 0 {
		applyEnums(t, s, cfg.enums)
	}
	s.Schema = Draft202012
	return s, nil
}

// recursiveSchema owns the local reference graph that jsonschema-go does not
// infer. Only back-edges become references; ordinary fields retain the pinned
// engine's representation.
func recursiveSchema(root reflect.Type, enums map[string][]any) (*jsonschema.Schema, error) {
	defs := make(map[string]*jsonschema.Schema)
	building := make(map[reflect.Type]bool)
	var build func(reflect.Type) (*jsonschema.Schema, error)
	build = func(t reflect.Type) (*jsonschema.Schema, error) {
		if override, ok := contractTypeSchemas()[t]; ok {
			return override.CloneSchemas(), nil
		}
		if t.Kind() == reflect.Pointer {
			inner, err := build(t.Elem())
			if err != nil {
				return nil, err
			}
			return nullableSchema(inner), nil
		}
		if building[t] && t.Name() != "" {
			return &jsonschema.Schema{Ref: "#/$defs/" + escapeJSONPointer(definitionKey(t))}, nil
		}
		if t.Kind() != reflect.Struct && t.Kind() != reflect.Slice && t.Kind() != reflect.Array && t.Kind() != reflect.Map {
			s, err := jsonschema.ForType(t, &jsonschema.ForOptions{TypeSchemas: contractTypeSchemas()})
			if err != nil {
				return nil, err
			}
			if vals := enumValues(enums, t); len(vals) > 0 {
				s.Enum = append([]any(nil), vals...)
			}
			return s, nil
		}
		building[t] = true
		defer delete(building, t)
		s := &jsonschema.Schema{}
		switch t.Kind() {
		case reflect.Struct:
			s.Type = "object"
			s.Properties = make(map[string]*jsonschema.Schema)
			s.AdditionalProperties = &jsonschema.Schema{Not: &jsonschema.Schema{}}
			for _, field := range jsonFields(t) {
				fs, err := build(field.field.Type)
				if err != nil {
					return nil, err
				}
				if d := field.field.Tag.Get("jsonschema"); d != "" {
					fs.Description = d
				}
				s.Properties[field.name] = fs
				s.PropertyOrder = append(s.PropertyOrder, field.name)
				if !jsonFieldOptional(field.field) && !field.nilableAncestor {
					s.Required = append(s.Required, field.name)
				}
			}
		case reflect.Slice, reflect.Array:
			if t.Kind() == reflect.Slice {
				s.Types = []string{"null", "array"}
			} else {
				s.Type = "array"
			}
			item, err := build(t.Elem())
			if err != nil {
				return nil, err
			}
			s.Items = item
		case reflect.Map:
			if t.Key().Kind() != reflect.String {
				return nil, fmt.Errorf("%w: map key %s is not string", ErrInvalidContract, t.Key())
			}
			s.Types = []string{"null", "object"}
			value, err := build(t.Elem())
			if err != nil {
				return nil, err
			}
			s.AdditionalProperties = value
		}
		if t.Name() != "" && t != dereference(root) {
			defs[definitionKey(t)] = s.CloneSchemas()
		}
		return s, nil
	}
	s, err := build(root)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("%w: %s", ErrInvalidContract, root), err)
	}
	// A self-reference needs the root definition as its target.
	rt := dereference(root)
	if rt.Name() != "" {
		defs[definitionKey(rt)] = s.CloneSchemas()
	}
	s.Defs = defs
	s.Schema = Draft202012
	return s, nil
}

func nullableSchema(inner *jsonschema.Schema) *jsonschema.Schema {
	if inner == nil {
		return &jsonschema.Schema{Type: "null"}
	}
	if inner.Ref == "" && len(inner.AnyOf) == 0 && inner.Type != "" {
		out := inner.CloneSchemas()
		out.Types = []string{"null", out.Type}
		out.Type = ""
		return out
	}
	return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "null"}, inner}}
}

func definitionKey(t reflect.Type) string {
	if t.PkgPath() == "" {
		return t.Name()
	}
	return t.PkgPath() + "." + t.Name()
}

func escapeJSONPointer(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

func enumValues(enums map[string][]any, t reflect.Type) []any {
	if values := enums[definitionKey(t)]; len(values) > 0 {
		return values
	}
	if values := enums[t.Name()]; len(values) > 0 {
		return values
	}
	if values, ok := registeredEnums.Load(definitionKey(t)); ok {
		return values.([]any)
	}
	return nil
}

type jsonField struct {
	field           reflect.StructField
	name            string
	depth           int
	tagged          bool
	nilableAncestor bool
	order           int
}

// jsonFields applies encoding/json's embedded-field dominance rules: shallow
// fields win, a tagged field wins over untagged fields at the same depth, and
// an unresolved same-priority conflict is omitted.
func jsonFields(root reflect.Type) []jsonField {
	var candidates []jsonField
	order := 0
	var walk func(reflect.Type, int, bool, map[reflect.Type]bool)
	walk = func(t reflect.Type, depth int, nilable bool, path map[reflect.Type]bool) {
		if path[t] {
			return
		}
		nextPath := make(map[reflect.Type]bool, len(path)+1)
		for typ := range path {
			nextPath[typ] = true
		}
		nextPath[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			name, tagged := jsonTagName(tag)
			if name == "-" || (!f.IsExported() && !f.Anonymous) {
				continue
			}
			ft := f.Type
			pointerEmbedded := f.Anonymous && ft.Kind() == reflect.Pointer
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.Anonymous && !tagged && ft.Kind() == reflect.Struct {
				walk(ft, depth+1, nilable || pointerEmbedded, nextPath)
				continue
			}
			if !f.IsExported() {
				continue
			}
			if !tagged {
				name = f.Name
			}
			candidates = append(candidates, jsonField{field: f, name: name, depth: depth, tagged: tagged, nilableAncestor: nilable, order: order})
			order++
		}
	}
	walk(root, 0, false, nil)
	byName := make(map[string][]jsonField)
	for _, field := range candidates {
		byName[field.name] = append(byName[field.name], field)
	}
	selected := make([]jsonField, 0, len(byName))
	for _, fields := range byName {
		minDepth := fields[0].depth
		for _, field := range fields[1:] {
			if field.depth < minDepth {
				minDepth = field.depth
			}
		}
		var dominant []jsonField
		for _, field := range fields {
			if field.depth == minDepth {
				dominant = append(dominant, field)
			}
		}
		hasTagged := false
		for _, field := range dominant {
			hasTagged = hasTagged || field.tagged
		}
		if hasTagged {
			filtered := dominant[:0]
			for _, field := range dominant {
				if field.tagged {
					filtered = append(filtered, field)
				}
			}
			dominant = filtered
		}
		if len(dominant) == 1 {
			selected = append(selected, dominant[0])
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].order < selected[j].order })
	return selected
}

func jsonTagName(tag string) (string, bool) {
	name := strings.Split(tag, ",")[0]
	return name, name != ""
}

func dereference(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func jsonFieldOptional(f reflect.StructField) bool {
	tag := f.Tag.Get("json")
	return containsTagOption(tag, "omitempty") || containsTagOption(tag, "omitzero")
}

func containsTagOption(tag, option string) bool {
	for _, part := range bytes.Split([]byte(tag), []byte(","))[1:] {
		if string(part) == option {
			return true
		}
	}
	return false
}

// Marshal serializes a schema to indented JSON deterministically: identical
// input always yields byte-identical output. Determinism is what makes
// regeneration safe and golden tests meaningful (brief 06 R1) — a drift in the
// generated schema, or a regression in the upstream inference engine, surfaces
// as a visible diff rather than churn.
//
// The jsonschema.Schema marshaller already renders object properties in struct
// field order via its PropertyOrder field; Marshal re-indents that output with
// two-space indentation and a trailing newline for a stable on-disk form.
func Marshal(s *jsonschema.Schema) ([]byte, error) {
	if s == nil {
		return nil, errors.New("dockyard/internal/codegen: Marshal of nil schema")
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/codegen: marshal schema: %w", err)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, fmt.Errorf("dockyard/internal/codegen: indent schema: %w", err)
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// applyEnums walks a generated schema and stamps an `enum` array onto every
// sub-schema that was inferred from a registered named type. The schema is
// matched to a Go type by walking the contract's reflect.Type and the schema
// graph in lockstep, so a named-constant type is recognised wherever it
// appears — a top-level property, a nested-struct field, a slice element, or a
// map value (D-051).
func applyEnums(t reflect.Type, s *jsonschema.Schema, enums map[string][]any) {
	walkSchema(t, s, enums, make(map[reflect.Type]bool))
}

// walkSchema is the recursive worker for applyEnums. The seen set guards
// against runaway recursion on a type graph; a contract that reaches
// SchemaForType is already cycle-free, but defensive termination keeps the walk
// total.
func walkSchema(t reflect.Type, s *jsonschema.Schema, enums map[string][]any, seen map[reflect.Type]bool) {
	if s == nil || t == nil {
		return
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	// A registered named type: stamp the enum array. enum is independent of
	// `type`, so the existing {"type":"string"} stays and gains an `enum`.
	if vals := enumValues(enums, t); len(vals) > 0 && t.Name() != "" && s.Enum == nil {
		s.Enum = append([]any(nil), vals...)
	}
	if t.Name() != "" {
		if seen[t] {
			return
		}
		seen[t] = true
		defer delete(seen, t)
	}
	switch t.Kind() {
	case reflect.Struct:
		for _, f := range reflect.VisibleFields(t) {
			if f.Anonymous {
				continue // embedded fields are promoted; visited via their own field
			}
			name := jsonFieldName(f)
			if name == "" {
				continue
			}
			if ps, ok := s.Properties[name]; ok {
				walkSchema(f.Type, ps, enums, seen)
			}
		}
	case reflect.Slice, reflect.Array:
		walkSchema(t.Elem(), s.Items, enums, seen)
	case reflect.Map:
		walkSchema(t.Elem(), s.AdditionalProperties, enums, seen)
	default:
	}
}

// jsonFieldName returns the JSON property name of a struct field — the `json`
// tag name when present, otherwise the Go field name — or "" when the field is
// unexported or explicitly omitted with `json:"-"`.
func jsonFieldName(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return f.Name
	}
	name := tag
	if i := indexByte(tag, ','); i >= 0 {
		name = tag[:i]
	}
	switch name {
	case "-":
		return ""
	case "":
		return f.Name
	default:
		return name
	}
}

// indexByte is strings.IndexByte without importing strings for one call.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// recursivePath reports whether t is recursive (contains itself, directly or
// transitively). It returns a human-readable description of the cycle path for
// the error message, or "" when t is acyclic. It mirrors the cycle detection
// inside the inference engine — a depth-first walk over named types — so
// SchemaForType can fail with a specific error before the engine fails with a
// vague one.
func recursivePath(t reflect.Type) string {
	var (
		stack  []string
		onPath = make(map[reflect.Type]bool)
		done   = make(map[reflect.Type]bool)
		cycle  string
	)
	var visit func(reflect.Type) bool
	visit = func(t reflect.Type) bool {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		named := t.Name() != ""
		if named {
			if onPath[t] {
				// Found the back-edge: render the cycle from its first sighting.
				start := 0
				for i, n := range stack {
					if n == t.String() {
						start = i
						break
					}
				}
				cycle = "cycle: " + joinArrow(append(append([]string(nil), stack[start:]...), t.String()))
				return true
			}
			if done[t] {
				return false
			}
			onPath[t] = true
			stack = append(stack, t.String())
		}
		found := false
		switch t.Kind() {
		case reflect.Struct:
			for _, f := range reflect.VisibleFields(t) {
				if f.Anonymous {
					continue
				}
				if visit(f.Type) {
					found = true
					break
				}
			}
		case reflect.Slice, reflect.Array, reflect.Pointer:
			found = visit(t.Elem())
		case reflect.Map:
			found = visit(t.Elem())
		default:
		}
		if named {
			onPath[t] = false
			done[t] = true
			stack = stack[:len(stack)-1]
		}
		return found
	}
	visit(t)
	return cycle
}

// joinArrow joins type names with " → " for a readable cycle path.
func joinArrow(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += " → "
		}
		out += n
	}
	return out
}

// isObjectType reports whether t produces JSON Schema type "object": a struct,
// a string-keyed map, or a pointer/named alias thereof.
func isObjectType(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return true
	case reflect.Map:
		return t.Key().Kind() == reflect.String
	default:
		return false
	}
}

// jsonKind names the JSON Schema type a Go kind maps to, for error messages.
func jsonKind(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		return "string"
	case reflect.Slice, reflect.Array:
		return "array"
	default:
		return t.Kind().String()
	}
}

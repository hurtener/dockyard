package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// ErrSchemaTSDrift is returned when the generated JSON Schema and the generated
// TypeScript for the same contract describe different property sets or
// inconsistent optionality. Design A generates the two artifacts independently
// from Go (RFC §6.2), so a bug in either generator could silently desync them;
// CrossCheck makes that desync a hard failure. Callers branch with errors.Is.
var ErrSchemaTSDrift = errors.New("dockyard/internal/codegen: schema/typescript drift")

// ErrStaleGenerated is returned when a generated artifact on disk no longer
// matches a fresh regeneration from its Go source. Stale generated output is a
// build blocker, never a warning (RFC §6.2, brief 06 R1). Callers branch with
// errors.Is.
var ErrStaleGenerated = errors.New("dockyard/internal/codegen: stale generated output")

// tsField is one property of a parsed TypeScript interface.
type tsField struct {
	name     string
	optional bool
	// kind is the normalised value-type of the field — see typeKind. "" means
	// the type could not be classified (a named or otherwise opaque type), in
	// which case CrossCheck skips the type comparison for that property.
	kind string
}

// Value-type kinds CrossCheck compares. They are deliberately coarse — coarse
// enough to be robust across the two independent generators (jsonschema-go and
// tygo) and their cosmetic noise, sharp enough to catch every value-type
// divergence findings 1–4 produce: a string rendered as a byte array, a
// date-time dropped to a bare string, an enum that lost its union, an embedded
// struct emitted as a nested object instead of inlined.
const (
	kindString  = "string"
	kindNumber  = "number"
	kindBoolean = "boolean"
	kindArray   = "array"
	kindObject  = "object"
	// kindUnknown marks a value with no constrained type — a JSON-Schema `true`
	// schema (e.g. json.RawMessage) or a TS `any`/`unknown`. It compares equal
	// to every kind, so an unconstrained field never reports false drift.
	kindUnknown = "unknown"
)

// CrossCheck verifies the generated JSON Schema and the generated TypeScript for
// one contract agree. It is the drift cross-check at the heart of RFC §6.2 and
// the seam Phase 18's `dockyard validate` calls.
//
// schema is the contract's JSON Schema (from SchemaFor). tsTypeName is the name
// of the TypeScript interface for the same contract — e.g. "ShowRevenueOutput".
// ts is the generated TypeScript (from TypeScriptForSource), normally the whole
// contracts.ts file; CrossCheck locates the named interface within it.
//
// It fails — returning an error wrapping ErrSchemaTSDrift — when:
//   - the schema is not an object schema (a tool contract must be an object);
//   - the TypeScript interface tsTypeName is absent;
//   - a property is present in one artifact and absent in the other;
//   - a property's optionality disagrees (required in the schema but optional in
//     TypeScript, or the reverse);
//   - a property's value-type disagrees — a string in one artifact and a
//     number/array/object in the other (D-051).
//
// CrossCheck compares the property name set, optionality, and a coarse
// value-type kind (string / number / boolean / array / object). It does not
// walk the full type graph — it does not descend into nested objects or array
// element types — but a same-named property whose top-level kind diverges
// between the two artifacts is caught as drift, which is exactly the failure
// mode findings 1–4 of the depth audit produced. A property typed by a named
// type (an enum or a nested interface) or by an unconstrained schema is treated
// as kind-compatible and skipped for the type comparison, so a legitimately
// opaque field never reports false drift.
//
// It expects the default optional style (`field?: T`); generate the TypeScript
// without WithNullOptional for the artifact passed here. WithNullOptional
// renders an optional field as `field: T | null` with no `?` marker, which
// parseTSInterface reads as required — see the documented limitation pinned by
// TestCrossCheck_WithNullOptionalIsMisclassified.
func CrossCheck(schema *jsonschema.Schema, tsTypeName string, ts []byte) error {
	if schema == nil {
		return fmt.Errorf("%w: nil schema for %q", ErrSchemaTSDrift, tsTypeName)
	}
	if !schemaIsObject(schema) {
		return fmt.Errorf("%w: schema for %q is not an object schema", ErrSchemaTSDrift, tsTypeName)
	}

	tsFields, ok := parseTSInterface(string(ts), tsTypeName)
	if !ok {
		return fmt.Errorf("%w: TypeScript interface %q not found in generated output",
			ErrSchemaTSDrift, tsTypeName)
	}

	schemaProps := schemaProperties(schema)
	tsByName := make(map[string]tsField, len(tsFields))
	for _, f := range tsFields {
		tsByName[f.name] = f
	}

	var problems []string

	// Schema property missing from TS, or optionality / value-type mismatch.
	for name, sp := range schemaProps {
		f, present := tsByName[name]
		if !present {
			problems = append(problems,
				fmt.Sprintf("property %q is in the schema but missing from TypeScript", name))
			continue
		}
		// Schema-required ⇒ TS field must be non-optional; schema-optional ⇒ TS
		// field must be optional.
		if sp.required && f.optional {
			problems = append(problems,
				fmt.Sprintf("property %q is required in the schema but optional in TypeScript", name))
		}
		if !sp.required && !f.optional {
			problems = append(problems,
				fmt.Sprintf("property %q is optional in the schema but required in TypeScript", name))
		}
		// Value-type drift: the schema and TypeScript disagree on the property's
		// kind (D-051). An unclassified or unconstrained kind on either side is
		// kind-compatible and skipped.
		if !kindsCompatible(sp.kind, f.kind) {
			problems = append(problems, fmt.Sprintf(
				"property %q has type %s in the schema but %s in TypeScript",
				name, describeKind(sp.kind), describeKind(f.kind)))
		}
	}
	// TS field with no schema property.
	for _, f := range tsFields {
		if _, present := schemaProps[f.name]; !present {
			problems = append(problems,
				fmt.Sprintf("property %q is in TypeScript but missing from the schema", f.name))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%w: %s: %s",
			ErrSchemaTSDrift, tsTypeName, strings.Join(problems, "; "))
	}
	return nil
}

// CheckStale reports whether on-disk generated output is stale versus a fresh
// regeneration. onDisk is the committed generated file (a schema JSON or a
// contracts.ts); fresh is a freshly generated artifact for the same contract
// source. It returns an error wrapping ErrStaleGenerated when the two differ —
// which means the Go source changed without `dockyard generate` being rerun.
//
// The comparison is byte-exact: both Marshal (schema) and TypeScriptForSource
// (TS) are deterministic, so any difference is a real change in the contract
// source, never formatting churn.
func CheckStale(onDisk, fresh []byte) error {
	if bytes.Equal(onDisk, fresh) {
		return nil
	}
	return fmt.Errorf("%w: generated output differs from a fresh regeneration "+
		"(%d on-disk bytes vs %d regenerated) — rerun `dockyard generate`",
		ErrStaleGenerated, len(onDisk), len(fresh))
}

// schemaIsObject reports whether s declares JSON Schema type "object", via
// either the single Type field or the multi-valued Types field.
func schemaIsObject(s *jsonschema.Schema) bool {
	if s.Type == "object" {
		return true
	}
	return slices.Contains(s.Types, "object")
}

// schemaProp is one top-level property of a contract's JSON Schema, reduced to
// the facets CrossCheck compares.
type schemaProp struct {
	required bool
	kind     string
}

// schemaProperties returns the contract's top-level properties keyed by name. A
// property is required when it appears in the schema's Required list; its kind
// is the normalised value-type (see schemaKind).
func schemaProperties(s *jsonschema.Schema) map[string]schemaProp {
	required := make(map[string]struct{}, len(s.Required))
	for _, r := range s.Required {
		required[r] = struct{}{}
	}
	props := make(map[string]schemaProp, len(s.Properties))
	for name, ps := range s.Properties {
		_, isRequired := required[name]
		props[name] = schemaProp{required: isRequired, kind: schemaKind(ps)}
	}
	return props
}

// schemaKind classifies a property sub-schema into a coarse value-type kind.
//
// A nilable Go type renders as a two-element type set ["null", <type>]; the
// "null" member is the optionality marker, not the value type, so it is
// ignored. A schema with an `enum` but no usable type still has a value type —
// a string-enum is kindString. A schema with no type at all is kindUnknown: the
// unconstrained `true` schema json.RawMessage produces, which is compatible
// with anything.
func schemaKind(s *jsonschema.Schema) string {
	if s == nil {
		return kindUnknown
	}
	t := s.Type
	if t == "" {
		for _, candidate := range s.Types {
			if candidate != "null" {
				t = candidate
				break
			}
		}
	}
	switch t {
	case "string":
		return kindString
	case "integer", "number":
		return kindNumber
	case "boolean":
		return kindBoolean
	case "array":
		return kindArray
	case "object":
		return kindObject
	default:
		// No type keyword. An enum still pins a value type when its members are
		// homogeneous; otherwise the schema is genuinely unconstrained.
		if len(s.Enum) > 0 {
			return enumKind(s.Enum)
		}
		return kindUnknown
	}
}

// enumKind returns the value-type kind shared by every member of an enum, or
// kindUnknown when the members are heterogeneous or empty.
func enumKind(members []any) string {
	kind := ""
	for _, m := range members {
		var k string
		switch m.(type) {
		case string:
			k = kindString
		case bool:
			k = kindBoolean
		case float64, int, int64, json.Number:
			k = kindNumber
		default:
			return kindUnknown
		}
		if kind == "" {
			kind = k
		} else if kind != k {
			return kindUnknown
		}
	}
	if kind == "" {
		return kindUnknown
	}
	return kind
}

// kindsCompatible reports whether a schema kind and a TypeScript kind agree.
// kindUnknown — an unconstrained schema or an opaque TS type — is compatible
// with every kind, so a legitimately opaque field never reports false drift.
func kindsCompatible(a, b string) bool {
	if a == "" || b == "" || a == kindUnknown || b == kindUnknown {
		return true
	}
	return a == b
}

// describeKind renders a kind for an error message.
func describeKind(k string) string {
	if k == "" || k == kindUnknown {
		return "an unconstrained/opaque type"
	}
	return k
}

// tsInterfaceRe matches the opening line of a TypeScript interface declaration,
// capturing its name.
var tsInterfaceRe = regexp.MustCompile(`(?m)^\s*export\s+interface\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:extends\s+[^{]+)?\{`)

// tsFieldRe matches one field line inside a TypeScript interface body,
// capturing the field name, the optional `?` marker, and the type expression
// up to the trailing semicolon.
var tsFieldRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)(\??)\s*:\s*(.+?);?\s*$`)

// tsLineCommentRe strips a tygo `/* ... */` annotation (e.g. `number /* int */`)
// from a TypeScript type expression before it is classified.
var tsLineCommentRe = regexp.MustCompile(`/\*.*?\*/`)

// tsTypeKind classifies a TypeScript type expression into a coarse value-type
// kind, matching schemaKind's vocabulary so the two can be compared.
//
// It strips tygo's `/* int */` annotations and any `| null` / `| undefined`
// optionality member, then recognises the boring, generated shapes Dockyard
// emits: a primitive, a `T[]` array, a `{ [key: ...]: ... }` index object, or a
// named type. A named type (an enum or a nested interface) and `any`/`unknown`
// classify as kindUnknown — kind-compatible with anything — because CrossCheck
// deliberately does not resolve a named TS type back to its declaration.
func tsTypeKind(expr string) string {
	t := tsLineCommentRe.ReplaceAllString(expr, "")
	t = strings.TrimSpace(t)
	t = strings.TrimSuffix(t, ";")
	t = strings.TrimSpace(t)

	// Drop `| null` / `| undefined` optionality members of a top-level union.
	if strings.Contains(t, "|") {
		var kept []string
		for _, part := range strings.Split(t, "|") {
			p := strings.TrimSpace(part)
			if p == "null" || p == "undefined" || p == "" {
				continue
			}
			kept = append(kept, p)
		}
		if len(kept) == 1 {
			t = kept[0]
		} else {
			// A genuine multi-member union — not a shape CrossCheck classifies.
			return kindUnknown
		}
	}
	t = strings.TrimSpace(t)

	switch {
	case t == "":
		return kindUnknown
	case t == "string":
		return kindString
	case t == "number":
		return kindNumber
	case t == "boolean":
		return kindBoolean
	case t == "any" || t == "unknown":
		return kindUnknown
	case strings.HasSuffix(t, "[]"):
		return kindArray
	case strings.HasPrefix(t, "Array<"):
		return kindArray
	case strings.HasPrefix(t, "{"):
		return kindObject
	default:
		// A named type — an enum or a nested interface. CrossCheck does not
		// resolve it, so it is treated as kind-compatible.
		return kindUnknown
	}
}

// parseTSInterface extracts the fields of the named interface from generated
// TypeScript. It returns the fields and true when the interface is found.
//
// The parse is line-oriented and deliberately small: the input is Dockyard's
// own boring, generated TypeScript (one field per line, no inline object
// types), not arbitrary TypeScript. A field carrying a `?` is optional.
func parseTSInterface(ts, name string) ([]tsField, bool) {
	lines := strings.Split(ts, "\n")
	for i := 0; i < len(lines); i++ {
		m := tsInterfaceRe.FindStringSubmatch(lines[i])
		if m == nil || m[1] != name {
			continue
		}
		var fields []tsField
		for j := i + 1; j < len(lines); j++ {
			line := lines[j]
			if strings.Contains(line, "}") && !strings.Contains(line, "{") {
				return fields, true
			}
			fm := tsFieldRe.FindStringSubmatch(line)
			if fm == nil {
				continue // blank line, comment, or continuation
			}
			fields = append(fields, tsField{
				name:     fm[1],
				optional: fm[2] == "?",
				kind:     tsTypeKind(fm[3]),
			})
		}
		return fields, true // interface ran to EOF without a close brace
	}
	return nil, false
}

package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
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
	// schema (e.g. json.RawMessage) or a TS `any`/`unknown`. An unconstrained
	// schema accepts every TS kind, but TS any cannot mask a constrained schema.
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
// between the two artifacts is caught as drift. Named generated interfaces and
// aliases are resolved to their declaration kind; `any`/`unknown` cannot mask a
// constrained schema. An unconstrained schema remains compatible with every TS
// kind. Base64 schema strings must be explicit TypeScript strings.
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
	if !schemaIsObject(schema, schema, make(map[*jsonschema.Schema]bool)) {
		return fmt.Errorf("%w: schema for %q is not an object schema", ErrSchemaTSDrift, tsTypeName)
	}

	tsFields, ok := parseTSInterface(string(ts), tsTypeName)
	if !ok {
		return fmt.Errorf("%w: TypeScript interface %q not found in generated output",
			ErrSchemaTSDrift, tsTypeName)
	}

	schemaProps := schemaProperties(schema, schema, make(map[*jsonschema.Schema]bool))
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
		if sp.base64 && f.kind != kindString {
			problems = append(problems, fmt.Sprintf(
				"property %q is base64 in the schema but %s in TypeScript",
				name, describeKind(f.kind)))
		} else if !kindsCompatible(sp.kind, f.kind) {
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
func schemaIsObject(root, s *jsonschema.Schema, seen map[*jsonschema.Schema]bool) bool {
	if s == nil || seen[s] {
		return false
	}
	seen[s] = true
	if s.Type == "object" {
		return true
	}
	if slices.Contains(s.Types, "object") {
		return true
	}
	if target := localRef(root, s.Ref); target != nil {
		return schemaIsObject(root, target, seen)
	}
	for _, branch := range s.AllOf {
		if schemaIsObject(root, branch, seen) {
			return true
		}
	}
	return false
}

// schemaProp is one top-level property of a contract's JSON Schema, reduced to
// the facets CrossCheck compares.
type schemaProp struct {
	required bool
	kind     string
	base64   bool
}

// schemaProperties returns the contract's top-level properties keyed by name. A
// property is required when it appears in the schema's Required list; its kind
// is the normalised value-type (see schemaKind). Base64 encoding is retained so
// byte-slice wire types cannot evade comparison as opaque named TypeScript.
func schemaProperties(root, s *jsonschema.Schema, seen map[*jsonschema.Schema]bool) map[string]schemaProp {
	if s == nil || seen[s] {
		return map[string]schemaProp{}
	}
	seen[s] = true
	props := map[string]schemaProp{}
	if target := localRef(root, s.Ref); target != nil {
		for name, prop := range schemaProperties(root, target, seen) {
			props[name] = prop
		}
	}
	for _, branch := range s.AllOf {
		for name, prop := range schemaProperties(root, branch, seen) {
			if old, ok := props[name]; ok {
				prop.required = prop.required || old.required
			}
			props[name] = prop
		}
	}
	required := make(map[string]struct{}, len(s.Required))
	for _, r := range s.Required {
		required[r] = struct{}{}
	}
	for name, ps := range s.Properties {
		_, isRequired := required[name]
		props[name] = schemaProp{
			required: isRequired,
			kind:     schemaKind(root, ps, make(map[*jsonschema.Schema]bool)),
			base64:   ps != nil && ps.ContentEncoding == "base64",
		}
	}
	return props
}

func localRef(root *jsonschema.Schema, ref string) *jsonschema.Schema {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return nil
	}
	key := strings.TrimPrefix(ref, prefix)
	key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
	return root.Defs[key]
}

// schemaKind classifies a property sub-schema into a coarse value-type kind.
//
// A nilable Go type renders as a two-element type set ["null", <type>]; the
// "null" member is the optionality marker, not the value type, so it is
// ignored. A schema with an `enum` but no usable type still has a value type —
// a string-enum is kindString. A schema with no type at all is kindUnknown: the
// unconstrained `true` schema json.RawMessage produces, which is compatible
// with anything.
func schemaKind(root, s *jsonschema.Schema, seen map[*jsonschema.Schema]bool) string {
	if s == nil || seen[s] {
		return kindUnknown
	}
	seen[s] = true
	if target := localRef(root, s.Ref); target != nil {
		return schemaKind(root, target, seen)
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
		for _, branches := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf} {
			kind := ""
			for _, branch := range branches {
				if schemaOnlyNull(branch) {
					continue
				}
				branchKind := schemaKind(root, branch, cloneSchemaPath(seen))
				if branchKind == kindUnknown || (kind != "" && kind != branchKind) {
					kind = kindUnknown
					break
				}
				kind = branchKind
			}
			if kind != "" {
				return kind
			}
		}
		// No type keyword. An enum still pins a value type when its members are
		// homogeneous; otherwise the schema is genuinely unconstrained.
		if len(s.Enum) > 0 {
			return enumKind(s.Enum)
		}
		return kindUnknown
	}
}

func schemaOnlyNull(s *jsonschema.Schema) bool {
	return s != nil && (s.Type == "null" || len(s.Types) == 1 && s.Types[0] == "null")
}

func cloneSchemaPath(path map[*jsonschema.Schema]bool) map[*jsonschema.Schema]bool {
	out := make(map[*jsonschema.Schema]bool, len(path))
	for schema := range path {
		out[schema] = true
	}
	return out
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
// An unconstrained schema is compatible with every TypeScript kind. The inverse
// is intentionally false: TypeScript any/unknown cannot hide a constrained
// schema kind.
func kindsCompatible(a, b string) bool {
	if a == "" || b == "" || a == kindUnknown {
		return true
	}
	if b == kindUnknown {
		return false
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

// tsFieldRe matches one field line inside a TypeScript interface body. Tygo
// quotes property names that are not TypeScript identifiers, including the
// literal JSON property "-".
var tsFieldRe = regexp.MustCompile(`^\s*(?:([A-Za-z_][A-Za-z0-9_]*)|'([^']*)'|"([^"]*)")(\??)\s*:\s*(.+?);?\s*$`)

// tsLineCommentRe strips a tygo `/* ... */` annotation (e.g. `number /* int */`)
// from a TypeScript type expression before it is classified.
var tsLineCommentRe = regexp.MustCompile(`/\*.*?\*/`)

func tsTypeKindWithSource(expr, source string, seen map[string]bool) string {
	t := tsLineCommentRe.ReplaceAllString(expr, "")
	t = strings.TrimSpace(t)
	t = strings.TrimSuffix(t, ";")
	t = strings.TrimSpace(t)
	if strings.HasSuffix(t, "[]") || strings.HasPrefix(t, "Array<") {
		return kindArray
	}

	// Drop `| null` / `| undefined` optionality members of a top-level union.
	if strings.Contains(t, "|") {
		var kept []string
		unionKind := ""
		for _, part := range strings.Split(t, "|") {
			p := strings.TrimSpace(part)
			if p == "null" || p == "undefined" || p == "" {
				continue
			}
			kept = append(kept, p)
			partKind := tsLiteralKind(p, source)
			if partKind == kindUnknown || (unionKind != "" && unionKind != partKind) {
				unionKind = kindUnknown
			} else if unionKind == "" {
				unionKind = partKind
			}
		}
		if len(kept) == 1 {
			t = kept[0]
		} else if unionKind != "" && unionKind != kindUnknown {
			return unionKind
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
	case strings.HasPrefix(t, "{"):
		return kindObject
	default:
		if source != "" && regexp.MustCompile(`(?m)^\s*export\s+interface\s+`+regexp.QuoteMeta(t)+`\b`).MatchString(source) {
			return kindObject
		}
		if source != "" && !seen[t] {
			alias := regexp.MustCompile(`(?m)^\s*export\s+type\s+` + regexp.QuoteMeta(t) + `\s*=\s*([^;]+);`).FindStringSubmatch(source)
			if alias != nil {
				next := make(map[string]bool, len(seen)+1)
				for name := range seen {
					next[name] = true
				}
				next[t] = true
				return tsTypeKindWithSource(alias[1], source, next)
			}
		}
		return kindUnknown
	}
}

func tsLiteralKind(expr, source string) string {
	if strings.HasPrefix(expr, "typeof ") && source != "" {
		name := strings.TrimSpace(strings.TrimPrefix(expr, "typeof "))
		constant := regexp.MustCompile(`(?m)^\s*export\s+const\s+` + regexp.QuoteMeta(name) + `\s*=\s*([^;]+);`).FindStringSubmatch(source)
		if constant != nil {
			return tsLiteralKind(strings.TrimSpace(constant[1]), "")
		}
	}
	if len(expr) >= 2 && ((expr[0] == '\'' && expr[len(expr)-1] == '\'') || (expr[0] == '"' && expr[len(expr)-1] == '"')) {
		return kindString
	}
	if expr == "true" || expr == "false" {
		return kindBoolean
	}
	if _, err := strconv.ParseFloat(expr, 64); err == nil {
		return kindNumber
	}
	return kindUnknown
}

// parseTSInterface extracts the fields of the named interface from generated
// TypeScript. It returns the fields and true when the interface is found.
//
// The parse is line-oriented and deliberately small: the input is Dockyard's
// own boring, generated TypeScript (one field per line), not arbitrary
// TypeScript. A field carrying a `?` is optional.
//
// JSDoc block comments (`/** … */`) are skipped: a Go doc comment can contain an
// example object literal whose `}` lands on a comment line with no matching `{`,
// and that brace must NOT be mistaken for the interface's closing brace — doing
// so truncated the field list and reported a real field as "missing from
// TypeScript" (pinned by TestCrossCheck_DocCommentBraceNotInterfaceClose).
func parseTSInterface(ts, name string) ([]tsField, bool) {
	lines := strings.Split(ts, "\n")
	for i := 0; i < len(lines); i++ {
		m := tsInterfaceRe.FindStringSubmatch(lines[i])
		if m == nil || m[1] != name {
			continue
		}
		var fields []tsField
		depth := braceDelta(lines[i])
		inComment := false
		var pending *tsField
		var pendingType strings.Builder
		for j := i + 1; j < len(lines); j++ {
			line := stripTSLineComment(lines[j])
			// Inside a block comment: skip until it closes. The braces in a
			// JSDoc example are comment text, not structure.
			if inComment {
				if strings.Contains(line, "*/") {
					inComment = false
				}
				continue
			}
			// A block comment that opens here but does not close on this line.
			if open := strings.Index(line, "/*"); open >= 0 &&
				!strings.Contains(line[open+2:], "*/") {
				inComment = true
				continue
			}
			// Strip any single-line `/* … */` so its braces don't confuse the
			// close-brace detection.
			clean := tsLineCommentRe.ReplaceAllString(line, "")
			if pending != nil {
				pendingType.WriteByte(' ')
				pendingType.WriteString(strings.TrimSpace(clean))
			} else if depth == 1 {
				fm := tsFieldRe.FindStringSubmatch(clean)
				if fm != nil {
					fieldName := fm[1]
					if fieldName == "" {
						fieldName = fm[2]
					}
					if fieldName == "" {
						fieldName = fm[3]
					}
					field := tsField{
						name:     fieldName,
						optional: fm[4] == "?",
					}
					if braceDelta(clean) > 0 {
						pending = &field
						pendingType.WriteString(fm[5])
					} else {
						field.kind = tsTypeKindWithSource(fm[5], ts, make(map[string]bool))
						fields = append(fields, field)
					}
				}
			}
			depth += braceDelta(clean)
			if pending != nil && depth == 1 {
				pending.kind = tsTypeKindWithSource(pendingType.String(), ts, make(map[string]bool))
				fields = append(fields, *pending)
				pending = nil
				pendingType.Reset()
			}
			if depth <= 0 {
				return fields, true
			}
		}
		return fields, true // interface ran to EOF without a close brace
	}
	return nil, false
}

// stripTSLineComment removes a TypeScript line comment without treating `//`
// inside string or template literals as a comment opener.
func stripTSLineComment(line string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		b := line[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
				continue
			}
			if b == quote {
				quote = 0
			}
			continue
		}
		switch b {
		case '\'', '"', '`':
			quote = b
		case '/':
			if i+1 < len(line) && line[i+1] == '/' {
				return line[:i]
			}
		}
	}
	return line
}

// braceDelta counts structural braces in generated TypeScript while ignoring
// braces inside quoted property names and literal types.
func braceDelta(line string) int {
	delta := 0
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		b := line[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
				continue
			}
			if b == quote {
				quote = 0
			}
			continue
		}
		switch b {
		case '\'', '"', '`':
			quote = b
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}

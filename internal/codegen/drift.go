package codegen

import (
	"bytes"
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
}

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
//     TypeScript, or the reverse).
//
// CrossCheck compares the property name set and optionality, not the full type
// graph: both artifacts derive from the same Go field, so a value-type
// divergence would be a generator bug, and the golden tests pin each generator
// independently (see D-034). It expects the default optional style (`field?:
// T`); generate the TypeScript without WithNullOptional for the artifact passed
// here.
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

	// Schema property missing from TS, or optionality mismatch.
	for name, required := range schemaProps {
		f, present := tsByName[name]
		if !present {
			problems = append(problems,
				fmt.Sprintf("property %q is in the schema but missing from TypeScript", name))
			continue
		}
		// Schema-required ⇒ TS field must be non-optional; schema-optional ⇒ TS
		// field must be optional.
		if required && f.optional {
			problems = append(problems,
				fmt.Sprintf("property %q is required in the schema but optional in TypeScript", name))
		}
		if !required && !f.optional {
			problems = append(problems,
				fmt.Sprintf("property %q is optional in the schema but required in TypeScript", name))
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

// schemaProperties returns the contract's top-level properties as a map from
// property name to whether it is required. A property is required when it
// appears in the schema's Required list.
func schemaProperties(s *jsonschema.Schema) map[string]bool {
	required := make(map[string]struct{}, len(s.Required))
	for _, r := range s.Required {
		required[r] = struct{}{}
	}
	props := make(map[string]bool, len(s.Properties))
	for name := range s.Properties {
		_, isRequired := required[name]
		props[name] = isRequired
	}
	return props
}

// tsInterfaceRe matches the opening line of a TypeScript interface declaration,
// capturing its name.
var tsInterfaceRe = regexp.MustCompile(`(?m)^\s*export\s+interface\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:extends\s+[^{]+)?\{`)

// tsFieldRe matches one field line inside a TypeScript interface body,
// capturing the field name and the optional `?` marker.
var tsFieldRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)(\??)\s*:`)

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
			fields = append(fields, tsField{name: fm[1], optional: fm[2] == "?"})
		}
		return fields, true // interface ran to EOF without a close brace
	}
	return nil, false
}

package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// ErrInvalidContract is returned when a Go type cannot be expressed as a
// tool-contract JSON Schema. It wraps the underlying inference failure so
// callers can branch with errors.Is.
var ErrInvalidContract = errors.New("dockyard/internal/codegen: invalid contract type")

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
// property description.
func SchemaFor[T any]() (*jsonschema.Schema, error) {
	return SchemaForType(reflect.TypeFor[T]())
}

// SchemaForType is SchemaFor for a reflect.Type rather than a type parameter.
// It is the seam Phase 06's manifest loader uses to resolve a Go type reference
// named in dockyard.app.yaml to its schema without a compile-time type argument.
func SchemaForType(t reflect.Type) (*jsonschema.Schema, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil type", ErrInvalidContract)
	}
	if !isObjectType(t) {
		return nil, fmt.Errorf(
			"%w: %s has JSON type %q, tool contracts must be objects (a struct or string-keyed map)",
			ErrInvalidContract, t, jsonKind(t))
	}
	s, err := jsonschema.ForType(t, nil)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("%w: %s", ErrInvalidContract, t), err)
	}
	return s, nil
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

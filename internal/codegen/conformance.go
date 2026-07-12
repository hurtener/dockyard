package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// ErrNonconformantSchema identifies schemas rejected by Dockyard's policy.
var ErrNonconformantSchema = errors.New("dockyard/internal/codegen: nonconformant JSON Schema")

type scanFrame struct {
	object, expectKey bool
	key               string
}

const (
	maxSchemaBytes = 4 << 20
	maxSchemaDepth = 128
	maxSchemaNodes = 100000
	maxRefWork     = 1000000
)

// ValidateSchema applies Dockyard's bounded, local-only 2020-12 policy.
func ValidateSchema(raw []byte, requireObject bool) (*jsonschema.Schema, error) {
	if len(raw) > maxSchemaBytes {
		return nil, fmt.Errorf("%w: schema exceeds %d bytes", ErrNonconformantSchema, maxSchemaBytes)
	}
	_, _, err := inspectSchema(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNonconformantSchema, err)
	}
	if s.Schema != Draft202012 {
		return nil, fmt.Errorf("%w: $schema must be %q", ErrNonconformantSchema, Draft202012)
	}
	schemaNodes, refs, err := inspectLocalRefs(&s, 0, make(map[*jsonschema.Schema]bool))
	if err != nil {
		return nil, err
	}
	if refs > 0 && schemaNodes > maxRefWork/refs {
		return nil, fmt.Errorf("%w: local reference work exceeds %d", ErrNonconformantSchema, maxRefWork)
	}
	if _, err := s.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true}); err != nil {
		return nil, fmt.Errorf("%w: schema does not resolve: %w", ErrNonconformantSchema, err)
	}
	if requireObject && !resolvedRootIsObject(&s) {
		return nil, fmt.Errorf("%w: tool input root must have type object", ErrNonconformantSchema)
	}
	return &s, nil
}

func inspectSchema(r *bytes.Reader) (nodes, refs int, err error) {
	dec := json.NewDecoder(r)
	stack := make([]scanFrame, 0, 16)
	for {
		tok, tokenErr := dec.Token()
		if errors.Is(tokenErr, io.EOF) {
			break
		}
		if tokenErr != nil {
			return 0, 0, fmt.Errorf("%w: invalid JSON: %w", ErrNonconformantSchema, tokenErr)
		}
		nodes++
		if nodes > maxSchemaNodes {
			return 0, 0, fmt.Errorf("%w: schema exceeds %d nodes", ErrNonconformantSchema, maxSchemaNodes)
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{':
				stack = append(stack, scanFrame{object: true, expectKey: true})
			case '[':
				stack = append(stack, scanFrame{})
			case '}', ']':
				if len(stack) == 0 {
					return 0, 0, fmt.Errorf("%w: unbalanced JSON", ErrNonconformantSchema)
				}
				stack = stack[:len(stack)-1]
				consumeParent(stack)
			}
			if len(stack) > maxSchemaDepth {
				return 0, 0, fmt.Errorf("%w: schema exceeds depth %d", ErrNonconformantSchema, maxSchemaDepth)
			}
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey {
			key, ok := tok.(string)
			if !ok {
				return 0, 0, fmt.Errorf("%w: object key is not a string", ErrNonconformantSchema)
			}
			stack[len(stack)-1].key, stack[len(stack)-1].expectKey = key, false
			continue
		}
		consumeParent(stack)
	}
	if len(stack) != 0 {
		return 0, 0, fmt.Errorf("%w: incomplete JSON", ErrNonconformantSchema)
	}
	return nodes, refs, nil
}

func inspectLocalRefs(s *jsonschema.Schema, depth int, seen map[*jsonschema.Schema]bool) (nodes, refs int, err error) {
	if s == nil || seen[s] {
		return 0, 0, nil
	}
	if depth > maxSchemaDepth {
		return 0, 0, fmt.Errorf("%w: schema exceeds depth %d", ErrNonconformantSchema, maxSchemaDepth)
	}
	seen[s] = true
	for keyword, ref := range map[string]string{"$ref": s.Ref, "$dynamicRef": s.DynamicRef} {
		if ref != "" {
			if !strings.HasPrefix(ref, "#") {
				return 0, 0, fmt.Errorf("%w: external %s %q is forbidden", ErrNonconformantSchema, keyword, ref)
			}
			refs++
		}
	}
	nodes = 1
	v := reflect.ValueOf(s).Elem()
	schemaPtr := reflect.TypeFor[*jsonschema.Schema]()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		switch {
		case field.Type() == schemaPtr:
			n, r, e := inspectLocalRefs(field.Interface().(*jsonschema.Schema), depth+1, seen)
			nodes += n
			refs += r
			if e != nil {
				return 0, 0, e
			}
		case field.Kind() == reflect.Slice && field.Type().Elem() == schemaPtr:
			for j := 0; j < field.Len(); j++ {
				n, r, e := inspectLocalRefs(field.Index(j).Interface().(*jsonschema.Schema), depth+1, seen)
				nodes += n
				refs += r
				if e != nil {
					return 0, 0, e
				}
			}
		case field.Kind() == reflect.Map && field.Type().Elem() == schemaPtr:
			keys := field.MapKeys()
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			for _, key := range keys {
				n, r, e := inspectLocalRefs(field.MapIndex(key).Interface().(*jsonschema.Schema), depth+1, seen)
				nodes += n
				refs += r
				if e != nil {
					return 0, 0, e
				}
			}
		}
		if nodes > maxSchemaNodes {
			return 0, 0, fmt.Errorf("%w: schema exceeds %d nodes", ErrNonconformantSchema, maxSchemaNodes)
		}
	}
	return nodes, refs, nil
}

func consumeParent(stack []scanFrame) {
	if len(stack) > 0 && stack[len(stack)-1].object {
		stack[len(stack)-1].expectKey = true
	}
}

func resolvedRootIsObject(s *jsonschema.Schema) bool {
	if s.Type == "object" {
		return true
	}
	for _, typ := range s.Types {
		if typ == "object" {
			return true
		}
	}
	if strings.HasPrefix(s.Ref, "#/$defs/") {
		return resolvedRootIsObject(s.Defs[strings.TrimPrefix(s.Ref, "#/$defs/")])
	}
	if len(s.AllOf) > 0 {
		for _, branch := range s.AllOf {
			if resolvedRootIsObject(branch) {
				return true
			}
		}
	}
	if len(s.AnyOf) > 0 {
		for _, branch := range s.AnyOf {
			if !resolvedRootIsObject(branch) {
				return false
			}
		}
		return true
	}
	if len(s.OneOf) > 0 {
		for _, branch := range s.OneOf {
			if !resolvedRootIsObject(branch) {
				return false
			}
		}
		return true
	}
	return false
}

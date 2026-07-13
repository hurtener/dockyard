package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"sort"
	"strconv"
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
			if !validLocalSchemaRef(ref) {
				return 0, 0, fmt.Errorf("%w: %s %q contains an invalid RFC 6901 pointer", ErrNonconformantSchema, keyword, ref)
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
	return resolvedSchemaIsObject(s, s, make(map[*jsonschema.Schema]bool), 0)
}

func resolvedSchemaIsObject(root, s *jsonschema.Schema, path map[*jsonschema.Schema]bool, depth int) bool {
	if s == nil || path[s] || depth > maxSchemaDepth {
		return false
	}
	path[s] = true
	defer delete(path, s)
	if s.Type != "" {
		return s.Type == "object"
	}
	if len(s.Types) > 0 {
		return len(s.Types) == 1 && s.Types[0] == "object"
	}
	ref := s.Ref
	if ref == "" {
		ref = s.DynamicRef
	}
	if ref != "" {
		target, ok := resolveLocalSchemaRef(root, ref)
		if !ok {
			return false
		}
		return resolvedSchemaIsObject(root, target, path, depth+1)
	}
	if len(s.AllOf) > 0 {
		for _, branch := range s.AllOf {
			if resolvedSchemaIsObject(root, branch, path, depth+1) {
				return true
			}
		}
	}
	if len(s.AnyOf) > 0 {
		for _, branch := range s.AnyOf {
			if !resolvedSchemaIsObject(root, branch, path, depth+1) {
				return false
			}
		}
		return true
	}
	if len(s.OneOf) > 0 {
		for _, branch := range s.OneOf {
			if !resolvedSchemaIsObject(root, branch, path, depth+1) {
				return false
			}
		}
		return true
	}
	return false
}

func resolveLocalSchemaRef(root *jsonschema.Schema, ref string) (*jsonschema.Schema, bool) {
	if root == nil || !strings.HasPrefix(ref, "#") {
		return nil, false
	}
	fragment, err := url.PathUnescape(strings.TrimPrefix(ref, "#"))
	if err != nil {
		return nil, false
	}
	if fragment == "" {
		return root, true
	}
	if !strings.HasPrefix(fragment, "/") {
		return findSchemaAnchor(root, fragment, make(map[*jsonschema.Schema]bool), 0)
	}
	tokens := strings.Split(strings.TrimPrefix(fragment, "/"), "/")
	for i := range tokens {
		if !validJSONPointerToken(tokens[i]) {
			return nil, false
		}
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(tokens[i], "~1", "/"), "~0", "~")
	}
	return schemaAtPointer(root, tokens, 0)
}

func validLocalSchemaRef(ref string) bool {
	fragment, err := url.PathUnescape(strings.TrimPrefix(ref, "#"))
	if err != nil || fragment == "" || !strings.HasPrefix(fragment, "/") {
		return err == nil
	}
	for _, token := range strings.Split(strings.TrimPrefix(fragment, "/"), "/") {
		if !validJSONPointerToken(token) {
			return false
		}
	}
	return true
}

func validJSONPointerToken(token string) bool {
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			continue
		}
		if i+1 >= len(token) || token[i+1] != '0' && token[i+1] != '1' {
			return false
		}
		i++
	}
	return true
}

func schemaAtPointer(current *jsonschema.Schema, tokens []string, depth int) (*jsonschema.Schema, bool) {
	if current == nil || depth > maxSchemaDepth {
		return nil, false
	}
	if len(tokens) == 0 {
		return current, true
	}
	if len(tokens) == 1 {
		var next *jsonschema.Schema
		switch tokens[0] {
		case "items":
			next = current.Items
		case "additionalItems":
			next = current.AdditionalItems
		case "contains":
			next = current.Contains
		case "unevaluatedItems":
			next = current.UnevaluatedItems
		case "additionalProperties":
			next = current.AdditionalProperties
		case "propertyNames":
			next = current.PropertyNames
		case "unevaluatedProperties":
			next = current.UnevaluatedProperties
		case "not":
			next = current.Not
		case "if":
			next = current.If
		case "then":
			next = current.Then
		case "else":
			next = current.Else
		case "contentSchema":
			next = current.ContentSchema
		default:
			return nil, false
		}
		return schemaAtPointer(next, nil, depth+1)
	}
	var next *jsonschema.Schema
	switch tokens[0] {
	case "$defs":
		next = current.Defs[tokens[1]]
	case "definitions":
		next = current.Definitions[tokens[1]]
	case "properties":
		next = current.Properties[tokens[1]]
	case "patternProperties":
		next = current.PatternProperties[tokens[1]]
	case "dependentSchemas":
		next = current.DependentSchemas[tokens[1]]
	case "allOf", "anyOf", "oneOf", "prefixItems":
		index, err := strconv.Atoi(tokens[1])
		if err != nil || index < 0 {
			return nil, false
		}
		var schemas []*jsonschema.Schema
		switch tokens[0] {
		case "allOf":
			schemas = current.AllOf
		case "anyOf":
			schemas = current.AnyOf
		case "oneOf":
			schemas = current.OneOf
		case "prefixItems":
			schemas = current.PrefixItems
		}
		if index >= len(schemas) {
			return nil, false
		}
		next = schemas[index]
	default:
		return nil, false
	}
	return schemaAtPointer(next, tokens[2:], depth+1)
}

func findSchemaAnchor(s *jsonschema.Schema, anchor string, seen map[*jsonschema.Schema]bool, depth int) (*jsonschema.Schema, bool) {
	if s == nil || seen[s] || depth > maxSchemaDepth {
		return nil, false
	}
	seen[s] = true
	if s.Anchor == anchor || s.DynamicAnchor == anchor {
		return s, true
	}
	for _, child := range schemaChildren(s) {
		if target, ok := findSchemaAnchor(child, anchor, seen, depth+1); ok {
			return target, true
		}
	}
	return nil, false
}

func schemaChildren(s *jsonschema.Schema) []*jsonschema.Schema {
	var children []*jsonschema.Schema
	v := reflect.ValueOf(s).Elem()
	schemaPtr := reflect.TypeFor[*jsonschema.Schema]()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		switch {
		case field.Type() == schemaPtr && !field.IsNil():
			children = append(children, field.Interface().(*jsonschema.Schema))
		case field.Kind() == reflect.Slice && field.Type().Elem() == schemaPtr:
			for j := 0; j < field.Len(); j++ {
				if !field.Index(j).IsNil() {
					children = append(children, field.Index(j).Interface().(*jsonschema.Schema))
				}
			}
		case field.Kind() == reflect.Map && field.Type().Elem() == schemaPtr:
			for _, key := range field.MapKeys() {
				value := field.MapIndex(key)
				if !value.IsNil() {
					children = append(children, value.Interface().(*jsonschema.Schema))
				}
			}
		}
	}
	return children
}

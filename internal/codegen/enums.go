package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// EnumsFromSource parses Go contract source and discovers named-type enum
// constant sets — a `type Severity string` (or integer) declaration paired with
// a `const` block of typed values — returning a SchemaOption for each (D-051).
//
// It is the seam the `generate` pipeline uses to feed WithEnum automatically:
// the schema generator works from reflection, which cannot see a `const` block,
// so the constant *values* must come from the source. EnumsFromSource bridges
// that — pass it the same contract source handed to TypeScriptForSource, then
// splat the result into SchemaFor:
//
//	enumOpts, err := codegen.EnumsFromSource(src)
//	schema, err := codegen.SchemaForType(t, enumOpts...)
//
// Only constants with an explicit named type and a literal string or integer
// value are collected; an untyped const, or one with a non-literal initializer,
// is skipped (it cannot be expressed as a static `enum`). On a parse failure
// the error wraps ErrInvalidContract.
func EnumsFromSource(goSource string) ([]SchemaOption, error) {
	values, err := EnumValuesFromSource(goSource)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	opts := make([]SchemaOption, 0, len(names))
	for _, name := range names {
		opts = append(opts, WithEnum(name, values[name]...))
	}
	return opts, nil
}

// EnumValuesFromSource returns the source-visible enum sets used by the real
// generate path. The returned map and slices are newly allocated.
func EnumValuesFromSource(goSource string) (map[string][]any, error) {
	src := goSource
	if !hasPackageClause(src) {
		src = "package contracts\n\n" + src
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "contracts.go", src, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: parse contract source for enums: %w", ErrInvalidContract, err)
	}

	// Collect named string/integer type declarations (candidate enum types).
	enumKind := make(map[string]bool) // type name -> is a candidate enum base
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			ident, ok := ts.Type.(*ast.Ident)
			if !ok {
				continue
			}
			switch ident.Name {
			case "string", "int", "int8", "int16", "int32", "int64",
				"uint", "uint8", "uint16", "uint32", "uint64":
				enumKind[ts.Name.Name] = true
			}
		}
	}

	// Collect the constant values declared against each candidate type.
	values := make(map[string][]any)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs.Type == nil {
				continue
			}
			typeIdent, ok := vs.Type.(*ast.Ident)
			if !ok || !enumKind[typeIdent.Name] {
				continue
			}
			for _, val := range vs.Values {
				lit, ok := val.(*ast.BasicLit)
				if !ok {
					continue
				}
				parsed, ok := literalValue(lit)
				if !ok {
					continue
				}
				values[typeIdent.Name] = append(values[typeIdent.Name], parsed)
			}
		}
	}

	return values, nil
}

// literalValue converts a Go basic literal to its JSON value, reporting false
// for a literal that cannot be a static enum member.
func literalValue(lit *ast.BasicLit) (any, bool) {
	switch lit.Kind {
	case token.STRING:
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil, false
		}
		return s, true
	case token.INT:
		n, err := strconv.ParseInt(strings.ReplaceAll(lit.Value, "_", ""), 0, 64)
		if err != nil {
			return nil, false
		}
		return n, true
	default:
		return nil, false
	}
}

package manifest

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hurtener/dockyard/internal/codegen"
)

// ContractsPackage is the canonical project-relative Go package containing all
// tool input and output contracts. Keeping every manifest reference at this
// boundary lets one generated contracts.ts cover every tool contract.
const ContractsPackage = "internal/contracts"

// ContractReference is a parsed tools[].input / tools[].output value: the Go
// type a tool's contract resolves to (RFC §6.1, contract-first). Its wire form
// is "<package path>.<TypeName>", e.g. "internal/contracts.ShowCustomerHealthInput".
type ContractReference struct {
	// Package is the import path of the package the type lives in.
	Package string
	// TypeName is the exported type identifier within that package.
	TypeName string
}

// String renders the reference back to its "<package>.<TypeName>" wire form.
func (r ContractReference) String() string { return r.Package + "." + r.TypeName }

// contractRefRe matches a contract reference: a slash-separated package path
// (each segment a Go identifier), a dot, then an exported (leading-uppercase)
// type name.
var contractRefRe = regexp.MustCompile(
	`^([A-Za-z_][A-Za-z0-9_]*(?:/[A-Za-z_][A-Za-z0-9_]*)*)\.([A-Z][A-Za-z0-9_]*)$`)

// ParseContractReference parses a tools[].input/output value into its package
// path and type name. It is the seam Wave 7's dockyard generate uses to locate
// the Go type to feed the codegen pipeline.
func ParseContractReference(ref string) (ContractReference, error) {
	m := contractRefRe.FindStringSubmatch(ref)
	if m == nil {
		return ContractReference{}, fmt.Errorf(
			"%q is not a Go type reference (want \"<package/path>.TypeName\")", ref)
	}
	return ContractReference{Package: m[1], TypeName: m[2]}, nil
}

// validateContractRef reports whether a tool input/output reference is a
// well-formed contract reference. An empty reference is itself a fault — every
// tool needs a typed contract (P1).
func validateContractRef(ref string) error {
	if ref == "" {
		return errors.New("required (a Go type reference, \"<package/path>.TypeName\")")
	}
	parsed, err := ParseContractReference(ref)
	if err != nil {
		return err
	}
	if parsed.Package != ContractsPackage {
		return fmt.Errorf("contract types must be declared in the canonical %q package (got %q)",
			ContractsPackage, parsed.Package)
	}
	return nil
}

// ContractResolver turns a tools[].input/output Go type reference into its JSON
// Schema. It is the seam between the manifest and the codegen pipeline (RFC
// §6.1): Phase 06 ships RegistryResolver for tests and the example, and Wave 7's
// dockyard generate supplies a source-scanning resolver that satisfies the same
// one-method contract.
type ContractResolver interface {
	// Resolve returns the JSON Schema for the named contract reference. It
	// returns an error wrapping ErrContractUnresolved when the reference names
	// no known type.
	Resolve(ref string) (*jsonschema.Schema, error)
}

// ErrContractUnresolved is returned by a ContractResolver when a reference names
// no type it can resolve. Callers branch on it with errors.Is.
var ErrContractUnresolved = errors.New("dockyard/internal/manifest: contract reference unresolved")

// ToolContracts is a tool's resolved input and output schemas, produced by
// ResolveContracts.
type ToolContracts struct {
	// Input is the resolved schema for the tool's input contract.
	Input *jsonschema.Schema
	// Output is the resolved schema for the tool's output contract.
	Output *jsonschema.Schema
}

// ResolveContracts resolves every tool's input and output reference through r,
// returning a map keyed by tool name. It fails fast on the first unresolved
// reference, wrapping the failure with the offending tool and field so the
// caller knows which manifest line to fix.
//
// ResolveContracts only reads the Manifest, so it is safe to call concurrently
// on a loaded Manifest with a concurrency-safe resolver.
func (m *Manifest) ResolveContracts(r ContractResolver) (map[string]ToolContracts, error) {
	if r == nil {
		return nil, errors.New("dockyard/internal/manifest: nil ContractResolver")
	}
	out := make(map[string]ToolContracts, len(m.Tools))
	for _, t := range m.Tools {
		if err := validateContractRef(t.Input); err != nil {
			return nil, fmt.Errorf("tool %q input %q: %w", t.Name, t.Input, err)
		}
		if err := validateContractRef(t.Output); err != nil {
			return nil, fmt.Errorf("tool %q output %q: %w", t.Name, t.Output, err)
		}
		in, err := r.Resolve(t.Input)
		if err != nil {
			return nil, fmt.Errorf("tool %q input %q: %w", t.Name, t.Input, err)
		}
		o, err := r.Resolve(t.Output)
		if err != nil {
			return nil, fmt.Errorf("tool %q output %q: %w", t.Name, t.Output, err)
		}
		out[t.Name] = ToolContracts{Input: in, Output: o}
	}
	return out, nil
}

// RegistryResolver is a ContractResolver backed by an explicit map of contract
// reference to Go type. It is how Phase 06 resolves contracts without scanning
// Go source: a caller registers each contract type, and Resolve runs it through
// internal/codegen.SchemaForType — the reflect-based seam Phase 04 shaped for
// exactly this use. Wave 7's dockyard generate replaces it with a
// source-scanning resolver behind the same ContractResolver interface.
//
// A RegistryResolver is built once and then read-only, so it is safe for
// concurrent use; Register must not race with Resolve.
type RegistryResolver struct {
	types map[string]reflect.Type
}

// NewRegistryResolver returns an empty RegistryResolver.
func NewRegistryResolver() *RegistryResolver {
	return &RegistryResolver{types: map[string]reflect.Type{}}
}

// Register binds a contract reference to the Go type T, so a manifest naming
// ref resolves to T's schema. It returns the resolver for fluent chaining.
func Register[T any](r *RegistryResolver, ref string) *RegistryResolver {
	r.types[ref] = reflect.TypeFor[T]()
	return r
}

// Resolve implements ContractResolver: it looks the reference up in the
// registry and runs the bound type through the codegen schema engine.
func (r *RegistryResolver) Resolve(ref string) (*jsonschema.Schema, error) {
	t, ok := r.types[ref]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrContractUnresolved, ref)
	}
	s, err := codegen.SchemaForType(t)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", ref, err)
	}
	return s, nil
}

package tool

import (
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/hurtener/dockyard/internal/codegen"
)

// MarshalSchema serializes a tool-contract JSON Schema to deterministic,
// indented JSON — identical input always yields byte-identical output.
//
// It is the public re-export of the codegen pipeline's deterministic schema
// marshaller (internal/codegen.Marshal). A scaffolded project lives in its own
// Go module and cannot import Dockyard's internal/ packages, but `dockyard
// generate` regenerates a project's per-contract schema files by `go run`-ing a
// small generator inside that project (Phase 18, D-081): that ephemeral
// generator obtains a tool's *jsonschema.Schema from Builder.Schemas and needs
// the same byte-stable marshalling the rest of the pipeline uses, so the
// regenerated file matches what `dockyard validate`'s stale-codegen check
// expects. MarshalSchema is that seam — exported here because runtime/tool is
// public and internal/codegen is not.
//
// Determinism is what makes regeneration safe and `dockyard validate`'s
// stale-codegen drift detection meaningful (RFC §6.2): a real change in a
// contract surfaces as a visible diff, never as formatting churn.
func MarshalSchema(s *jsonschema.Schema) ([]byte, error) {
	return codegen.Marshal(s)
}

// SchemaOption configures generated contract schemas.
type SchemaOption = codegen.SchemaOption

// WithEnum preserves a named Go constant set in generated schemas.
var WithEnum = codegen.WithEnum

// RegisterEnumMetadata is called by generated contract metadata so runtime
// builder schemas retain the enum sets discovered from Go source.
func RegisterEnumMetadata(typeName string, values ...any) {
	codegen.RegisterEnumMetadata(typeName, values...)
}

// InputSchemaFor generates the object-only input contract schema.
func InputSchemaFor[T any](opts ...SchemaOption) (*jsonschema.Schema, error) {
	return codegen.SchemaFor[T](opts...)
}

// OutputSchemaFor generates an output schema for any JSON-representable type.
func OutputSchemaFor[T any](opts ...SchemaOption) (*jsonschema.Schema, error) {
	return codegen.OutputSchemaFor[T](opts...)
}

package codegen_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/internal/codegen"
)

func FuzzValidateSchema(f *testing.F) {
	f.Add([]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`))
	f.Add([]byte(`{"$ref":"https://example.com/x"}`))
	f.Fuzz(func(_ *testing.T, raw []byte) {
		_, _ = codegen.ValidateSchema(raw, false)
	})
}

func TestRecursiveSchemaConcurrent(t *testing.T) {
	t.Parallel()
	const workers = 16
	errs := make(chan error, workers)
	for range workers {
		go func() {
			s, err := codegen.SchemaFor[auditNode]()
			if err == nil {
				_, err = codegen.Marshal(s)
			}
			errs <- err
		}()
	}
	for range workers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateSchemaRejectsWrongDialectAndExternalRefs(t *testing.T) {
	for _, raw := range []string{
		`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"https://example.com/schema"}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","$dynamicRef":"other.json#node"}`,
	} {
		if _, err := codegen.ValidateSchema([]byte(raw), false); !errors.Is(err, codegen.ErrNonconformantSchema) {
			t.Fatalf("ValidateSchema(%s) error = %v", raw, err)
		}
	}
}

func TestValidateSchemaAcceptsLocalComposition(t *testing.T) {
	raw := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"name":{"type":"string"}},"allOf":[{"type":"object","properties":{"name":{"$ref":"#/$defs/name"}},"required":["name"]}]}`)
	if _, err := codegen.ValidateSchema(raw, true); err != nil {
		t.Fatalf("local composition: %v", err)
	}
}

func TestValidateSchemaResolvesNestedLocalRefFromRoot(t *testing.T) {
	raw := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"wrapper":{"$defs":{"input":{"type":"object"}}}},"allOf":[{"$ref":"#/$defs/wrapper/$defs/input"}]}`)
	if _, err := codegen.ValidateSchema(raw, true); err != nil {
		t.Fatalf("nested local ref: %v", err)
	}
}

func TestValidateSchemaRejectsInvalidJSONPointerEscapes(t *testing.T) {
	for _, token := range []string{"~", "~2", "name~x"} {
		raw := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"` + token + `":{"type":"object"}},"$ref":"#/$defs/` + token + `"}`)
		if _, err := codegen.ValidateSchema(raw, false); !errors.Is(err, codegen.ErrNonconformantSchema) {
			t.Fatalf("pointer token %q error = %v, want nonconformant schema", token, err)
		}
	}
}

func TestValidateSchemaAcceptsEscapedJSONPointerTokens(t *testing.T) {
	for _, raw := range []string{
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"a/b":{"type":"object"}},"$ref":"#/$defs/a~1b"}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"a~b":{"type":"object"}},"$ref":"#/$defs/a~0b"}`,
	} {
		if _, err := codegen.ValidateSchema([]byte(raw), true); err != nil {
			t.Fatalf("escaped pointer %s: %v", raw, err)
		}
	}
}

func TestValidateSchemaResolvesLocalAnchorFromRoot(t *testing.T) {
	raw := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"input":{"$anchor":"input","type":"object"}},"$ref":"#input"}`)
	if _, err := codegen.ValidateSchema(raw, true); err != nil {
		t.Fatalf("anchor local ref: %v", err)
	}
}

func TestValidateSchemaRejectsCyclicLocalAnchorObjectRoot(t *testing.T) {
	raw := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"loop":{"$anchor":"loop","$ref":"#loop"}},"$ref":"#loop"}`)
	if _, err := codegen.ValidateSchema(raw, true); !errors.Is(err, codegen.ErrNonconformantSchema) {
		t.Fatalf("cyclic anchor error = %v", err)
	}
}

func TestValidateSchemaRequiresObjectOnlyInputRoot(t *testing.T) {
	for _, raw := range []string{
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":["null","object"]}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":["object","string"]}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","anyOf":[{"type":"object"},{"type":"string"}]}`,
	} {
		if _, err := codegen.ValidateSchema([]byte(raw), true); !errors.Is(err, codegen.ErrNonconformantSchema) {
			t.Fatalf("schema %s error = %v", raw, err)
		}
	}
}

func TestValidateSchemaRejectsRecursiveNonObjectRoot(t *testing.T) {
	raw := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"loop":{"$ref":"#/$defs/loop"}},"$ref":"#/$defs/loop"}`)
	if _, err := codegen.ValidateSchema(raw, true); !errors.Is(err, codegen.ErrNonconformantSchema) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSchemaAcceptsFragmentAndAnchorRefs(t *testing.T) {
	for _, raw := range []string{
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"#"}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"name":{"$anchor":"name","type":"string"}},"$ref":"#name"}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","examples":[{"$ref":"https://example.com/instance-value"}]}`,
	} {
		if _, err := codegen.ValidateSchema([]byte(raw), false); err != nil {
			t.Fatalf("local ref %s: %v", raw, err)
		}
	}
}

func TestValidateSchemaBoundsDepth(t *testing.T) {
	raw := `{"$schema":"https://json-schema.org/draft/2020-12/schema","allOf":[` + strings.Repeat(`{"allOf":[`, 130) + `{}` + strings.Repeat(`]}`, 130) + `]}`
	if _, err := codegen.ValidateSchema([]byte(raw), false); !errors.Is(err, codegen.ErrNonconformantSchema) {
		t.Fatalf("error = %v", err)
	}
}

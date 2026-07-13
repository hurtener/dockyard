package codegen

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type boundedEmbeddedLeaf struct {
	Value string `json:"value"`
}

type boundedEmbeddedMiddle struct {
	boundedEmbeddedLeaf
}

type boundedEmbeddedRoot struct {
	boundedEmbeddedMiddle
}

type jsonStringScalarBase int
type jsonStringScalar jsonStringScalarBase
type jsonStringPointerAlias = *int
type jsonStringByteAlias = byte
type jsonStringRuneAlias = rune
type jsonStringBytePointerAlias = *byte
type jsonStringRunePointerAlias = *rune

type jsonStringContract struct {
	Count int               `json:"count,string"`
	Ratio float64           `json:"ratio,string,omitempty"`
	Limit *jsonStringScalar `json:"limit,string"`
	Maybe *jsonStringScalar `json:"maybe,string,omitempty"`
}

type invalidJSONStringContract struct {
	Values []int `json:"values,string"` //nolint:staticcheck // This malformed option is the input under test.
}

type invalidJSONStringPointerChainContract struct {
	Value **int `json:"value,string"` //nolint:staticcheck // encoding/json does not quote through two pointers.
}

type jsonStringNamedPointer *int

type invalidJSONStringNamedPointerContract struct {
	Value jsonStringNamedPointer `json:"value,string"` //nolint:staticcheck // encoding/json does not quote named pointers.
}

type jsonStringPointerKindsContract struct {
	Bool   *bool    `json:"bool,string"`
	Int    *int64   `json:"int,string"`
	Uint   *uint64  `json:"uint,string"`
	Float  *float64 `json:"float,string"`
	String *string  `json:"string,string"`
}

type jsonStringEnum string

type jsonStringEnumContract struct {
	Value jsonStringEnum `json:"value,string"`
}

type jsonStringPointerAliasContract struct {
	Value jsonStringPointerAlias `json:"value,string"`
}

type jsonStringPredeclaredAliasContract struct {
	Byte        byte                       `json:"byte,string"`
	Rune        rune                       `json:"rune,string"`
	ByteAlias   jsonStringByteAlias        `json:"byte_alias,string"`
	RuneAlias   jsonStringRuneAlias        `json:"rune_alias,string"`
	BytePointer jsonStringBytePointerAlias `json:"byte_pointer,string"`
	RunePointer jsonStringRunePointerAlias `json:"rune_pointer,string"`
}

type JSONStringAnonymousScalar int
type JSONStringScalar int

type jsonStringAnonymousContract struct {
	JSONStringAnonymousScalar `json:"count,string"`
	*JSONStringScalar         `json:"limit,string"`
}

func TestJSONFieldsBoundsEmbeddedExpansion(t *testing.T) {
	if _, err := jsonFieldsBounded(reflect.TypeFor[boundedEmbeddedRoot](), 1, maxGenerationNodes); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("depth error = %v, want ErrInvalidContract", err)
	}
	if _, err := jsonFieldsBounded(reflect.TypeFor[boundedEmbeddedRoot](), maxGenerationDepth, 1); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("node error = %v, want ErrInvalidContract", err)
	}
}

func TestSchemaJSONStringOptionUsesWireType(t *testing.T) {
	schema, err := SchemaFor[jsonStringContract]()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"count", "ratio"} {
		if got := schema.Properties[name]; got == nil || got.Type != "string" {
			t.Fatalf("property %s = %#v, want string schema", name, got)
		}
	}
	for _, name := range []string{"limit", "maybe"} {
		got := schema.Properties[name]
		if got == nil || !reflect.DeepEqual(got.Types, []string{"null", "string"}) {
			t.Fatalf("property %s = %#v, want nullable string schema", name, got)
		}
	}
	if !reflect.DeepEqual(schema.Required, []string{"count", "limit"}) {
		t.Fatalf("required = %v, want count and non-omitempty pointer limit", schema.Required)
	}
	if _, err := SchemaFor[invalidJSONStringContract](); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("unsupported ,string error = %v, want ErrInvalidContract", err)
	}
	if _, err := SchemaFor[invalidJSONStringPointerChainContract](); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("pointer-chain ,string error = %v, want ErrInvalidContract", err)
	}
	if _, err := SchemaFor[invalidJSONStringNamedPointerContract](); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("named-pointer ,string error = %v, want ErrInvalidContract", err)
	}
	enumSchema, err := SchemaFor[jsonStringEnumContract](WithEnum("jsonStringEnum", "one", "two"))
	if err != nil {
		t.Fatal(err)
	}
	if got := enumSchema.Properties["value"]; len(got.Enum) != 0 {
		t.Fatalf("json ,string wire encoding retained source enum values: %v", got.Enum)
	}
	aliasSchema, err := SchemaFor[jsonStringPointerAliasContract]()
	if err != nil {
		t.Fatal(err)
	}
	if got := aliasSchema.Properties["value"]; got == nil || !reflect.DeepEqual(got.Types, []string{"null", "string"}) {
		t.Fatalf("pointer alias property = %#v, want nullable string schema", got)
	}
	predeclaredAliasSchema, err := SchemaFor[jsonStringPredeclaredAliasContract]()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"byte", "rune", "byte_alias", "rune_alias"} {
		if got := predeclaredAliasSchema.Properties[name]; got == nil || got.Type != "string" {
			t.Fatalf("predeclared alias property %s = %#v, want string schema", name, got)
		}
	}
	for _, name := range []string{"byte_pointer", "rune_pointer"} {
		if got := predeclaredAliasSchema.Properties[name]; got == nil || !reflect.DeepEqual(got.Types, []string{"null", "string"}) {
			t.Fatalf("predeclared pointer alias property %s = %#v, want nullable string schema", name, got)
		}
	}
	anonymousSchema, err := SchemaFor[jsonStringAnonymousContract]()
	if err != nil {
		t.Fatal(err)
	}
	if got := anonymousSchema.Properties["count"]; got == nil || got.Type != "string" {
		t.Fatalf("anonymous scalar property = %#v, want string schema", got)
	}
	if got := anonymousSchema.Properties["limit"]; got == nil || !reflect.DeepEqual(got.Types, []string{"null", "string"}) {
		t.Fatalf("anonymous pointer property = %#v, want nullable string schema", got)
	}
}

func TestJSONStringPointerWireBehavior(t *testing.T) {
	n := 42
	scalar := jsonStringScalar(n)
	tests := []struct {
		name string
		in   jsonStringContract
		want string
	}{
		{name: "nil", in: jsonStringContract{}, want: `"limit":null`},
		{name: "non-nil", in: jsonStringContract{Limit: &scalar, Maybe: &scalar}, want: `"limit":"42"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), tt.want) {
				t.Fatalf("json.Marshal = %s, want fragment %s", raw, tt.want)
			}
			if tt.name == "nil" && strings.Contains(string(raw), `"maybe"`) {
				t.Fatalf("omitempty nil pointer was emitted: %s", raw)
			}
			if tt.name == "non-nil" && !strings.Contains(string(raw), `"maybe":"42"`) {
				t.Fatalf("non-nil omitempty pointer was not string encoded: %s", raw)
			}
		})
	}
	chain := &n
	raw, err := json.Marshal(invalidJSONStringPointerChainContract{Value: &chain})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"value":42}` {
		t.Fatalf("pointer-chain encoding changed: %s", raw)
	}
	named := jsonStringNamedPointer(&n)
	raw, err = json.Marshal(invalidJSONStringNamedPointerContract{Value: named})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"value":42}` {
		t.Fatalf("named-pointer encoding changed: %s", raw)
	}
	var decoded jsonStringContract
	if err := json.Unmarshal([]byte(`{"count":"0","limit":"42","maybe":"7"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Limit == nil || *decoded.Limit != 42 || decoded.Maybe == nil || *decoded.Maybe != 7 {
		t.Fatalf("quoted pointer values decoded incorrectly: %#v", decoded)
	}
	if err := json.Unmarshal([]byte(`{"count":"0","limit":null}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Limit != nil {
		t.Fatalf("null pointer decoded as non-nil: %#v", decoded.Limit)
	}
	if err := json.Unmarshal([]byte(`{"count":"0","limit":42}`), &decoded); err == nil {
		t.Fatal("unquoted pointer value unexpectedly accepted for json ,string")
	}
}

func TestJSONStringAliasAndAnonymousWireBehavior(t *testing.T) {
	n := 42
	alias := &n
	raw, err := json.Marshal(jsonStringPointerAliasContract{Value: alias})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"value":"42"}` {
		t.Fatalf("pointer alias json.Marshal = %s", raw)
	}

	scalar := JSONStringScalar(n)
	raw, err = json.Marshal(jsonStringAnonymousContract{
		JSONStringAnonymousScalar: JSONStringAnonymousScalar(n),
		JSONStringScalar:          &scalar,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"count":"42","limit":"42"}` {
		t.Fatalf("anonymous scalar json.Marshal = %s", raw)
	}

	var decoded jsonStringAnonymousContract
	if err := json.Unmarshal([]byte(`{"count":"7","limit":"9"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.JSONStringAnonymousScalar != 7 || decoded.JSONStringScalar == nil || *decoded.JSONStringScalar != 9 {
		t.Fatalf("anonymous scalar json.Unmarshal = %#v", decoded)
	}
}

func TestJSONStringPredeclaredAliasWireBehavior(t *testing.T) {
	b, r := byte(255), rune(-7)
	in := jsonStringPredeclaredAliasContract{
		Byte:        1,
		Rune:        2,
		ByteAlias:   3,
		RuneAlias:   -4,
		BytePointer: &b,
		RunePointer: &r,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"byte":"1"`, `"rune":"2"`, `"byte_alias":"3"`, `"rune_alias":"-4"`, `"byte_pointer":"255"`, `"rune_pointer":"-7"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("json.Marshal = %s, want fragment %s", raw, want)
		}
	}

	var decoded jsonStringPredeclaredAliasContract
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Byte != 1 || decoded.Rune != 2 || decoded.ByteAlias != 3 || decoded.RuneAlias != -4 ||
		decoded.BytePointer == nil || *decoded.BytePointer != 255 || decoded.RunePointer == nil || *decoded.RunePointer != -7 {
		t.Fatalf("json.Unmarshal = %#v", decoded)
	}
}

func TestJSONStringPointerScalarKinds(t *testing.T) {
	schema, err := SchemaFor[jsonStringPointerKindsContract]()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bool", "int", "uint", "float", "string"} {
		if got := schema.Properties[name]; got == nil || !reflect.DeepEqual(got.Types, []string{"null", "string"}) {
			t.Fatalf("property %s = %#v, want nullable string schema", name, got)
		}
	}
	b, i, u, f, s := true, int64(-4), uint64(5), 1.25, "value"
	raw, err := json.Marshal(jsonStringPointerKindsContract{Bool: &b, Int: &i, Uint: &u, Float: &f, String: &s})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"bool":"true"`, `"int":"-4"`, `"uint":"5"`, `"float":"1.25"`, `"string":"\"value\""`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("json.Marshal = %s, want fragment %s", raw, want)
		}
	}
}

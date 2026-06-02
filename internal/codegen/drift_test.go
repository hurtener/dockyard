package codegen_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hurtener/dockyard/internal/codegen"
)

// driftRevenueSource is a contract whose Go field set and json tags match the
// showRevenueOutput schema fixture (golden_test.go) exactly — so a freshly
// generated schema and a freshly generated TypeScript interface for the same
// contract cross-check clean.
const driftRevenueSource = "type ShowRevenueOutput struct {\n" +
	"\tHeadline string  `json:\"headline\"`\n" +
	"\tTotal    float64 `json:\"total\"`\n" +
	"\tLines    []int   `json:\"lines\"`\n" +
	"\tCurrency string  `json:\"currency,omitempty\"`\n" +
	"}\n"

// approvalLike mirrors the approval-flows template's RequestApprovalInput: a
// free-shape `map[string]any` field (`metadata`) whose Go doc comment carries an
// example object literal. The generated TS renders that comment as JSDoc, and the
// example's closing `}` lands on a comment line with no `{` — the case that used
// to truncate parseTSInterface and report `metadata` as missing.
type approvalLike struct {
	Title string `json:"title"`
	// Metadata is an opaque, free-shape JSON object the App renders as a
	// key/value table for context (e.g. {"subscribers": 1247, "since":
	// "Mon 09:00"}). Optional.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// --- CrossCheck: passing -----------------------------------------------------

func TestCrossCheck_Match(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	ts, err := codegen.TypeScriptForSource(driftRevenueSource)
	if err != nil {
		t.Fatalf("TypeScriptForSource: %v", err)
	}
	if err := codegen.CrossCheck(schema, "ShowRevenueOutput", ts); err != nil {
		t.Errorf("CrossCheck on a matched schema/TS pair should pass, got: %v", err)
	}
}

// TestCrossCheck_DocCommentBraceNotInterfaceClose pins the fix for the
// approval-flows `dockyard build` blocker: a `}` inside a field's JSDoc comment
// (here, an example object in the `metadata` doc) must not be read as the
// interface's closing brace, which truncated the field list and falsely reported
// the trailing `metadata` field as "missing from TypeScript".
func TestCrossCheck_DocCommentBraceNotInterfaceClose(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[approvalLike]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	// The TS exactly as generated: a JSDoc example whose closing `}` is on a
	// comment line with no `{`, then the real `metadata` field after it.
	ts := []byte("export interface ApprovalLike {\n" +
		"  title: string;\n" +
		"  /**\n" +
		"   * Metadata is an opaque, free-shape JSON object (e.g.\n" +
		"   * {\"subscribers\": 1247, \"since\": \"Mon 09:00\"}). Optional.\n" +
		"   */\n" +
		"  metadata?: { [key: string]: any};\n" +
		"}\n")
	if err := codegen.CrossCheck(schema, "ApprovalLike", ts); err != nil {
		t.Errorf("a }-in-doc-comment must not truncate the interface; got drift: %v", err)
	}
}

// --- CrossCheck: desync classes ---------------------------------------------

func TestCrossCheck_PropertyMissingFromTS(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	// A TS interface that drops the `total` property — a schema↔TS desync.
	ts := []byte("export interface ShowRevenueOutput {\n" +
		"  headline: string;\n" +
		"  lines: number[];\n" +
		"  currency?: string;\n" +
		"}\n")
	assertDrift(t, codegen.CrossCheck(schema, "ShowRevenueOutput", ts), "total")
}

func TestCrossCheck_ExtraPropertyInTS(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	// A TS interface with a `surprise` field the schema does not have.
	ts := []byte("export interface ShowRevenueOutput {\n" +
		"  headline: string;\n" +
		"  total: number;\n" +
		"  lines: number[];\n" +
		"  currency?: string;\n" +
		"  surprise: string;\n" +
		"}\n")
	assertDrift(t, codegen.CrossCheck(schema, "ShowRevenueOutput", ts), "surprise")
}

func TestCrossCheck_OptionalityMismatch(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	// `currency` is optional in the schema (omitempty) but required in this TS.
	ts := []byte("export interface ShowRevenueOutput {\n" +
		"  headline: string;\n" +
		"  total: number;\n" +
		"  lines: number[];\n" +
		"  currency: string;\n" +
		"}\n")
	assertDrift(t, codegen.CrossCheck(schema, "ShowRevenueOutput", ts), "currency")
}

func TestCrossCheck_InterfaceNotFound(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	ts := []byte("export interface SomethingElse {\n  x: string;\n}\n")
	assertDrift(t, codegen.CrossCheck(schema, "ShowRevenueOutput", ts), "not found")
}

func TestCrossCheck_NilSchema(t *testing.T) {
	t.Parallel()
	assertDrift(t, codegen.CrossCheck(nil, "X", []byte("export interface X {}")), "nil schema")
}

func TestCrossCheck_NonObjectSchema(t *testing.T) {
	t.Parallel()
	// SchemaForType rejects non-objects, so build a bare scalar schema by hand
	// to drive the non-object branch of CrossCheck.
	scalar := mustScalarSchema(t)
	assertDrift(t, codegen.CrossCheck(scalar, "X", []byte("export interface X {\n  y: string;\n}")),
		"not an object")
}

// --- CrossCheck: value-type drift (finding 6 / D-051) -----------------------

func TestCrossCheck_TypeDivergence(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	// `total` is a number in the schema but rendered as a string in this TS —
	// exactly the value-type divergence findings 1–4 produce. The name set and
	// optionality all match, so only the new type check can catch it.
	ts := []byte("export interface ShowRevenueOutput {\n" +
		"  headline: string;\n" +
		"  total: string;\n" +
		"  lines: number[];\n" +
		"  currency?: string;\n" +
		"}\n")
	err = codegen.CrossCheck(schema, "ShowRevenueOutput", ts)
	assertDrift(t, err, "total")
	if !strings.Contains(err.Error(), "number in the schema but string in TypeScript") {
		t.Errorf("error should describe the type divergence, got %v", err)
	}
}

func TestCrossCheck_ArrayVsObjectDivergence(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	// `lines` is an array in the schema but an index object in this TS.
	ts := []byte("export interface ShowRevenueOutput {\n" +
		"  headline: string;\n" +
		"  total: number;\n" +
		"  lines: { [key: string]: number};\n" +
		"  currency?: string;\n" +
		"}\n")
	assertDrift(t, codegen.CrossCheck(schema, "ShowRevenueOutput", ts), "lines")
}

func TestCrossCheck_TygoIntAnnotationIsNotDrift(t *testing.T) {
	t.Parallel()
	// tygo renders an int as `number /* int */`; the `/* int */` annotation
	// must not be mistaken for a divergent type.
	schema, err := codegen.SchemaFor[nestedOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	ts := []byte("export interface NestedOutput {\n" +
		"  summary: string;\n" +
		"  score: number /* int */;\n" +
		"  signals: HealthSignal[];\n" +
		"}\n")
	if err := codegen.CrossCheck(schema, "NestedOutput", ts); err != nil {
		t.Errorf("a tygo `/* int */` annotation should not be read as drift, got: %v", err)
	}
}

func TestCrossCheck_UnconstrainedTypeIsNeverDrift(t *testing.T) {
	t.Parallel()
	// A json.RawMessage property is an unconstrained schema; whatever TS type it
	// is paired with, it must not report a type drift.
	type rawCarrier struct {
		Note    string          `json:"note"`
		Payload json.RawMessage `json:"payload"`
	}
	schema, err := codegen.SchemaFor[rawCarrier]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	ts := []byte("export interface RawCarrier {\n" +
		"  note: string;\n" +
		"  payload: any;\n" +
		"}\n")
	if err := codegen.CrossCheck(schema, "RawCarrier", ts); err != nil {
		t.Errorf("an unconstrained (json.RawMessage) property should not drift, got: %v", err)
	}
}

// --- CrossCheck: the documented WithNullOptional limitation ------------------

// TestCrossCheck_WithNullOptionalIsMisclassified pins the limitation CrossCheck's
// doc comment warns about: an optional field generated with WithNullOptional
// renders as `field: T | null` with NO `?` marker, so parseTSInterface — which
// keys optionality solely off the `?` — reads it as required. CrossCheck then
// reports a false ErrSchemaTSDrift on any omitempty field whenever it is handed
// WithNullOptional output. The doc comment instructs callers to pass non-null
// (default `field?: T`) TypeScript to CrossCheck; this test makes that
// documented contract a guarded one, so Phase 18 inherits a known boundary
// rather than a surprise.
func TestCrossCheck_WithNullOptionalIsMisclassified(t *testing.T) {
	t.Parallel()
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}

	// Sanity: with the default optional style the matched pair cross-checks
	// clean — the WithNullOptional artifact is the only thing changing below.
	defaultTS, err := codegen.TypeScriptForSource(driftRevenueSource)
	if err != nil {
		t.Fatalf("TypeScriptForSource (default): %v", err)
	}
	if err := codegen.CrossCheck(schema, "ShowRevenueOutput", defaultTS); err != nil {
		t.Fatalf("CrossCheck on the default-style pair should pass, got: %v", err)
	}

	// Same contract, generated with WithNullOptional: `currency` (an omitempty
	// field) renders as `currency: string | null`, with no `?`.
	nullTS, err := codegen.TypeScriptForSource(driftRevenueSource, codegen.WithNullOptional())
	if err != nil {
		t.Fatalf("TypeScriptForSource (WithNullOptional): %v", err)
	}
	if !strings.Contains(string(nullTS), "currency: string | null") {
		t.Fatalf("WithNullOptional output should render `currency: string | null`, got:\n%s", nullTS)
	}
	if strings.Contains(string(nullTS), "currency?:") {
		t.Fatalf("WithNullOptional output should NOT carry a `?` on currency, got:\n%s", nullTS)
	}

	// CrossCheck misclassifies the schema-optional `currency` as TS-required and
	// reports drift — the documented limitation. This is the contract Phase 18
	// inherits: feed CrossCheck default-style TypeScript, not WithNullOptional.
	err = codegen.CrossCheck(schema, "ShowRevenueOutput", nullTS)
	if err == nil {
		t.Fatal("documented limitation regressed: CrossCheck should report a false " +
			"drift on a WithNullOptional artifact's omitempty field")
	}
	if !errors.Is(err, codegen.ErrSchemaTSDrift) {
		t.Errorf("error should wrap ErrSchemaTSDrift, got %v", err)
	}
	if !strings.Contains(err.Error(), "currency") {
		t.Errorf("error should name the misclassified property `currency`, got %v", err)
	}
	if !strings.Contains(err.Error(), "optional in the schema but required in TypeScript") {
		t.Errorf("error should describe the optionality misclassification, got %v", err)
	}
}

// --- CheckStale -------------------------------------------------------------

func TestCheckStale_Fresh(t *testing.T) {
	t.Parallel()
	a, err := codegen.TypeScriptForSource(driftRevenueSource)
	if err != nil {
		t.Fatalf("TypeScriptForSource: %v", err)
	}
	// Identical regeneration ⇒ not stale.
	if err := codegen.CheckStale(a, a); err != nil {
		t.Errorf("CheckStale on identical bytes should pass, got: %v", err)
	}
}

func TestCheckStale_Drifted(t *testing.T) {
	t.Parallel()
	onDisk, err := codegen.TypeScriptForSource(driftRevenueSource)
	if err != nil {
		t.Fatalf("TypeScriptForSource: %v", err)
	}
	// The Go source gained a field; the on-disk file was not regenerated.
	fresh, err := codegen.TypeScriptForSource(driftRevenueSource +
		"\ntype Extra struct {\n\tNote string `json:\"note\"`\n}\n")
	if err != nil {
		t.Fatalf("TypeScriptForSource: %v", err)
	}
	err = codegen.CheckStale(onDisk, fresh)
	if err == nil {
		t.Fatal("CheckStale should fail when on-disk output differs from a fresh regeneration")
	}
	if !errors.Is(err, codegen.ErrStaleGenerated) {
		t.Errorf("error should wrap ErrStaleGenerated, got %v", err)
	}
}

func TestCheckStale_SchemaArtifact(t *testing.T) {
	t.Parallel()
	// CheckStale is artifact-agnostic — it also guards the generated schema.
	schema, err := codegen.SchemaFor[showRevenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	onDisk, err := codegen.Marshal(schema)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	other, err := codegen.SchemaFor[scalarsInput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	fresh, err := codegen.Marshal(other)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := codegen.CheckStale(onDisk, fresh); err == nil {
		t.Error("CheckStale should flag a stale generated schema")
	}
}

// --- helpers ----------------------------------------------------------------

// mustScalarSchema builds a bare scalar (non-object) schema directly, to drive
// CrossCheck's non-object branch — SchemaForType rejects non-objects up front.
func mustScalarSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	return &jsonschema.Schema{Type: "string"}
}

func assertDrift(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a drift error mentioning %q, got nil", substr)
	}
	if !errors.Is(err, codegen.ErrSchemaTSDrift) {
		t.Errorf("error should wrap ErrSchemaTSDrift, got %v", err)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Errorf("error %q should mention %q", err.Error(), substr)
	}
}

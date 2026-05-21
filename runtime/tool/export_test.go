package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/runtime/server"
)

// testRuntime is a thin in-package test wrapper over an unexported
// handlerRuntime so the external _test package can exercise edge validation,
// the content split, and flag detection without the handler runtime leaking
// into the public API.
type testRuntime struct {
	rt *handlerRuntime[ratedTestInput, struct{}]
}

// ratedTestInput is the contract fixture for the edge-validation tests. It is
// in-package so export_test.go can name it; the external test's ratedInput is a
// separate, equivalent fixture.
type ratedTestInput struct {
	Period string `json:"period" jsonschema:"the reporting period"`
}

// NewHandlerRuntimeForTest builds a handler runtime over the ratedTestInput
// contract for the edge-validation tests.
func NewHandlerRuntimeForTest(t *testing.T) *testRuntime {
	t.Helper()
	in, err := codegen.SchemaFor[ratedTestInput]()
	if err != nil {
		t.Fatalf("SchemaFor: %v", err)
	}
	rt, err := newHandlerRuntime[ratedTestInput, struct{}](
		"rated",
		func(context.Context, ratedTestInput) (Result[struct{}], error) {
			return Result[struct{}]{}, nil
		},
		in,
		DefaultOutputSizeBudget,
	)
	if err != nil {
		t.Fatalf("newHandlerRuntime: %v", err)
	}
	return &testRuntime{rt: rt}
}

// ValidateForTest runs edge validation against raw JSON arguments, as the wire
// path does (server.RawArguments carries the raw bytes).
func (tr *testRuntime) ValidateForTest(ctx context.Context, rawArgs string) error {
	ctx = server.WithRawArguments(ctx, json.RawMessage(rawArgs))
	var zero ratedTestInput
	return tr.rt.validateArgs(ctx, zero)
}

// ValidateDecodedForTest runs edge validation with no raw arguments in context,
// exercising the decoded-value fallback path.
func (tr *testRuntime) ValidateDecodedForTest() error {
	var zero ratedTestInput
	return tr.rt.validateArgs(context.Background(), zero)
}

// DetectFlagsForTest exposes the pure flag detector for unit tests.
func DetectFlagsForTest(toolName, text string, structuredJSON []byte, budget int) []Flag {
	return detectFlags(toolName, text, structuredJSON, budget)
}

// LooksLikeJSONPayloadForTest exposes the misroute heuristic for unit tests.
func LooksLikeJSONPayloadForTest(s string) bool { return looksLikeJSONPayload(s) }

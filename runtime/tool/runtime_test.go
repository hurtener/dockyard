package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// ratedInput carries a constrained field so edge validation has something to
// reject that survives Go's JSON decode (an out-of-range value, an unknown
// field).
type ratedInput struct {
	Period string `json:"period" jsonschema:"the reporting period"`
}

// callWithRawArgs invokes a registered tool over the in-memory transport with
// the given raw JSON arguments and returns the result. It is the wire path:
// the SDK decodes and the Dockyard handler runtime runs behind it.
func callWithRawArgs(t *testing.T, s *server.Server, name string, raw string) *mcpsdk.CallToolResult {
	t.Helper()
	session := connect(t, s)
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: json.RawMessage(raw),
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	return res
}

// TestEdgeValidationRejectsInvalidArguments proves the catalog-edge validation
// path: a schema-violating argument is rejected as an error result, never a
// panic and never a vague success (RFC §5, §6.3; D-044).
func TestEdgeValidationRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
	}{
		{"wrong type for required field", `{"period": 123}`},
		{"missing required field", `{}`},
		{"required field is null", `{"period": null}`},
		{"unknown field", `{"period": "2026-Q1", "unknown": true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newServer(t)
			if err := tool.New[ratedInput, revenueOutput]("rated").
				Handler(func(context.Context, ratedInput) (tool.Result[revenueOutput], error) {
					return tool.Result[revenueOutput]{Text: "ok"}, nil
				}).
				Register(s); err != nil {
				t.Fatalf("Register: %v", err)
			}
			res := callWithRawArgs(t, s, "rated", tc.args)
			if !res.IsError {
				t.Fatalf("invalid arguments %s should produce an error result, got %+v", tc.args, res)
			}
		})
	}
}

// TestArgumentErrorIsTyped proves the handler runtime produces a typed
// *ArgumentError that wraps ErrInvalidArguments — the in-process edge-validation
// surface contract-first callers (the inspector, tests) branch on (D-044).
func TestArgumentErrorIsTyped(t *testing.T) {
	t.Parallel()

	rt := tool.NewHandlerRuntimeForTest(t)

	// Valid arguments pass.
	if err := rt.ValidateForTest(context.Background(), `{"period":"2026-Q1"}`); err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}

	// A schema-violating argument is a typed *ArgumentError.
	err := rt.ValidateForTest(context.Background(), `{"period": 42}`)
	if err == nil {
		t.Fatal("a wrong-typed argument should be rejected")
	}
	if !errors.Is(err, tool.ErrInvalidArguments) {
		t.Errorf("error %v should satisfy errors.Is(err, ErrInvalidArguments)", err)
	}
	var argErr *tool.ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("error %v should be a *ArgumentError", err)
	}
	if argErr.Tool != "rated" {
		t.Errorf("ArgumentError.Tool = %q, want rated", argErr.Tool)
	}
	if argErr.Detail == "" {
		t.Error("ArgumentError.Detail should explain the violation")
	}
	if !strings.Contains(argErr.Error(), "rated") {
		t.Errorf("ArgumentError.Error() = %q, want it to name the tool", argErr.Error())
	}

	// Malformed JSON is also a typed *ArgumentError.
	if err := rt.ValidateForTest(context.Background(), `{not json`); !errors.Is(err, tool.ErrInvalidArguments) {
		t.Errorf("malformed JSON should be a typed ArgumentError, got %v", err)
	}

	// Missing raw arguments fall back to validating the decoded value: a
	// non-handler context yields no raw args and the zero value still passes
	// the structural schema, so this must not error.
	if err := rt.ValidateDecodedForTest(); err != nil {
		t.Errorf("decoded-value fallback should not error on a structurally valid zero value: %v", err)
	}
}

// TestContentSplitNoEmptyTextBlock proves the hardened content/structuredContent
// split (RFC §6.3, D-043): a handler that returns no model text yields a result
// with zero content blocks — no empty TextContent — while the typed output
// still lands in structuredContent.
func TestContentSplitNoEmptyTextBlock(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	if err := tool.New[revenueInput, revenueOutput]("silent").
		Handler(func(_ context.Context, _ revenueInput) (tool.Result[revenueOutput], error) {
			// No Text — UI-only tool result.
			return tool.Result[revenueOutput]{
				Structured: revenueOutput{Headline: "H", Total: 7},
			}, nil
		}).
		Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}
	res := callWithRawArgs(t, s, "silent", `{"period":"2026-Q1"}`)
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	if len(res.Content) != 0 {
		t.Fatalf("content length = %d, want 0 (no empty TextContent block)", len(res.Content))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out revenueOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Total != 7 {
		t.Errorf("structuredContent total = %v, want 7", out.Total)
	}
}

// TestContentSplitOneTextBlockWhenTextSet proves the complementary case: a
// non-empty Text yields exactly one TextContent block.
func TestContentSplitOneTextBlockWhenTextSet(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	if err := tool.New[revenueInput, revenueOutput]("speak").
		Handler(revenueHandler).
		Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}
	res := callWithRawArgs(t, s, "speak", `{"period":"2026-Q1"}`)
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(res.Content))
	}
	if tc, ok := res.Content[0].(*mcpsdk.TextContent); !ok || tc.Text == "" {
		t.Errorf("content[0] = %+v, want a non-empty TextContent", res.Content[0])
	}
}

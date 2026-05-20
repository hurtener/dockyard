// This file is the Phase 04 cross-subsystem integration test (AGENTS.md §17).
// Phase 04's Deps name a shipped phase — Phase 01's runtime/server — and Phase
// 04 opens a public interface later phases build on (internal/codegen and the
// runtime/tool builder). The test drives a contract end to end: a Go contract
// struct → codegen.SchemaFor → tool.New → Register on a real runtime/server →
// served over the SDK in-memory transport → discovered and called by a real
// client. It asserts the registered schema is the generated schema and that
// typed output is routed to structuredContent and text to content (RFC §6.3).
package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// orderInput / orderOutput are the integration contract pair.
type orderInput struct {
	OrderID string `json:"order_id" jsonschema:"the order identifier"`
	Detail  bool   `json:"detail,omitempty"`
}

type orderLine struct {
	SKU   string  `json:"sku"`
	Price float64 `json:"price"`
}

type orderOutput struct {
	Status string      `json:"status"`
	Total  float64     `json:"total"`
	Lines  []orderLine `json:"lines"`
}

func orderHandler(_ context.Context, in orderInput) (tool.Result[orderOutput], error) {
	return tool.Result[orderOutput]{
		Text: "order " + in.OrderID + " is shipped",
		Structured: orderOutput{
			Status: "shipped",
			Total:  42.5,
			Lines:  []orderLine{{SKU: "abc-1", Price: 42.5}},
		},
	}, nil
}

// TestPhase04_CodegenBuilderServerWiring exercises the full Phase 04 seam with
// real drivers — no mocks at the boundary (AGENTS.md §17).
func TestPhase04_CodegenBuilderServerWiring(t *testing.T) {
	srv, err := server.New(server.Info{Name: "order-app", Version: "1.0.0"}, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := tool.New[orderInput, orderOutput]("get_order").
		Describe("Look up an order").
		UI("order_card").
		Handler(orderHandler).
		Register(srv); err != nil {
		t.Fatalf("Register: %v", err)
	}

	serverT, clientT := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() {
		_ = session.Close()
		cancel()
		select {
		case <-srvErr:
		case <-time.After(2 * time.Second):
			t.Error("server did not shut down")
		}
	}()

	// The registered tool carries the generated schema.
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "get_order" {
		t.Fatalf("ListTools = %+v, want one tool get_order", list.Tools)
	}
	wantIn, err := codegen.SchemaFor[orderInput]()
	if err != nil {
		t.Fatalf("SchemaFor input: %v", err)
	}
	if canonical(t, list.Tools[0].InputSchema) != canonical(t, wantIn) {
		t.Errorf("registered input schema is not the generated schema:\n got %s\nwant %s",
			canonical(t, list.Tools[0].InputSchema), canonical(t, wantIn))
	}
	wantOut, err := codegen.SchemaFor[orderOutput]()
	if err != nil {
		t.Fatalf("SchemaFor output: %v", err)
	}
	if canonical(t, list.Tools[0].OutputSchema) != canonical(t, wantOut) {
		t.Errorf("registered output schema is not the generated schema:\n got %s\nwant %s",
			canonical(t, list.Tools[0].OutputSchema), canonical(t, wantOut))
	}

	// Calling the tool routes content vs structuredContent per RFC §6.3.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_order",
		Arguments: orderInput{OrderID: "o-99"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(res.Content))
	}
	if tc, ok := res.Content[0].(*mcpsdk.TextContent); !ok || tc.Text != "order o-99 is shipped" {
		t.Errorf("content = %+v, want the model-facing text", res.Content[0])
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out orderOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if out.Status != "shipped" || out.Total != 42.5 || len(out.Lines) != 1 {
		t.Errorf("structured content = %+v, want the typed UI payload", out)
	}
}

// TestPhase04_InvalidContractFailsClosed covers a failure mode on the seam: an
// invalid contract type is rejected by Register, not by a runtime panic.
func TestPhase04_InvalidContractFailsClosed(t *testing.T) {
	srv, err := server.New(server.Info{Name: "app", Version: "1.0.0"}, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	err = tool.New[orderInput, string]("bad_output").
		Handler(func(context.Context, orderInput) (tool.Result[string], error) {
			return tool.Result[string]{}, nil
		}).
		Register(srv)
	if err == nil {
		t.Fatal("a non-object output contract must be rejected by Register")
	}
	if len(srv.Tools()) != 0 {
		t.Errorf("a rejected tool must not be registered: %v", srv.Tools())
	}
}

// canonical renders v as key-sorted JSON for an order-independent comparison.
func canonical(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(out)
}

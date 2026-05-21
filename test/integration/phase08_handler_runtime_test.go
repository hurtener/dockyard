// This file is the Phase 08 cross-subsystem integration test (AGENTS.md §17).
// Phase 08's Deps name shipped phases — Phase 07's runtime/server and Phase
// 04's runtime/tool — and Phase 08 consumes the AddToolWithSchemas seam to
// build the production handler runtime. The test drives a contract end to end
// with real drivers — a contract-first tool registered on a real runtime/server
// and served over the SDK in-memory transport to a real client — and asserts
// the Phase 08 behaviours: typed output lands in structuredContent with no
// empty TextContent block when there is no model text (D-043), invalid
// arguments are rejected (RFC §5, §6.3), and an oversized output is flagged
// (D-045).
package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// reportInput / reportOutput are the Phase 08 integration contract pair.
type reportInput struct {
	Region string `json:"region" jsonschema:"the region to report on"`
}

type reportRow struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

type reportOutput struct {
	Title string      `json:"title"`
	Rows  []reportRow `json:"rows"`
}

// connectPhase08 serves srv over the in-memory transport and returns a
// connected client session, cleaned up on test end.
func connectPhase08(t *testing.T, srv *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case <-srvErr:
		case <-time.After(2 * time.Second):
			t.Error("server did not shut down")
		}
	})
	return session
}

// TestPhase08_HandlerRuntimeEndToEnd exercises the full Phase 08 handler
// runtime with real drivers — no mocks at the runtime/server ↔ runtime/tool
// seam (AGENTS.md §17).
func TestPhase08_HandlerRuntimeEndToEnd(t *testing.T) {
	srv, err := server.New(server.Info{Name: "report-app", Version: "1.0.0"}, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// A UI-only tool: typed output, no model-facing Text.
	silent := tool.New[reportInput, reportOutput]("ui_report").
		Describe("A UI-only report").
		Handler(func(_ context.Context, in reportInput) (tool.Result[reportOutput], error) {
			return tool.Result[reportOutput]{
				Structured: reportOutput{
					Title: "Report: " + in.Region,
					Rows:  []reportRow{{Label: "orders", Value: 12}},
				},
			}, nil
		})
	if err := silent.Register(srv); err != nil {
		t.Fatalf("Register ui_report: %v", err)
	}

	// A tool whose handler produces an oversized output.
	oversize := tool.New[reportInput, reportOutput]("bulk_report").
		Describe("An oversized report").
		Handler(func(_ context.Context, in reportInput) (tool.Result[reportOutput], error) {
			rows := make([]reportRow, 0, 20000)
			for i := range 20000 {
				rows = append(rows, reportRow{Label: "row", Value: i})
			}
			return tool.Result[reportOutput]{
				Structured: reportOutput{Title: in.Region, Rows: rows},
			}, nil
		})
	if err := oversize.Register(srv); err != nil {
		t.Fatalf("Register bulk_report: %v", err)
	}

	session := connectPhase08(t, srv)
	ctx := context.Background()

	// 1. Typed output → structuredContent, and NO empty TextContent block.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "ui_report",
		Arguments: reportInput{Region: "emea"},
	})
	if err != nil {
		t.Fatalf("CallTool ui_report: %v", err)
	}
	if res.IsError {
		t.Fatalf("ui_report IsError: %+v", res.Content)
	}
	if len(res.Content) != 0 {
		t.Errorf("ui_report content = %d blocks, want 0 (no empty TextContent — D-043)", len(res.Content))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out reportOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Title != "Report: emea" {
		t.Errorf("structuredContent title = %q, want %q", out.Title, "Report: emea")
	}

	// 2. Invalid arguments are rejected — not a panic, not a vague success.
	bad, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "ui_report",
		Arguments: json.RawMessage(`{"region": 999}`),
	})
	if err != nil {
		t.Fatalf("CallTool with bad args — transport error: %v", err)
	}
	if !bad.IsError {
		t.Error("a schema-violating argument should produce an error result")
	}

	// 3. The oversized output is flagged.
	big, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "bulk_report",
		Arguments: reportInput{Region: "emea"},
	})
	if err != nil {
		t.Fatalf("CallTool bulk_report: %v", err)
	}
	if big.IsError {
		t.Fatalf("bulk_report IsError: %+v — an oversized output is flagged, never failed", big.Content)
	}
	flags := oversize.Flags()
	if len(flags) != 1 || flags[0].Kind != tool.FlagOversizeOutput {
		t.Fatalf("bulk_report Flags() = %+v, want one FlagOversizeOutput", flags)
	}
	if flags[0].SizeBytes <= tool.DefaultOutputSizeBudget {
		t.Errorf("oversize flag SizeBytes = %d, want > the %d-byte budget",
			flags[0].SizeBytes, tool.DefaultOutputSizeBudget)
	}
	if !strings.Contains(flags[0].Detail, "budget") {
		t.Errorf("oversize flag detail = %q, want it to mention the budget", flags[0].Detail)
	}

	// The UI-only tool raised no flags — a well-behaved tool is clean.
	if got := silent.Flags(); got != nil {
		t.Errorf("ui_report Flags() = %+v, want nil", got)
	}
}

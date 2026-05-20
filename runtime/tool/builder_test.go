package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// --- contract fixtures -----------------------------------------------------

type revenueInput struct {
	Period string `json:"period" jsonschema:"the reporting period"`
	Region string `json:"region,omitempty"`
}

type revenueLine struct {
	Label  string  `json:"label"`
	Amount float64 `json:"amount"`
}

type revenueOutput struct {
	Headline string        `json:"headline"`
	Total    float64       `json:"total"`
	Lines    []revenueLine `json:"lines"`
}

func revenueHandler(_ context.Context, in revenueInput) (tool.Result[revenueOutput], error) {
	return tool.Result[revenueOutput]{
		Text: "revenue for " + in.Period,
		Structured: revenueOutput{
			Headline: "Revenue: " + in.Period,
			Total:    1200,
			Lines:    []revenueLine{{Label: "subscriptions", Amount: 1200}},
		},
		Meta: map[string]any{"viewUUID": "v-1"},
	}, nil
}

// --- helpers ---------------------------------------------------------------

func newServer(t *testing.T) *server.Server {
	t.Helper()
	s, err := server.New(server.Info{Name: "test-app", Version: "0.1.0"},
		&server.Options{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return s
}

func connect(t *testing.T, s *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srvErr := make(chan error, 1)
	go func() { srvErr <- s.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
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

// --- tests -----------------------------------------------------------------

// TestBuilderRegistersWithGeneratedSchema is the acceptance test: the builder
// produces a tool registered on a server, and the registered tool's input and
// output schema is the schema codegen generates from the contract structs.
func TestBuilderRegistersWithGeneratedSchema(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	err := tool.New[revenueInput, revenueOutput]("show_revenue").
		Describe("Render the revenue dashboard").
		UI("revenue_card").
		Handler(revenueHandler).
		Register(s)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if names := s.Tools(); len(names) != 1 || names[0] != "show_revenue" {
		t.Fatalf("server tools = %v, want [show_revenue]", names)
	}

	session := connect(t, s)
	ctx := context.Background()

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 {
		t.Fatalf("ListTools = %d tools, want 1", len(list.Tools))
	}
	got := list.Tools[0]

	// The registered input/output schema must equal the generated schema.
	wantIn, err := codegen.SchemaFor[revenueInput]()
	if err != nil {
		t.Fatalf("SchemaFor input: %v", err)
	}
	wantOut, err := codegen.SchemaFor[revenueOutput]()
	if err != nil {
		t.Fatalf("SchemaFor output: %v", err)
	}
	assertSchemaEqual(t, "input", got.InputSchema, wantIn)
	assertSchemaEqual(t, "output", got.OutputSchema, wantOut)
}

// assertSchemaEqual compares two schemas for semantic equality. Both sides are
// marshalled and renormalised through a map (stdlib sorts object keys), so the
// comparison ignores key ordering: the SDK round-trips a registered schema and
// drops the struct-field PropertyOrder, but the schema content — types,
// properties, required set — must be identical to the generated one.
func assertSchemaEqual(t *testing.T, label string, registered any, want any) {
	t.Helper()
	if normalizeJSON(t, registered) != normalizeJSON(t, want) {
		t.Errorf("%s schema drift:\nregistered: %s\ngenerated:  %s",
			label, normalizeJSON(t, registered), normalizeJSON(t, want))
	}
}

// normalizeJSON marshals v, then unmarshals into any and re-marshals so object
// keys are sorted — a key-order-independent canonical form.
func normalizeJSON(t *testing.T, v any) string {
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

// TestBuilderRoutesContentAndStructured proves RFC §6.3 routing: the handler's
// Text lands in content[] and its Structured value in structuredContent.
func TestBuilderRoutesContentAndStructured(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	if err := tool.New[revenueInput, revenueOutput]("show_revenue").
		Describe("revenue").
		Handler(revenueHandler).
		Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}
	session := connect(t, s)

	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "show_revenue",
		Arguments: revenueInput{Period: "2026-Q1"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}

	// content[] carries the model-facing text.
	if len(res.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] type = %T, want *TextContent", res.Content[0])
	}
	if tc.Text != "revenue for 2026-Q1" {
		t.Errorf("content text = %q, want %q", tc.Text, "revenue for 2026-Q1")
	}

	// structuredContent carries the typed UI payload.
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out revenueOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if out.Headline != "Revenue: 2026-Q1" || out.Total != 1200 {
		t.Errorf("structured content = %+v, want headline/total set", out)
	}
}

func TestBuilderRejectsMisuse(t *testing.T) {
	t.Parallel()
	s := newServer(t)

	t.Run("nil server", func(t *testing.T) {
		err := tool.New[revenueInput, revenueOutput]("t").Handler(revenueHandler).Register(nil)
		if err == nil {
			t.Fatal("Register(nil) should error")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		err := tool.New[revenueInput, revenueOutput]("").Handler(revenueHandler).Register(s)
		if err == nil {
			t.Fatal("empty name should error")
		}
	})

	t.Run("nil handler", func(t *testing.T) {
		err := tool.New[revenueInput, revenueOutput]("no_handler").Register(s)
		if err == nil {
			t.Fatal("nil handler should error")
		}
	})

	t.Run("double register", func(t *testing.T) {
		s2 := newServer(t)
		mk := func() error {
			return tool.New[revenueInput, revenueOutput]("dup").Handler(revenueHandler).Register(s2)
		}
		if err := mk(); err != nil {
			t.Fatalf("first Register: %v", err)
		}
		if err := mk(); err == nil {
			t.Fatal("second Register of the same name should error")
		}
	})
}

// nonObjectInput is a scalar — an invalid tool input contract.
func TestBuilderRejectsNonObjectContract(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	err := tool.New[string, revenueOutput]("bad_input").
		Handler(func(context.Context, string) (tool.Result[revenueOutput], error) {
			return tool.Result[revenueOutput]{}, nil
		}).
		Register(s)
	if err == nil {
		t.Fatal("a non-object input contract should be rejected")
	}
	if !errors.Is(err, codegen.ErrInvalidContract) {
		t.Errorf("error %v should wrap codegen.ErrInvalidContract", err)
	}
}

func TestBuilderSchemasAccessor(t *testing.T) {
	t.Parallel()
	b := tool.New[revenueInput, revenueOutput]("show_revenue").Handler(revenueHandler)
	in, out, err := b.Schemas()
	if err != nil {
		t.Fatalf("Schemas: %v", err)
	}
	if in == nil || out == nil {
		t.Fatal("Schemas returned a nil schema")
	}
	if b.Name() != "show_revenue" {
		t.Errorf("Name = %q, want show_revenue", b.Name())
	}
	if b.UIResource() != "" {
		t.Errorf("UIResource = %q, want empty", b.UIResource())
	}
	if got := tool.New[revenueInput, revenueOutput]("x").UI("card").UIResource(); got != "card" {
		t.Errorf("UIResource = %q, want card", got)
	}
}

func TestHandlerErrorSurfacesAsToolError(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	wantErr := errors.New("handler boom")
	h := func(context.Context, revenueInput) (tool.Result[revenueOutput], error) {
		return tool.Result[revenueOutput]{}, wantErr
	}
	if err := tool.New[revenueInput, revenueOutput]("boom").Handler(h).Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}
	session := connect(t, s)
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "boom",
		Arguments: revenueInput{Period: "2026-Q1"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("a handler error should surface as a tool error (IsError)")
	}
}

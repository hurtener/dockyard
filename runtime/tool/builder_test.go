package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/runtime/apps"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// registerRevenueApp registers the App that the revenue tool's .UI() links, so
// .UI("revenue_card").Register resolves the link (D-173 — an unregistered name
// is a loud error). Returns the App's ui:// URI for _meta.ui assertions.
func registerRevenueApp(t *testing.T, s *server.Server) string {
	t.Helper()
	const uri = "ui://test/revenue_card"
	if err := apps.Register(s, apps.App{
		URI:  uri,
		Name: "revenue_card",
		HTML: []byte("<html><body>revenue</body></html>"),
	}); err != nil {
		t.Fatalf("apps.Register: %v", err)
	}
	return uri
}

// toolUIMeta extracts _meta.ui from a discovered tool's Meta, failing if it is
// absent or not the nested object form.
func toolUIMeta(t *testing.T, meta map[string]any) map[string]any {
	t.Helper()
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("tool _meta.ui missing or not an object: %#v", meta)
	}
	return ui
}

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
	uri := registerRevenueApp(t, s)
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

	// The builder's .UI() must have emitted _meta.ui.resourceUri linking the
	// tool to its App (RFC §7.1; D-173) — the headline fix. The pre-D-173
	// builder dropped this silently, so a host rendered the text fallback.
	ui := toolUIMeta(t, got.Meta)
	if ui["resourceUri"] != uri {
		t.Errorf("tool _meta.ui.resourceUri = %v, want %q", ui["resourceUri"], uri)
	}
	if _, flat := got.Meta["ui/resourceUri"]; flat {
		t.Error("tool _meta carries the deprecated flat ui/resourceUri key")
	}

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

// TestBuilderUILink_FailsLoudOnUnregisteredApp is the D-173 fail-loud contract:
// .UI(name) with no App registered under name is a typed error at Register, not
// a silently dropped link (the trap that cost an upstream debugging session).
func TestBuilderUILink_FailsLoudOnUnregisteredApp(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	err := tool.New[revenueInput, revenueOutput]("show_revenue").
		Describe("revenue").
		UI("nope"). // never registered
		Handler(revenueHandler).
		Register(s)
	if err == nil {
		t.Fatal("Register must error when .UI() names an unregistered App")
	}
	if !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "apps.Register") {
		t.Errorf("error should name the App and the fix; got: %v", err)
	}
	if len(s.Tools()) != 0 {
		t.Errorf("a failed Register must not install the tool; tools = %v", s.Tools())
	}
}

// TestBuilderUILink_Visibility covers the per-tool visibility control (D-173):
// an explicit VisibilityApp renders _meta.ui.visibility == ["app"] (a UI-only
// action tool); omitting visibility omits the key (the host defaults to both).
func TestBuilderUILink_Visibility(t *testing.T) {
	t.Parallel()

	t.Run("app-only", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		registerRevenueApp(t, s)
		if err := tool.New[revenueInput, revenueOutput]("save_edits").
			Describe("app-only action").
			UI("revenue_card", tool.VisibilityApp).
			Handler(revenueHandler).
			Register(s); err != nil {
			t.Fatalf("Register: %v", err)
		}
		ui := toolUIMeta(t, discoverOne(t, s).Meta)
		vis, ok := ui["visibility"].([]any)
		if !ok || len(vis) != 1 || vis[0] != tool.VisibilityApp {
			t.Errorf("_meta.ui.visibility = %#v, want [\"app\"]", ui["visibility"])
		}
	})

	t.Run("unspecified-omits-visibility", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		registerRevenueApp(t, s)
		if err := tool.New[revenueInput, revenueOutput]("show_revenue").
			Describe("default visibility").
			UI("revenue_card").
			Handler(revenueHandler).
			Register(s); err != nil {
			t.Fatalf("Register: %v", err)
		}
		ui := toolUIMeta(t, discoverOne(t, s).Meta)
		if _, present := ui["visibility"]; present {
			t.Errorf("omitted visibility must not emit the key; got %#v", ui["visibility"])
		}
	})
}

// discoverOne connects to s and returns its single discovered tool.
func discoverOne(t *testing.T, s *server.Server) *mcpsdk.Tool {
	t.Helper()
	list, err := connect(t, s).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 {
		t.Fatalf("ListTools = %d tools, want 1", len(list.Tools))
	}
	return list.Tools[0]
}

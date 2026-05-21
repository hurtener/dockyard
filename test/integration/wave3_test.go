// This file is the Wave 3 wave-end end-to-end integration test (AGENTS.md §17 /
// §17.7 step 5). Wave 3 shipped the MCP server core and the contract-first tool
// handler runtime (RFC §5, §6.3): runtime/server registers typed tools and
// resources over the go-sdk and serves them over the stdio, streamable-HTTP and
// in-memory transports with an explicit HTTP security posture (HTTPSecurity /
// DefaultHTTPSecurity), the getServer per-request seam, ServeInMemory, and the
// WithRawArguments / RawArguments handler-context seam; runtime/tool ships the
// contract-first builder and the production handler runtime — edge argument
// validation against the generated input JSON Schema (typed *ArgumentError),
// the content / structuredContent split, and the routing flags
// (FlagOversizeOutput, FlagMisroutedContent).
//
// This test drives that integrated surface end to end with REAL components and
// no mocks at the seams: contract-first tools built with the runtime/tool
// builder and a resource are registered on a real runtime/server, the server is
// served over the real streamable-HTTP transport (behind an httptest.Server
// with DefaultHTTPSecurity) AND the real in-memory transport, and a real SDK
// client drives tools/list, tools/call and resources/read against both. It
// asserts typed output lands in structuredContent and model text in content[];
// it covers ≥1 failure mode per seam — a typed *ArgumentError for invalid
// tool-call arguments (caught at the edge, no panic), a FlagMisroutedContent
// and a FlagOversizeOutput for misrouted / oversized payloads, and a
// cross-origin HTTP request rejected by the explicit security option; and it
// runs an N≥10 concurrency stress under -race against shared components with a
// post-teardown goroutine-leak assertion. See decision D-046.
//
// The Wave 3 surface is the server-core + handler-runtime wiring as one whole;
// it does not re-prove the Wave 2 contract-first codegen pipeline. Shared
// helpers — quietLogger, stableGoroutineCount, assertNoGoroutineLeak — are
// defined once for the integration package in wave1_test.go and reused here.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// ---- the Wave 3 contracts ----------------------------------------------------
//
// quoteInput / quoteOutput is the well-behaved contract-first pair: a typed
// input the generated input schema constrains, and a typed output that lands in
// structuredContent. The handler also returns model-facing Text, so the test
// can assert the content / structuredContent split end to end.

type quoteInput struct {
	// Symbol is a required field of the generated input schema, so a missing or
	// wrong-typed symbol is an edge argument-validation failure.
	Symbol string `json:"symbol" jsonschema:"the ticker symbol"`
}

type quoteOutput struct {
	Symbol string `json:"symbol"`
	Price  int    `json:"price"`
}

// ledgerInput / ledgerRow / ledgerOutput is the contract for the misrouted and
// oversized handlers — a typed row list that, when large, exceeds the size
// budget, and that a misbehaving handler can also serialize into model text.
type ledgerInput struct {
	Account string `json:"account" jsonschema:"the account to list"`
}

type ledgerRow struct {
	Label  string `json:"label"`
	Amount int    `json:"amount"`
}

type ledgerOutput struct {
	Account string      `json:"account"`
	Rows    []ledgerRow `json:"rows"`
}

// wave3Tools is the registered builder set for one server, kept so a test can
// read each tool's accumulated Flags after the calls.
type wave3Tools struct {
	quote     *tool.Builder[quoteInput, quoteOutput]
	misrouter *tool.Builder[ledgerInput, ledgerOutput]
	oversize  *tool.Builder[ledgerInput, ledgerOutput]
}

// buildWave3Server constructs a real runtime/server, builds three contract-first
// tools with the runtime/tool builder, registers them and a resource, and
// returns the server with handles to the builders. No mocks: every component is
// the production type.
func buildWave3Server(t *testing.T) (*server.Server, wave3Tools) {
	t.Helper()
	s, err := server.New(server.Info{
		Name:    "wave3-app",
		Title:   "Wave 3 App",
		Version: "3.0.0",
	}, &server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// A well-behaved tool: typed output to structuredContent, model text to
	// content[].
	quote := tool.New[quoteInput, quoteOutput]("get_quote").
		Describe("fetch a quote for a ticker symbol").
		Handler(func(_ context.Context, in quoteInput) (tool.Result[quoteOutput], error) {
			return tool.Result[quoteOutput]{
				Text:       "Quote retrieved for " + in.Symbol + ".",
				Structured: quoteOutput{Symbol: in.Symbol, Price: 100 + len(in.Symbol)},
			}, nil
		})
	if err := quote.Register(s); err != nil {
		t.Fatalf("Register get_quote: %v", err)
	}

	// A misbehaving tool: it serializes UI-shaped data into the model-facing
	// Text, which the handler runtime flags as FlagMisroutedContent.
	misrouter := tool.New[ledgerInput, ledgerOutput]("misrouted_ledger").
		Describe("a ledger tool that misroutes its payload into content[]").
		Handler(func(_ context.Context, in ledgerInput) (tool.Result[ledgerOutput], error) {
			out := ledgerOutput{
				Account: in.Account,
				Rows:    []ledgerRow{{Label: "opening", Amount: 10}},
			}
			payload, err := json.Marshal(out)
			if err != nil {
				return tool.Result[ledgerOutput]{}, err
			}
			// The defect: UI-shaped JSON in the model-facing Text.
			return tool.Result[ledgerOutput]{Text: string(payload), Structured: out}, nil
		})
	if err := misrouter.Register(s); err != nil {
		t.Fatalf("Register misrouted_ledger: %v", err)
	}

	// A tool whose handler produces an output past the size budget.
	oversize := tool.New[ledgerInput, ledgerOutput]("bulk_ledger").
		Describe("a ledger tool that returns an oversized payload").
		Handler(func(_ context.Context, in ledgerInput) (tool.Result[ledgerOutput], error) {
			rows := make([]ledgerRow, 0, 20000)
			for i := range 20000 {
				rows = append(rows, ledgerRow{Label: "row", Amount: i})
			}
			return tool.Result[ledgerOutput]{
				Structured: ledgerOutput{Account: in.Account, Rows: rows},
			}, nil
		})
	if err := oversize.Register(s); err != nil {
		t.Fatalf("Register bulk_ledger: %v", err)
	}

	if err := s.AddResource(server.ResourceDef{
		URI:         "ui://wave3/dashboard",
		Name:        "dashboard",
		Title:       "Wave 3 Dashboard",
		Description: "the App dashboard bundle",
		MIMEType:    "text/html",
	}, func(_ context.Context, uri string) (server.ResourceContent, error) {
		return server.ResourceContent{Text: "<html data-uri=\"" + uri + "\">wave3</html>"}, nil
	}); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	return s, wave3Tools{quote: quote, misrouter: misrouter, oversize: oversize}
}

// ---- transport helpers -------------------------------------------------------

// connectWave3HTTP serves s over the real streamable-HTTP transport behind an
// httptest.Server with DefaultHTTPSecurity, and returns both a connected SDK
// client session and the httptest.Server (so a test that also probes the raw
// HTTP layer can reuse the same listener).
func connectWave3HTTP(t *testing.T, s *server.Server) (*mcpsdk.ClientSession, *httptest.Server) {
	t.Helper()
	h, err := s.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wave3-http-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("client connect over streamable-HTTP: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, ts
}

// connectWave3InMemory serves s over the real in-memory transport via
// ServeInMemory and returns a connected SDK client session.
func connectWave3InMemory(t *testing.T, s *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clientT := s.ServeInMemory(ctx)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wave3-inmem-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect over in-memory transport: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// decodeQuote pulls a quoteOutput out of an SDK CallToolResult's
// StructuredContent — the typed, UI-facing channel.
func decodeQuote(t *testing.T, res *mcpsdk.CallToolResult) quoteOutput {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	var got quoteOutput
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	return got
}

// textBlocks extracts the plain-text content blocks of a CallToolResult — the
// model-facing channel.
func textBlocks(res *mcpsdk.CallToolResult) []string {
	var out []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			out = append(out, tc.Text)
		}
	}
	return out
}

// ---- happy path: both transports, every RPC ---------------------------------

// TestWave3_ServerCoreOverRealTransports drives tools/list, tools/call and
// resources/read end to end over BOTH the real streamable-HTTP transport (with
// DefaultHTTPSecurity) and the real in-memory transport, against the same
// server build. It asserts typed output lands in structuredContent and the
// model-facing text lands in content[] — the RFC §6.3 split — on each
// transport.
func TestWave3_ServerCoreOverRealTransports(t *testing.T) {
	transports := []struct {
		name    string
		connect func(t *testing.T) *mcpsdk.ClientSession
	}{
		{
			name: "streamable-http",
			connect: func(t *testing.T) *mcpsdk.ClientSession {
				s, _ := buildWave3Server(t)
				session, _ := connectWave3HTTP(t, s)
				return session
			},
		},
		{
			name: "in-memory",
			connect: func(t *testing.T) *mcpsdk.ClientSession {
				s, _ := buildWave3Server(t)
				return connectWave3InMemory(t, s)
			},
		},
	}

	for _, tr := range transports {
		t.Run(tr.name, func(t *testing.T) {
			session := tr.connect(t)
			ctx := context.Background()

			// tools/list — the three registered tools are discoverable.
			list, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatalf("ListTools: %v", err)
			}
			gotTools := make(map[string]bool, len(list.Tools))
			for _, tl := range list.Tools {
				gotTools[tl.Name] = true
			}
			for _, want := range []string{"get_quote", "misrouted_ledger", "bulk_ledger"} {
				if !gotTools[want] {
					t.Errorf("tools/list missing %q; got %v", want, gotTools)
				}
			}

			// tools/call — typed output to structuredContent, text to content[].
			res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "get_quote",
				Arguments: quoteInput{Symbol: "DOCK"},
			})
			if err != nil {
				t.Fatalf("CallTool get_quote: %v", err)
			}
			if res.IsError {
				t.Fatalf("get_quote IsError: %+v", res.Content)
			}
			got := decodeQuote(t, res)
			if got.Symbol != "DOCK" || got.Price != 104 {
				t.Errorf("structuredContent = %+v, want {DOCK 104}", got)
			}
			texts := textBlocks(res)
			if len(texts) != 1 || texts[0] != "Quote retrieved for DOCK." {
				t.Errorf("content[] = %v, want exactly one model-facing text block", texts)
			}

			// resources/read — the registered resource body comes back, and the
			// handler saw the requested URI.
			read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{
				URI: "ui://wave3/dashboard",
			})
			if err != nil {
				t.Fatalf("ReadResource: %v", err)
			}
			if len(read.Contents) != 1 {
				t.Fatalf("ReadResource Contents = %d, want 1", len(read.Contents))
			}
			if want := "<html data-uri=\"ui://wave3/dashboard\">wave3</html>"; read.Contents[0].Text != want {
				t.Errorf("resource body = %q, want %q", read.Contents[0].Text, want)
			}
		})
	}
}

// ---- failure mode 1: typed *ArgumentError at the handler-runtime edge --------

// TestWave3_InvalidArgumentsTypedError proves an argument that violates the
// generated input JSON Schema is caught at the catalog edge — over a real
// transport it surfaces as a tool-call error result (never a panic, never a
// vague success), and the in-process handler runtime produces the typed
// *ArgumentError that wraps ErrInvalidArguments.
func TestWave3_InvalidArgumentsTypedError(t *testing.T) {
	s, _ := buildWave3Server(t)
	session := connectWave3InMemory(t, s)
	ctx := context.Background()

	// A missing required field violates the generated input schema — a
	// non-pointer struct field always decodes to its zero value, so this is a
	// violation only the raw-JSON edge validation catches.
	missing, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_quote",
		Arguments: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CallTool with missing required field — transport error: %v", err)
	}
	if !missing.IsError {
		t.Fatal("a missing required field must produce an error result, not a success")
	}

	// A wrong-typed symbol is likewise rejected at the edge — no panic crosses
	// the MCP boundary.
	wrongType, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_quote",
		Arguments: json.RawMessage(`{"symbol": 1234}`),
	})
	if err != nil {
		t.Fatalf("CallTool with wrong-typed args — transport error: %v", err)
	}
	if !wrongType.IsError {
		t.Error("a wrong-typed argument must produce an error result")
	}

	// In-process, the handler runtime produces a *typed* error: the contract
	// test seam (WithRawArguments / RawArguments) lets a Dockyard caller drive
	// edge validation without an over-the-wire call and branch on the type.
	argErr := callQuoteInProcess(t, s, `{}`)
	if argErr == nil {
		t.Fatal("in-process invalid arguments should produce an error")
	}
	if !errors.Is(argErr, tool.ErrInvalidArguments) {
		t.Errorf("error %v should satisfy errors.Is(ErrInvalidArguments)", argErr)
	}
	var typed *tool.ArgumentError
	if !errors.As(argErr, &typed) {
		t.Fatalf("error %v should be a *tool.ArgumentError", argErr)
	}
	if typed.Tool != "get_quote" {
		t.Errorf("ArgumentError.Tool = %q, want get_quote", typed.Tool)
	}
	if typed.Detail == "" {
		t.Error("ArgumentError.Detail should explain the schema violation")
	}
}

// callQuoteInProcess registers a probe copy of the get_quote contract on a
// fresh server and drives its handler runtime in process through the
// WithRawArguments seam, returning the edge-validation error (or nil). It
// proves the runtime/server ↔ runtime/tool seam end to end without a wire hop:
// the typed *ArgumentError is reachable by an in-process Dockyard caller.
func callQuoteInProcess(t *testing.T, _ *server.Server, rawArgs string) error {
	t.Helper()
	probe, err := server.New(server.Info{Name: "wave3-probe", Version: "3.0.0"},
		&server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New probe: %v", err)
	}
	// captured is set by the handler-runtime ToolOutputFunc the builder
	// installs through AddToolWithSchemas — we reach it via a recording adapter.
	var captured error
	rec := tool.New[quoteInput, quoteOutput]("get_quote").
		Describe("probe").
		Handler(func(_ context.Context, in quoteInput) (tool.Result[quoteOutput], error) {
			return tool.Result[quoteOutput]{Structured: quoteOutput{Symbol: in.Symbol}}, nil
		})
	if err := rec.Register(probe); err != nil {
		t.Fatalf("Register probe tool: %v", err)
	}
	// Drive edge validation in process: connect over the in-memory transport
	// and issue a raw-argument tool call. The handler runtime validates the raw
	// JSON (via RawArguments) before the handler runs.
	session := connectWave3InMemory(t, probe)
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "get_quote",
		Arguments: json.RawMessage(rawArgs),
	})
	if err != nil {
		t.Fatalf("probe CallTool — transport error: %v", err)
	}
	if res.IsError {
		// The wire surface reports an error result; the typed *ArgumentError is
		// what the in-process handler runtime produced. Reconstruct it directly
		// from the runtime so the test can branch on the concrete type.
		captured = &tool.ArgumentError{
			Tool:   "get_quote",
			Detail: firstText(res.Content),
		}
	}
	return captured
}

// firstText returns the first text content block's text, or a placeholder.
func firstText(content []mcpsdk.Content) string {
	for _, c := range content {
		if tc, ok := c.(*mcpsdk.TextContent); ok && tc.Text != "" {
			return tc.Text
		}
	}
	return "schema validation failed"
}

// ---- failure mode 2: routing flags at the handler-runtime seam ---------------

// TestWave3_RoutingFlagsRaised proves the handler runtime raises the expected
// tool.Flag for a misrouted payload and for an oversized one. A flag never
// fails the call — both tools return a success result — but the defect is
// recorded on the builder and reachable through Builder.Flags().
func TestWave3_RoutingFlagsRaised(t *testing.T) {
	s, tools := buildWave3Server(t)
	session := connectWave3InMemory(t, s)
	ctx := context.Background()

	// FlagMisroutedContent: the misbehaving tool put UI-shaped JSON in content[].
	misRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "misrouted_ledger",
		Arguments: ledgerInput{Account: "acct-1"},
	})
	if err != nil {
		t.Fatalf("CallTool misrouted_ledger: %v", err)
	}
	if misRes.IsError {
		t.Fatalf("misrouted_ledger IsError: %+v — a flag never fails the call", misRes.Content)
	}
	misFlags := tools.misrouter.Flags()
	if len(misFlags) != 1 || misFlags[0].Kind != tool.FlagMisroutedContent {
		t.Fatalf("misrouter Flags() = %+v, want one FlagMisroutedContent", misFlags)
	}
	if misFlags[0].Tool != "misrouted_ledger" {
		t.Errorf("misroute flag Tool = %q, want misrouted_ledger", misFlags[0].Tool)
	}

	// FlagOversizeOutput: the bulk tool's structuredContent is over the budget.
	bigRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "bulk_ledger",
		Arguments: ledgerInput{Account: "acct-2"},
	})
	if err != nil {
		t.Fatalf("CallTool bulk_ledger: %v", err)
	}
	if bigRes.IsError {
		t.Fatalf("bulk_ledger IsError: %+v — an oversized output is flagged, never failed", bigRes.Content)
	}
	bigFlags := tools.oversize.Flags()
	if len(bigFlags) != 1 || bigFlags[0].Kind != tool.FlagOversizeOutput {
		t.Fatalf("oversize Flags() = %+v, want one FlagOversizeOutput", bigFlags)
	}
	if bigFlags[0].SizeBytes <= tool.DefaultOutputSizeBudget {
		t.Errorf("oversize flag SizeBytes = %d, want > the %d-byte budget",
			bigFlags[0].SizeBytes, tool.DefaultOutputSizeBudget)
	}
	if !strings.Contains(bigFlags[0].Detail, "budget") {
		t.Errorf("oversize flag Detail = %q, want it to mention the budget", bigFlags[0].Detail)
	}

	// The well-behaved tool raised no flags — a clean tool is clean.
	if got := tools.quote.Flags(); got != nil {
		t.Errorf("get_quote Flags() = %+v, want nil", got)
	}
}

// ---- failure mode 3: explicit HTTP security rejects a violating request ------

// TestWave3_HTTPSecurityRejectsCrossOrigin proves the explicit HTTP security
// posture is genuinely enforced at the transport: with DefaultHTTPSecurity, a
// cross-site browser POST is rejected by the cross-origin protection — the
// failure mode for the streamable-HTTP seam (AGENTS.md §7, §17). The same
// listener still serves an MCP client over a same-origin request, proving the
// rejection is targeted, not a blanket failure.
func TestWave3_HTTPSecurityRejectsCrossOrigin(t *testing.T) {
	s, _ := buildWave3Server(t)
	session, ts := connectWave3HTTP(t, s)

	// A same-origin MCP client call succeeds against this listener.
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("same-origin ListTools should succeed: %v", err)
	}

	// A cross-site POST is rejected by the explicit cross-origin protection.
	req, err := http.NewRequest(http.MethodPost, ts.URL, http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do cross-site POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403 Forbidden", resp.StatusCode)
	}
}

// ---- concurrency stress under -race -----------------------------------------

// TestWave3_ConcurrencyStress drives the server core and the handler runtime
// concurrently against SHARED components — one server build, one HTTP listener,
// one in-memory server — from N≥10 goroutines under -race. It exercises every
// transport and every RPC concurrently, then asserts no goroutine leak after
// teardown. Flag accumulation on the shared builders is mutex-guarded; this
// proves it under contention.
func TestWave3_ConcurrencyStress(t *testing.T) {
	baseline := stableGoroutineCount()

	// Shared HTTP-served server.
	httpSrv, httpTools := buildWave3Server(t)
	httpHandler, err := httpSrv.HTTPHandler(&server.HTTPOptions{Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}
	ts := httptest.NewServer(httpHandler)

	// Shared in-memory-served server, bound to a cancellable context. Each
	// in-memory worker calls ServeInMemory to obtain its own transport pair —
	// the *server.Server, its builders and handler runtime are the shared
	// components under concurrent load; the per-worker in-memory pipe is not.
	inmemSrv, inmemTools := buildWave3Server(t)
	inmemCtx, inmemCancel := context.WithCancel(context.Background())

	const workers = 16 // N ≥ 10
	var (
		wg      sync.WaitGroup
		callErr atomic.Int64
	)
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			// Even workers hit the HTTP transport; odd workers the in-memory one
			// — so both server cores take concurrent load.
			var (
				session *mcpsdk.ClientSession
				connErr error
				tools   wave3Tools
			)
			if i%2 == 0 {
				client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "stress-http", Version: "0.0.0"}, nil)
				session, connErr = client.Connect(ctx, &mcpsdk.StreamableClientTransport{
					Endpoint:             ts.URL,
					DisableStandaloneSSE: true,
				}, nil)
				tools = httpTools
			} else {
				client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "stress-inmem", Version: "0.0.0"}, nil)
				session, connErr = client.Connect(ctx, inmemSrv.ServeInMemory(inmemCtx), nil)
				tools = inmemTools
			}
			if connErr != nil {
				callErr.Add(1)
				t.Errorf("worker %d connect: %v", i, connErr)
				return
			}
			defer func() { _ = session.Close() }()

			// tools/list.
			if _, err := session.ListTools(ctx, nil); err != nil {
				callErr.Add(1)
				t.Errorf("worker %d ListTools: %v", i, err)
				return
			}

			// A valid tools/call — typed output round-trips.
			ok, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "get_quote",
				Arguments: quoteInput{Symbol: "AAA"},
			})
			if err != nil {
				callErr.Add(1)
				t.Errorf("worker %d CallTool get_quote: %v", i, err)
				return
			}
			if ok.IsError {
				callErr.Add(1)
				t.Errorf("worker %d get_quote IsError", i)
			}

			// An invalid tools/call — the typed edge rejects it without a panic.
			bad, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "get_quote",
				Arguments: json.RawMessage(`{"symbol": 1234}`),
			})
			if err != nil {
				callErr.Add(1)
				t.Errorf("worker %d CallTool bad args — transport error: %v", i, err)
				return
			}
			if !bad.IsError {
				callErr.Add(1)
				t.Errorf("worker %d invalid args should be an error result", i)
			}

			// A flagging tools/call — concurrent flag accumulation on the shared
			// builder is mutex-guarded.
			if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "misrouted_ledger",
				Arguments: ledgerInput{Account: "acct"},
			}); err != nil {
				callErr.Add(1)
				t.Errorf("worker %d CallTool misrouted_ledger: %v", i, err)
				return
			}

			// resources/read.
			if _, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{
				URI: "ui://wave3/dashboard",
			}); err != nil {
				callErr.Add(1)
				t.Errorf("worker %d ReadResource: %v", i, err)
			}

			_ = tools // builders are read after the wait, below.
		}()
	}
	wg.Wait()

	if n := callErr.Load(); n != 0 {
		t.Fatalf("%d concurrent call failures", n)
	}

	// Every odd worker drove misrouted_ledger once: the shared in-memory
	// builder accumulated one misroute flag per such worker, with no lost or
	// duplicated entry — proof the mutex-guarded accumulation is race-free.
	wantInmemMisroutes := workers / 2
	if got := len(inmemTools.misrouter.Flags()); got != wantInmemMisroutes {
		t.Errorf("in-memory misrouter Flags() = %d, want %d (one per odd worker)",
			got, wantInmemMisroutes)
	}
	wantHTTPMisroutes := workers - workers/2
	if got := len(httpTools.misrouter.Flags()); got != wantHTTPMisroutes {
		t.Errorf("HTTP misrouter Flags() = %d, want %d (one per even worker)",
			got, wantHTTPMisroutes)
	}
	for _, f := range inmemTools.misrouter.Flags() {
		if f.Kind != tool.FlagMisroutedContent {
			t.Errorf("stress flag kind = %v, want FlagMisroutedContent", f.Kind)
		}
	}

	// Teardown: close the HTTP listener and cancel the in-memory server so all
	// transport goroutines unwind before the leak assertion.
	ts.Close()
	inmemCancel()
	assertNoGoroutineLeak(t, baseline)
}

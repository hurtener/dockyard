// This file is the Wave 2 wave-end end-to-end integration test (AGENTS.md
// §17 / §17.7 step 5). Unlike Wave 1's three independent foundations, Wave 2
// shipped the contract-first pipeline (RFC §4.2, §6) as a genuinely INTEGRATED
// flow: internal/manifest resolves a dockyard.app.yaml tool's Go type
// references through a ContractResolver into internal/codegen; internal/codegen
// turns a Go contract struct into a JSON Schema AND, independently, into
// TypeScript, then cross-checks the two for drift; and runtime/tool's
// contract-first builder generates a tool's schema from the same struct and
// installs it on a real runtime/server.
//
// This test drives that pipeline end to end with REAL components and no mocks
// at the seams: it loads the shipped example manifest, resolves its contract
// references, runs both halves of the Design A pipeline on a contract and
// cross-checks them, builds a tool from the contract and invokes it over the
// SDK in-memory transport, asserts the registered schema is the generated
// schema and that typed output lands in structuredContent, covers ≥1 failure
// mode per seam (located manifest error, ErrSchemaTSDrift, ErrStaleGenerated,
// rejected contract), and runs an N≥10 concurrency stress under -race with a
// goroutine-leak assertion. See decision D-038.
//
// Shared helpers — quietLogger, stableGoroutineCount, assertNoGoroutineLeak,
// canonical — are defined once for the integration package in wave1_test.go and
// phase04_codegen_test.go and reused here.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/codegen"
	"github.com/hurtener/dockyard/internal/manifest"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// ---- the Wave 2 contract -----------------------------------------------------
//
// healthInput / healthOutput is the contract-first pair this test drives. The
// Go struct is the single source of truth (P1, RFC §6.1): the JSON Schema, the
// TypeScript and the registered tool schema are all generated from it. The type
// names match the references the shipped example manifest's show_customer_health
// tool names — internal/contracts.ShowCustomerHealthInput / …Output — so the
// RegistryResolver below resolves the real manifest's tool contracts.

// healthInput is the show_customer_health tool input contract.
type healthInput struct {
	AccountID string `json:"account_id" jsonschema:"the account to score"`
	Window    int    `json:"window,omitempty"`
}

// healthSignal is one health signal in the output.
type healthSignal struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
}

// healthOutput is the show_customer_health tool output contract.
type healthOutput struct {
	Score   int            `json:"score"`
	Tier    string         `json:"tier"`
	Signals []healthSignal `json:"signals"`
	Note    string         `json:"note,omitempty"`
}

// healthContractTS is the Go contract source for the same pair, in the bare
// type-declaration shape codegen.TypeScriptForSource consumes. Field names and
// json tags match the structs above exactly, so the JSON Schema and the
// TypeScript — generated independently from Go (Design A, RFC §6.2) — agree and
// CrossCheck passes.
const healthContractTS = "// ShowCustomerHealthInput is the tool input contract.\n" +
	"type ShowCustomerHealthInput struct {\n" +
	"\tAccountID string `json:\"account_id\"`\n" +
	"\tWindow    int    `json:\"window,omitempty\"`\n" +
	"}\n\n" +
	"type HealthSignal struct {\n" +
	"\tName   string  `json:\"name\"`\n" +
	"\tWeight float64 `json:\"weight\"`\n" +
	"}\n\n" +
	"// ShowCustomerHealthOutput is the tool output contract.\n" +
	"type ShowCustomerHealthOutput struct {\n" +
	"\tScore   int            `json:\"score\"`\n" +
	"\tTier    string         `json:\"tier\"`\n" +
	"\tSignals []HealthSignal `json:\"signals\"`\n" +
	"\tNote    string         `json:\"note,omitempty\"`\n" +
	"}\n"

// healthRefInput / healthRefOutput are the contract references the shipped
// example manifest (examples/customer-health/dockyard.app.yaml) names for its
// show_customer_health tool.
const (
	healthRefInput  = "internal/contracts.ShowCustomerHealthInput"
	healthRefOutput = "internal/contracts.ShowCustomerHealthOutput"
)

// healthHandler is the contract-first tool handler: it receives typed,
// schema-validated input and returns a typed Result whose Structured field is
// the generated output contract (RFC §6.3).
func healthHandler(_ context.Context, in healthInput) (tool.Result[healthOutput], error) {
	return tool.Result[healthOutput]{
		Text: "account " + in.AccountID + " scored",
		Structured: healthOutput{
			Score: 87,
			Tier:  "healthy",
			Signals: []healthSignal{
				{Name: "usage", Weight: 0.6},
				{Name: "support", Weight: 0.4},
			},
		},
	}, nil
}

// exampleManifestPath is the shipped example manifest, relative to this test's
// package directory (test/integration).
const exampleManifestPath = "../../examples/customer-health/dockyard.app.yaml"

// healthResolver builds a real RegistryResolver that binds the example
// manifest's show_customer_health contract references to the Wave 2 contract
// types. A RegistryResolver is read-only after construction, so it is safe to
// share across the concurrency stress.
func healthResolver() *manifest.RegistryResolver {
	r := manifest.NewRegistryResolver()
	manifest.Register[healthInput](r, healthRefInput)
	manifest.Register[healthOutput](r, healthRefOutput)
	return r
}

// ---- 1. the contract-first pipeline, end to end -----------------------------

// TestWave2ContractFirstPipeline drives the full Wave 2 pipeline with real
// components and no mocks at the seams: load the shipped example manifest →
// resolve its tool contract references through a ContractResolver into
// internal/codegen → run both halves of Design A on the contract (Go struct →
// JSON Schema and Go struct → TypeScript) → CrossCheck the two → build the tool
// from the contract with the runtime/tool builder, Register it on a real
// runtime/server, serve it over the SDK in-memory transport, and invoke it.
func TestWave2ContractFirstPipeline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// --- internal/manifest: load the real shipped example manifest ---
	m, err := manifest.LoadFile(exampleManifestPath)
	if err != nil {
		t.Fatalf("LoadFile(%s): %v", exampleManifestPath, err)
	}
	if m.Name != "customer-health" {
		t.Fatalf("manifest name = %q, want customer-health", m.Name)
	}
	tl, ok := m.Tool("show_customer_health")
	if !ok {
		t.Fatalf("manifest has no show_customer_health tool: %+v", m.Tools)
	}
	if tl.Input != healthRefInput || tl.Output != healthRefOutput {
		t.Fatalf("tool contract refs = (%q,%q), want (%q,%q)",
			tl.Input, tl.Output, healthRefInput, healthRefOutput)
	}

	// --- internal/manifest → internal/codegen: resolve the manifest's tool
	// contract references through the ContractResolver seam ---
	resolved, err := m.ResolveContracts(healthResolver())
	if err != nil {
		t.Fatalf("ResolveContracts: %v", err)
	}
	contracts, ok := resolved["show_customer_health"]
	if !ok {
		t.Fatalf("ResolveContracts produced no entry for show_customer_health: %v", resolved)
	}
	if contracts.Input == nil || contracts.Output == nil {
		t.Fatalf("resolved contracts have a nil schema: %+v", contracts)
	}

	// The schema the resolver produced for the manifest's output reference must
	// be the same schema codegen.SchemaFor generates directly from the Go type
	// — that is the contract-first guarantee end to end (P1).
	directOut, err := codegen.SchemaFor[healthOutput]()
	if err != nil {
		t.Fatalf("SchemaFor[healthOutput]: %v", err)
	}
	if canonical(t, contracts.Output) != canonical(t, directOut) {
		t.Errorf("resolver-produced output schema differs from a direct SchemaFor:\n got %s\nwant %s",
			canonical(t, contracts.Output), canonical(t, directOut))
	}

	// --- internal/codegen: run BOTH halves of Design A on the contract ---
	// JSON Schema half (the resolved schema, from the manifest path above) and
	// the TypeScript half, generated independently from the same Go contract.
	ts, err := codegen.TypeScriptForSource(healthContractTS)
	if err != nil {
		t.Fatalf("TypeScriptForSource: %v", err)
	}
	if !strings.Contains(string(ts), "export interface ShowCustomerHealthOutput {") {
		t.Fatalf("generated TS missing ShowCustomerHealthOutput interface:\n%s", ts)
	}

	// --- internal/codegen: CrossCheck the two artifacts agree ---
	if err := codegen.CrossCheck(contracts.Input, "ShowCustomerHealthInput", ts); err != nil {
		t.Errorf("CrossCheck(input): a matched schema/TS pair should agree: %v", err)
	}
	if err := codegen.CrossCheck(contracts.Output, "ShowCustomerHealthOutput", ts); err != nil {
		t.Errorf("CrossCheck(output): a matched schema/TS pair should agree: %v", err)
	}

	// --- runtime/tool → runtime/server: build the tool from the contract,
	// register it on a real server, and confirm the registered schema is the
	// generated schema before serving ---
	srv, err := server.New(server.Info{
		Name:    m.Name,
		Title:   m.Title,
		Version: m.Version,
	}, &server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	builder := tool.New[healthInput, healthOutput](tl.Name).
		Describe(tl.Description).
		UI(tl.UI).
		Handler(healthHandler)

	// The builder's generated schemas must equal the manifest-resolved schemas:
	// both derive from the one Go contract struct (P1, RFC §6.1).
	builtIn, builtOut, err := builder.Schemas()
	if err != nil {
		t.Fatalf("builder.Schemas: %v", err)
	}
	if canonical(t, builtIn) != canonical(t, contracts.Input) {
		t.Errorf("builder input schema differs from the manifest-resolved schema")
	}
	if canonical(t, builtOut) != canonical(t, contracts.Output) {
		t.Errorf("builder output schema differs from the manifest-resolved schema")
	}

	if err := builder.Register(srv); err != nil {
		t.Fatalf("builder.Register: %v", err)
	}
	if got := srv.Tools(); len(got) != 1 || got[0] != tl.Name {
		t.Fatalf("server.Tools() = %v, want [%s]", got, tl.Name)
	}

	// --- serve over the SDK in-memory transport and invoke ---
	session := connectWave2(t, srv)

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != tl.Name {
		t.Fatalf("ListTools = %+v, want one tool %s", list.Tools, tl.Name)
	}

	// The registered tool carries the generated schema — not whatever the SDK
	// would infer separately (P1, RFC §6.1).
	if canonical(t, list.Tools[0].InputSchema) != canonical(t, contracts.Input) {
		t.Errorf("registered input schema is not the generated/resolved schema:\n got %s\nwant %s",
			canonical(t, list.Tools[0].InputSchema), canonical(t, contracts.Input))
	}
	if canonical(t, list.Tools[0].OutputSchema) != canonical(t, contracts.Output) {
		t.Errorf("registered output schema is not the generated/resolved schema:\n got %s\nwant %s",
			canonical(t, list.Tools[0].OutputSchema), canonical(t, contracts.Output))
	}

	// Invoke the tool and confirm typed output lands in structuredContent and
	// model-facing text in content[] (RFC §6.3).
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      tl.Name,
		Arguments: healthInput{AccountID: "acct-7", Window: 30},
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
	if tc, ok := res.Content[0].(*mcpsdk.TextContent); !ok || tc.Text != "account acct-7 scored" {
		t.Errorf("content = %+v, want the model-facing text", res.Content[0])
	}
	out := decodeHealthOutput(t, res.StructuredContent)
	if out.Score != 87 || out.Tier != "healthy" || len(out.Signals) != 2 {
		t.Errorf("structuredContent = %+v, want the typed UI payload", out)
	}
}

// ---- 2. failure modes — at least one per seam -------------------------------

// TestWave2FailureModes proves each Wave 2 seam fails closed with a typed,
// located error rather than a panic or silent acceptance (AGENTS.md §13).
func TestWave2FailureModes(t *testing.T) {
	t.Parallel()

	// internal/manifest: an invalid manifest fails with an ErrInvalidManifest
	// that carries a "file:line" location.
	t.Run("manifest/invalid-located-error", func(t *testing.T) {
		t.Parallel()
		// A manifest with a malformed tools[].input contract reference.
		bad := "name: bad-app\n" +
			"title: Bad App\n" +
			"version: 1.0.0\n" +
			"runtime:\n" +
			"  transports: [stdio]\n" +
			"tools:\n" +
			"  - name: broken\n" +
			"    description: A tool with a malformed contract reference.\n" +
			"    input: NotAGoTypeReference\n" +
			"    output: internal/contracts.BrokenOutput\n"
		_, err := manifest.Load(strings.NewReader(bad), "dockyard.app.yaml")
		if err == nil {
			t.Fatal("Load of a manifest with a bad contract ref: want error, got nil")
		}
		if !errors.Is(err, manifest.ErrInvalidManifest) {
			t.Fatalf("error should wrap ErrInvalidManifest, got %v", err)
		}
		// The fault is source-located: it names the file and the offending line.
		var me *manifest.Error
		var ml manifest.ErrorList
		switch {
		case errors.As(err, &ml):
			if len(ml) == 0 || ml[0].Line == 0 {
				t.Fatalf("manifest error list carries no located line: %v", err)
			}
		case errors.As(err, &me):
			if me.Line == 0 {
				t.Fatalf("manifest error carries no located line: %v", err)
			}
		default:
			t.Fatalf("error is not a manifest.Error/ErrorList: %T %v", err, err)
		}
		if !strings.Contains(err.Error(), "dockyard.app.yaml:") {
			t.Errorf("error should name the located source file: %v", err)
		}
	})

	// internal/manifest → internal/codegen: a manifest naming an unresolvable
	// contract reference fails ResolveContracts with ErrContractUnresolved,
	// wrapped with the offending tool and field.
	t.Run("resolver/unresolved-contract", func(t *testing.T) {
		t.Parallel()
		m, err := manifest.LoadFile(exampleManifestPath)
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		// An empty resolver knows none of the manifest's contract references.
		_, err = m.ResolveContracts(manifest.NewRegistryResolver())
		if err == nil {
			t.Fatal("ResolveContracts with an empty resolver: want error, got nil")
		}
		if !errors.Is(err, manifest.ErrContractUnresolved) {
			t.Fatalf("error should wrap ErrContractUnresolved, got %v", err)
		}
		if !strings.Contains(err.Error(), "show_customer_health") {
			t.Errorf("error should name the offending tool: %v", err)
		}
	})

	// internal/codegen: a deliberately drifted schema/TS pair trips
	// ErrSchemaTSDrift — the silent server↔UI drift the cross-check exists to
	// catch (RFC §6.2).
	t.Run("codegen/schema-ts-drift", func(t *testing.T) {
		t.Parallel()
		outSchema, err := codegen.SchemaFor[healthOutput]()
		if err != nil {
			t.Fatalf("SchemaFor[healthOutput]: %v", err)
		}
		// A hand-mutated TypeScript file that drops the `signals` property.
		drifted := []byte("export interface ShowCustomerHealthOutput {\n" +
			"  score: number;\n" +
			"  tier: string;\n" +
			"  note?: string;\n" +
			"}\n")
		err = codegen.CrossCheck(outSchema, "ShowCustomerHealthOutput", drifted)
		if err == nil {
			t.Fatal("CrossCheck of a drifted pair: want error, got nil")
		}
		if !errors.Is(err, codegen.ErrSchemaTSDrift) {
			t.Fatalf("error should wrap ErrSchemaTSDrift, got %v", err)
		}
		if !strings.Contains(err.Error(), "signals") {
			t.Errorf("error should name the drifted property: %v", err)
		}
	})

	// internal/codegen: stale on-disk generated output trips ErrStaleGenerated
	// — the "generated types out of date = build blocker" rule (brief 06 R1).
	t.Run("codegen/stale-generated", func(t *testing.T) {
		t.Parallel()
		stale, err := codegen.TypeScriptForSource(healthContractTS)
		if err != nil {
			t.Fatalf("TypeScriptForSource (stale): %v", err)
		}
		// The Go contract gained a field; the on-disk TS was not regenerated.
		freshSource := strings.Replace(healthContractTS,
			"\tNote    string         `json:\"note,omitempty\"`\n",
			"\tNote     string         `json:\"note,omitempty\"`\n"+
				"\tRenewal  string         `json:\"renewal\"`\n",
			1)
		fresh, err := codegen.TypeScriptForSource(freshSource)
		if err != nil {
			t.Fatalf("TypeScriptForSource (fresh): %v", err)
		}
		if err := codegen.CheckStale(stale, fresh); err == nil {
			t.Fatal("CheckStale of a stale artifact: want error, got nil")
		} else if !errors.Is(err, codegen.ErrStaleGenerated) {
			t.Fatalf("error should wrap ErrStaleGenerated, got %v", err)
		}
		// Sanity: a fresh artifact compared against itself is not stale.
		if err := codegen.CheckStale(fresh, fresh); err != nil {
			t.Errorf("CheckStale on identical bytes should pass: %v", err)
		}
	})

	// runtime/tool: a non-object output contract is rejected by Register with a
	// typed error — never a panic across the MCP boundary — and the rejected
	// tool is not installed on the server.
	t.Run("tool/invalid-contract-rejected", func(t *testing.T) {
		t.Parallel()
		srv, err := server.New(server.Info{Name: "bad-contract", Version: "0.0.1"},
			&server.Options{Logger: quietLogger()})
		if err != nil {
			t.Fatalf("server.New: %v", err)
		}
		err = tool.New[healthInput, string]("bad_tool").
			Handler(func(context.Context, healthInput) (tool.Result[string], error) {
				return tool.Result[string]{}, nil
			}).
			Register(srv)
		if err == nil {
			t.Fatal("Register of a non-object output contract: want error, got nil")
		}
		if len(srv.Tools()) != 0 {
			t.Errorf("a rejected tool must not be registered: %v", srv.Tools())
		}
	})
}

// ---- 3. concurrency stress under -race + goroutine-leak gate ----------------

// TestWave2ConcurrencyStress drives the contract-first pipeline concurrently
// from N≥10 goroutines against shared components — one loaded Manifest, one
// RegistryResolver, one runtime/server — and asserts no race (the -race
// detector does the asserting) and no goroutine leak after teardown. A loaded
// Manifest, a built RegistryResolver and a registered Server are each documented
// safe for concurrent use (AGENTS.md §5; manifest/resolve.go; server.go).
func TestWave2ConcurrencyStress(t *testing.T) {
	ctx := context.Background()

	// Settle pre-existing goroutines, then snapshot the baseline.
	baseline := stableGoroutineCount()

	// One shared, loaded Manifest and one shared, built resolver across all
	// workers — both read-only after construction.
	m, err := manifest.LoadFile(exampleManifestPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	resolver := healthResolver()

	// One shared Server with the contract-first tool registered up front.
	srv, err := server.New(server.Info{Name: m.Name, Version: m.Version},
		&server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	tl, _ := m.Tool("show_customer_health")
	if err := tool.New[healthInput, healthOutput](tl.Name).
		Describe(tl.Description).
		Handler(healthHandler).
		Register(srv); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// The schema every worker validates against — generated once, up front.
	wantOut, err := codegen.SchemaFor[healthOutput]()
	if err != nil {
		t.Fatalf("SchemaFor[healthOutput]: %v", err)
	}
	wantOutCanonical := canonical(t, wantOut)

	const workers = 16 // N >= 10
	const iterations = 20

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		go func(w int) {
			defer wg.Done()

			// Each worker gets its own client session against the shared
			// server — proves the Server is safe across concurrent sessions.
			// The worker tears its own session down before returning so the
			// post-wait goroutine-leak assertion sees a fully unwound wave.
			session, teardown := connectWave2WithTeardown(t, srv)
			defer teardown()

			for i := range iterations {
				// internal/manifest → internal/codegen: resolve the shared
				// manifest's tool contracts through the shared resolver.
				resolved, err := m.ResolveContracts(resolver)
				if err != nil {
					t.Errorf("worker %d iter %d: ResolveContracts: %v", w, i, err)
					return
				}
				contracts := resolved["show_customer_health"]
				if contracts.Output == nil {
					t.Errorf("worker %d iter %d: nil resolved output schema", w, i)
					return
				}
				if canonical(t, contracts.Output) != wantOutCanonical {
					t.Errorf("worker %d iter %d: resolved output schema drifted", w, i)
					return
				}

				// internal/codegen: run both Design A halves and cross-check.
				ts, err := codegen.TypeScriptForSource(healthContractTS)
				if err != nil {
					t.Errorf("worker %d iter %d: TypeScriptForSource: %v", w, i, err)
					return
				}
				if err := codegen.CrossCheck(contracts.Output,
					"ShowCustomerHealthOutput", ts); err != nil {
					t.Errorf("worker %d iter %d: CrossCheck: %v", w, i, err)
					return
				}

				// runtime/tool → runtime/server: invoke the registered tool
				// over the worker's own session.
				res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
					Name:      tl.Name,
					Arguments: healthInput{AccountID: "acct", Window: w},
				})
				if err != nil {
					t.Errorf("worker %d iter %d: CallTool: %v", w, i, err)
					return
				}
				if res.IsError {
					t.Errorf("worker %d iter %d: CallTool IsError: %+v", w, i, res.Content)
					return
				}
				out := decodeHealthOutput(t, res.StructuredContent)
				if out.Score != 87 || out.Tier != "healthy" {
					t.Errorf("worker %d iter %d: structuredContent = %+v", w, i, out)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	// Every worker has torn down its own session via its deferred teardown, so
	// after wg.Wait the wave is fully unwound.
	assertNoGoroutineLeak(t, baseline)
}

// ---- shared Wave 2 helpers --------------------------------------------------

// decodeHealthOutput decodes an MCP structuredContent value into the typed
// healthOutput contract. It is the read seam asserting that typed output lands
// in structuredContent (RFC §6.3).
func decodeHealthOutput(t *testing.T, structured any) healthOutput {
	t.Helper()
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out healthOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	return out
}

// connectWave2WithTeardown serves srv over the SDK in-memory transport and
// returns a connected client session plus an explicit teardown func. The
// teardown closes the session, cancels the serve goroutine and waits for it to
// exit — so a caller can prove the pipeline fully unwinds before a
// goroutine-leak assertion. It mirrors wave1_test.go's connectWithTeardown but
// is independent of that file's greet-tool server fixture.
func connectWave2WithTeardown(t *testing.T, srv *server.Server) (*mcpsdk.ClientSession, func()) {
	t.Helper()
	serverT, clientT := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())

	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wave2-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("client.Connect: %v", err)
	}

	var once sync.Once
	teardown := func() {
		once.Do(func() {
			_ = session.Close()
			cancel()
			select {
			case <-srvErr:
			case <-time.After(2 * time.Second):
				t.Error("server did not shut down")
			}
		})
	}
	return session, teardown
}

// connectWave2 is connectWave2WithTeardown with teardown registered as a
// t.Cleanup hook — the convenient form for a straight-line test.
func connectWave2(t *testing.T, srv *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	session, teardown := connectWave2WithTeardown(t, srv)
	t.Cleanup(teardown)
	return session
}

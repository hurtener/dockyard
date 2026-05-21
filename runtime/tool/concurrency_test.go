package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/hurtener/dockyard/runtime/tool"
)

// schemasMatch reports whether two schemas serialize identically.
func schemasMatch(a, b *jsonschema.Schema) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aj) == string(bj)
}

// TestConcurrentBuildAndRegister builds and registers tools from many
// goroutines, each on its own server, under -race. The builder is a per-tool
// throwaway; this proves independent builders and the codegen path it calls are
// free of shared mutable state (AGENTS.md §5, §14 — reusable-artifact rule).
func TestConcurrentBuildAndRegister(t *testing.T) {
	t.Parallel()
	const goroutines = 24

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := newServer(t)
			name := fmt.Sprintf("show_revenue_%d", i)
			if err := tool.New[revenueInput, revenueOutput](name).
				Describe("revenue").
				UI("revenue_card").
				Handler(revenueHandler).
				Register(s); err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", i, err)
				return
			}
			if got := s.Tools(); len(got) != 1 || got[0] != name {
				errs <- fmt.Errorf("goroutine %d: tools = %v", i, got)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestConcurrentToolCalls proves the Phase 08 handler runtime — a reusable
// artifact — is safe under concurrent tool calls (AGENTS.md §5, §14). N
// goroutines call one registered tool whose handler misroutes JSON into Text,
// so every call raises a flag; the test asserts (a) no data race accumulating
// flags, (b) every call's result is independent, (c) the flag count equals the
// call count.
func TestConcurrentToolCalls(t *testing.T) {
	t.Parallel()
	const calls = 32

	s := newServer(t)
	b := tool.New[revenueInput, revenueOutput]("concurrent").
		Handler(func(_ context.Context, in revenueInput) (tool.Result[revenueOutput], error) {
			return tool.Result[revenueOutput]{
				// JSON-shaped Text raises FlagMisroutedContent on every call.
				Text:       `{"period":"` + in.Period + `"}`,
				Structured: revenueOutput{Headline: in.Period, Total: 1},
			}, nil
		})
	if err := b.Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}
	session := connect(t, s)

	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			period := fmt.Sprintf("2026-Q%d", i)
			res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name:      "concurrent",
				Arguments: revenueInput{Period: period},
			})
			if err != nil {
				errs <- fmt.Errorf("call %d: %w", i, err)
				return
			}
			if res.IsError {
				errs <- fmt.Errorf("call %d: IsError", i)
				return
			}
			raw, _ := json.Marshal(res.StructuredContent)
			var out revenueOutput
			if err := json.Unmarshal(raw, &out); err != nil {
				errs <- fmt.Errorf("call %d: unmarshal: %w", i, err)
				return
			}
			if out.Headline != period {
				errs <- fmt.Errorf("call %d: headline = %q, want %q (results crossed)", i, out.Headline, period)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if flags := b.Flags(); len(flags) != calls {
		t.Errorf("Flags() = %d flags, want %d (one misroute flag per call)", len(flags), calls)
	}
}

// TestConcurrentSchemaGeneration hammers the codegen path the builder uses from
// many goroutines and asserts every call yields an identical schema — proving
// schema generation has no shared mutable state.
func TestConcurrentSchemaGeneration(t *testing.T) {
	t.Parallel()
	const goroutines = 24

	b := tool.New[revenueInput, revenueOutput]("show_revenue").Handler(revenueHandler)
	baseIn, baseOut, err := b.Schemas()
	if err != nil {
		t.Fatalf("baseline Schemas: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in, out, err := tool.New[revenueInput, revenueOutput]("show_revenue").
				Handler(revenueHandler).Schemas()
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", i, err)
				return
			}
			if !schemasMatch(baseIn, in) || !schemasMatch(baseOut, out) {
				errs <- fmt.Errorf("goroutine %d: schema differs from baseline", i)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/hurtener/dockyard/runtime/server"
)

// TestRawArgumentsRoundTrip proves the public WithRawArguments/RawArguments seam
// (tool.go) directly: a value stashed by WithRawArguments is returned verbatim
// by RawArguments. The seam is the in-process edge-validation path the
// contract-first handler runtime (runtime/tool, Phase 08) and the inspector
// drive without an over-the-wire call (RFC §5, §6.3; D-046).
func TestRawArgumentsRoundTrip(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"period":"2026-Q1"}`)
	ctx := server.WithRawArguments(context.Background(), raw)

	got := server.RawArguments(ctx)
	if !bytes.Equal(got, raw) {
		t.Fatalf("RawArguments() = %s, want %s", got, raw)
	}
}

// TestRawArgumentsNoOpBranches proves the nil/empty no-op branch of
// WithRawArguments and the absent-value branch of RawArguments: a context with
// no raw arguments yields nil, and passing nil or empty args leaves ctx
// unchanged so RawArguments still reports nil.
func TestRawArgumentsNoOpBranches(t *testing.T) {
	t.Parallel()

	base := context.Background()

	// No value stashed: RawArguments reports nil.
	if got := server.RawArguments(base); got != nil {
		t.Errorf("RawArguments() on a bare context = %s, want nil", got)
	}

	// nil and empty args are no-ops: ctx is returned unchanged.
	for _, raw := range []json.RawMessage{nil, {}} {
		ctx := server.WithRawArguments(base, raw)
		if ctx != base {
			t.Errorf("WithRawArguments(ctx, %#v) should return ctx unchanged", raw)
		}
		if got := server.RawArguments(ctx); got != nil {
			t.Errorf("RawArguments() after a no-op WithRawArguments = %s, want nil", got)
		}
	}
}

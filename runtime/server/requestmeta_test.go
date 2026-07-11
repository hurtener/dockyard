package server_test

import (
	"context"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/server"
)

// TestRequestMetaRoundTrip proves the public WithRequestMeta/RequestMeta seam
// (tool.go) directly: a map stashed by WithRequestMeta is returned, key-for-key,
// by RequestMeta. It mirrors RawArguments — the read-only seam an app author
// uses to read host-injected, per-call context (user, session, agent_id) that a
// host attaches outside the model-filled arguments (RFC §5, §6.3; D-189).
func TestRequestMetaRoundTrip(t *testing.T) {
	t.Parallel()

	src := map[string]any{"agent_id": "agt-7", "user": "alice"}
	ctx := server.WithRequestMeta(context.Background(), src)

	got := server.RequestMeta(ctx)
	if len(got) != len(src) {
		t.Fatalf("RequestMeta() = %v, want %v", got, src)
	}
	for k, want := range src {
		if got[k] != want {
			t.Errorf("RequestMeta()[%q] = %v, want %v", k, got[k], want)
		}
	}
}

// TestRequestMetaNoOpBranches proves the nil/empty no-op branch of
// WithRequestMeta and the absent-value branch of RequestMeta: a bare context
// yields nil, and a nil or empty map leaves ctx unchanged.
func TestRequestMetaNoOpBranches(t *testing.T) {
	t.Parallel()

	base := context.Background()

	if got := server.RequestMeta(base); got != nil {
		t.Errorf("RequestMeta() on a bare context = %v, want nil", got)
	}

	for _, m := range []map[string]any{nil, {}} {
		ctx := server.WithRequestMeta(base, m)
		if ctx != base {
			t.Errorf("WithRequestMeta(ctx, %#v) should return ctx unchanged", m)
		}
		if got := server.RequestMeta(ctx); got != nil {
			t.Errorf("RequestMeta() after a no-op WithRequestMeta = %v, want nil", got)
		}
	}
}

// TestRequestMetaDefensiveCopy proves WithRequestMeta shallow-copies its input:
// mutating the caller's source map after the call does not change what
// RequestMeta returns, and mutating the returned map does not change the source.
// The inbound `_meta` is the SDK's live request map, shared with the protocol
// machinery (it may carry reserved keys such as progressToken), so the copy
// keeps a handler from reaching back into protocol state.
func TestRequestMetaDefensiveCopy(t *testing.T) {
	t.Parallel()

	src := map[string]any{"agent_id": "agt-7"}
	ctx := server.WithRequestMeta(context.Background(), src)

	// Mutating the source after stashing must not leak into the stored copy.
	src["agent_id"] = "tampered"
	src["injected"] = "late"
	got := server.RequestMeta(ctx)
	if got["agent_id"] != "agt-7" {
		t.Errorf("stored copy agent_id = %v, want the value at stash time (agt-7)", got["agent_id"])
	}
	if _, ok := got["injected"]; ok {
		t.Errorf("stored copy leaked a key injected into the source after the stash")
	}

	// Mutating the returned map must not reach the caller's source map.
	got["agent_id"] = "handler-wrote"
	if src["agent_id"] != "tampered" {
		t.Errorf("mutating RequestMeta()'s result reached back into the source map")
	}
}

// TestRequestMetaShallowCopyIsShallow pins the documented limit of the per-call
// copy: it is shallow. A top-level key is isolated, but a nested map/slice value
// stays shared with the caller's source — which is why the seam's godoc/D-189
// document RequestMeta as read-only rather than a deep clone.
func TestRequestMetaShallowCopyIsShallow(t *testing.T) {
	t.Parallel()

	nested := map[string]any{"x": 1}
	src := map[string]any{"top": "v", "nested": nested}
	got := server.RequestMeta(server.WithRequestMeta(context.Background(), src))

	// Top level is isolated: replacing the source's key does not change the copy.
	src["top"] = "changed"
	if got["top"] != "v" {
		t.Errorf("top-level key not isolated: got[top] = %v, want v", got["top"])
	}

	// Nested value is shared: a mutation through the copy reaches the source's
	// nested map (the documented shallow-copy limit).
	got["nested"].(map[string]any)["x"] = 99
	if nested["x"] != 99 {
		t.Errorf("nested value unexpectedly deep-copied: nested[x] = %v, want 99 (shared)", nested["x"])
	}
}

// metaRecorder captures the `_meta` a handler observed via RequestMeta. The
// mutex makes the handler-goroutine write and the test-goroutine read race-free
// under -race (AGENTS.md §5, §11).
type metaRecorder struct {
	mu   sync.Mutex
	seen map[string]any
}

func (r *metaRecorder) set(m map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = m
}

func (r *metaRecorder) get() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen
}

// TestRequestMetaReachesHandler is the end-to-end proof (acceptance criterion 4):
// a tools/call carrying `_meta` reaches the typed handler, which reads the
// host-injected keys via RequestMeta — across BOTH registration wrappers
// (AddTool and AddToolWithSchemas). The 2026 SDK supplies its required modern
// metadata even when the caller omits application metadata, so the second call
// proves only caller-supplied keys are absent.
func TestRequestMetaReachesHandler(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)

	addToolRec := &metaRecorder{}
	if err := server.AddTool(s, server.ToolDef{Name: "echo-addtool"},
		func(ctx context.Context, in echoIn) (echoOut, error) {
			addToolRec.set(server.RequestMeta(ctx))
			return echoOut{Echo: in.Message}, nil
		}); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	schemasRec := &metaRecorder{}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "echo-schemas"}, nil, nil,
		func(ctx context.Context, in echoIn) (server.ToolOutput[echoOut], error) {
			schemasRec.set(server.RequestMeta(ctx))
			return server.ToolOutput[echoOut]{Structured: echoOut{Echo: in.Message}}, nil
		}); err != nil {
		t.Fatalf("AddToolWithSchemas: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT := s.ServeInMemory(ctx)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "meta-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	cases := []struct {
		name string
		tool string
		rec  *metaRecorder
	}{
		{"AddTool wrapper", "echo-addtool", addToolRec},
		{"AddToolWithSchemas wrapper", "echo-schemas", schemasRec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// With `_meta`: the host-injected keys reach the handler.
			if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      tc.tool,
				Arguments: echoIn{Message: "hi"},
				Meta:      mcpsdk.Meta{"agent_id": "agt-7", "user": "alice"},
			}); err != nil {
				t.Fatalf("CallTool with _meta: %v", err)
			}
			seen := tc.rec.get()
			if seen["agent_id"] != "agt-7" || seen["user"] != "alice" {
				t.Fatalf("handler saw _meta %v, want the host-injected agent_id/user", seen)
			}

			// Without caller-supplied `_meta`: the SDK still attaches required
			// modern lifecycle metadata, but must not retain keys from the prior
			// request.
			if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      tc.tool,
				Arguments: echoIn{Message: "hi"},
			}); err != nil {
				t.Fatalf("CallTool without _meta: %v", err)
			}
			if seen := tc.rec.get(); seen["agent_id"] != nil || seen["user"] != nil {
				t.Fatalf("handler retained caller _meta on a later call: %v", seen)
			}
			if seen[mcpsdk.MetaKeyProtocolVersion] == nil ||
				seen[mcpsdk.MetaKeyClientInfo] == nil ||
				seen[mcpsdk.MetaKeyClientCapabilities] == nil {
				t.Fatalf("handler missing SDK-required modern metadata: %v", seen)
			}
		})
	}
}

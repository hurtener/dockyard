package tasks

import (
	"context"
	"testing"
)

// This file holds the Phase 27 fuzz targets for the Tasks JSON-RPC frame
// parser (the wire surface every tasks/* method passes through before the
// engine sees it). The invariant is uniform: [Mount.HandleFrame] NEVER
// panics on arbitrary input. A frame-decoding error or a typed dispatch
// error is correct behaviour; only a panic is a fuzz failure.
//
// The fuzz harness builds a real Engine over [NewInMemoryStore] so the
// frame parser is exercised against a live dispatch path, not a stub —
// the parser, the codec, the engine's auth-context binding, and the
// store all participate. The engine is constructed with
// AdvertiseList=true + RequestorIdentifiable=true so the
// tasks/list path is reachable from the fuzzer.
//
// CI runs the seed corpus as ordinary tests. For a longer local session:
//
//	go test ./runtime/tasks -run '^$' -fuzz FuzzMountHandleFrame -fuzztime 60s

// FuzzMountHandleFrame fuzzes the JSON-RPC frame parser entrypoint of the
// Tasks transport mount. Arbitrary bytes — valid frames, malformed JSON,
// wrong-typed params, garbage — all must yield a typed (error, handled)
// pair without panicking.
func FuzzMountHandleFrame(f *testing.F) {
	// Seed corpus: a representative frame for every tasks/* method,
	// plus hostile-input shapes.
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{"taskId":"t-1"}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/cancel","params":{"taskId":"t-1"}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/result","params":{"taskId":"t-1"}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/list","params":{}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"dockyard/tasks/supplyInput","params":{"taskId":"t-1"}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"tasks/get","params":{"taskId":"t-1"}}`)) // notification (no id)
	f.Add([]byte(`{"jsonrpc":"2.0","id":null,"method":"tasks/get","params":{}}`))     // null id
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))
	f.Add([]byte(`not even json`))
	// Wrong-typed params:
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":42}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":[1,2,3]}`))
	// Unknown method — must surface as a typed ErrUnknownMethod, not a panic.
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/unknown","params":{}}`))
	// Method missing — must surface as a typed error.
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"params":{"taskId":"t"}}`))
	// Oversized taskId in supplyInput — hostile shape.
	long := make([]byte, 0, 16384)
	for i := 0; i < 4096; i++ {
		long = append(long, 'A', 'B', 'C', 'D')
	}
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"dockyard/tasks/supplyInput","params":{"taskId":"` + string(long) + `"}}`))
	// Embedded NUL byte in the body (must not corrupt the parser).
	f.Add([]byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tasks/get\",\"params\":{\"taskId\":\"x\x00y\"}}"))
	// Embedded carriage returns / line feeds (transport-level hostile inputs).
	f.Add([]byte("{\r\n\"jsonrpc\":\"2.0\",\r\n\"id\":1,\r\n\"method\":\"tasks/get\",\r\n\"params\":{\"taskId\":\"a\"}}"))

	engine, err := NewEngine(NewInMemoryStore(), &Options{
		AdvertiseList:         true,
		RequestorIdentifiable: true,
	})
	if err != nil {
		f.Fatalf("NewEngine: %v", err)
	}
	mount := NewMount(engine)

	f.Fuzz(func(_ *testing.T, frame []byte) {
		// Invariant: never panics. Anything else — (nil, false, nil) for a
		// non-tasks frame, a non-nil response for a handled frame, a typed
		// error for a decode failure — is acceptable.
		ctx, cancel := context.WithTimeout(context.Background(), 0) // immediately cancelled
		defer cancel()
		_, _, _ = mount.HandleFrame(ctx, "", frame)
	})
}

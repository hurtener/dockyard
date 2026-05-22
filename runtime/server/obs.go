package server

import (
	"encoding/json"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// This file holds the small obs/v1 instrumentation helpers shared by the tool
// and resource handlers. The runtime EMITS obs/v1 events through s.rec; nothing
// reads runtime internals to observe (P2, CLAUDE.md §6). The helpers never
// fail and never panic: observability must not fail a request.

// toolArgs returns the raw, undecoded JSON arguments of a tools/call request,
// or nil. obs uses it for shape+size capture (CLAUDE.md §7) — the raw bytes are
// never embedded in an event under the default policy, only their shape.
func toolArgs(req *mcpsdk.CallToolRequest) json.RawMessage {
	if req == nil || req.Params == nil {
		return nil
	}
	return req.Params.Arguments
}

// toolTransport reports the MCP transport a tools/call arrived on, for the
// obs/v1 ToolCallPayload. The SDK request does not surface the transport
// directly at this layer; Phase 16's transport instrumentation refines this.
// Phase 15 records "" (unknown) rather than guessing.
func toolTransport(_ *mcpsdk.CallToolRequest) string { return "" }

// marshalForObs encodes a typed handler output to JSON for obs/v1 shape
// capture. A marshal failure yields nil — the event then carries a null output
// shape rather than failing the tools/call (P2: observability never fails a
// request).
func marshalForObs(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// resourceBytes reports the served byte size of a resource's content — the
// obs/v1 ResourceReadPayload size guardrail signal.
func resourceBytes(c ResourceContent) int {
	if len(c.Blob) > 0 {
		return len(c.Blob)
	}
	return len(c.Text)
}

package otel

import (
	"encoding/json"

	"github.com/hurtener/dockyard/runtime/obs"
)

// This file decodes the per-kind obs/v1 payloads the adapter needs to read to
// build MCP semantic-convention attributes. It decodes the PUBLIC obs payload
// types (obs.ToolCallPayload, obs.ResourceReadPayload, obs.LogPayload) from the
// event's JSON payload field — the adapter is a pure obs/v1 client (P2), it
// reads no runtime internals.

// decodeToolPayload decodes a tool.call event's payload.
func decodeToolPayload(e obs.Event) (obs.ToolCallPayload, error) {
	var p obs.ToolCallPayload
	if len(e.Payload) == 0 {
		return p, nil
	}
	err := json.Unmarshal(e.Payload, &p)
	return p, err
}

// decodeResourcePayload decodes a resource.read event's payload.
func decodeResourcePayload(e obs.Event) (obs.ResourceReadPayload, error) {
	var p obs.ResourceReadPayload
	if len(e.Payload) == 0 {
		return p, nil
	}
	err := json.Unmarshal(e.Payload, &p)
	return p, err
}

// decodeLogPayload decodes a log event's payload.
func decodeLogPayload(e obs.Event) (obs.LogPayload, error) {
	var p obs.LogPayload
	if len(e.Payload) == 0 {
		return p, nil
	}
	err := json.Unmarshal(e.Payload, &p)
	return p, err
}

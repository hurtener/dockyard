package server

import (
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMRTRWireAdaptersUseTypedSDKFields(t *testing.T) {
	t.Parallel()

	requests, err := encodeInputRequests(map[string]InputRequest{
		"approval": ElicitationRequest{Message: "Approve?", RequestedSchema: json.RawMessage(`{"type":"object"}`)},
		"sample":   SamplingRequest{MaxTokens: 20, Messages: json.RawMessage(`[]`)},
		"roots":    RootsRequest{},
	})
	if err != nil {
		t.Fatalf("encodeInputRequests: %v", err)
	}
	if _, ok := requests["approval"].(*mcpsdk.ElicitParams); !ok {
		t.Fatalf("approval type = %T", requests["approval"])
	}
	if _, ok := requests["sample"].(*mcpsdk.CreateMessageParams); !ok { //nolint:staticcheck // Required by the MCP 2026-07-28 MRTR closed union.
		t.Fatalf("sample type = %T", requests["sample"])
	}
	if _, ok := requests["roots"].(*mcpsdk.ListRootsParams); !ok { //nolint:staticcheck // Required by the MCP 2026-07-28 MRTR closed union.
		t.Fatalf("roots type = %T", requests["roots"])
	}

	responses, err := decodeInputResponses(mcpsdk.InputResponseMap{
		"approval": &mcpsdk.ElicitResult{Action: "accept", Content: map[string]any{"approved": true}},
		"roots":    &mcpsdk.ListRootsResult{Roots: []*mcpsdk.Root{{URI: "file:///workspace", Name: "workspace"}}}, //nolint:staticcheck // Required by the MCP 2026-07-28 MRTR closed union.
	})
	if err != nil {
		t.Fatalf("decodeInputResponses: %v", err)
	}
	if got := responses["approval"].(ElicitationResponse); got.Action != "accept" || got.Content["approved"] != true {
		t.Fatalf("approval = %#v", got)
	}
	if got := responses["roots"].(RootsResponse); len(got.Roots) != 1 || got.Roots[0].URI != "file:///workspace" {
		t.Fatalf("roots = %#v", got)
	}
}

func TestRequestStateRemainsOpaque(t *testing.T) {
	t.Parallel()
	state := RequestState("protected-by-application")
	if string(state) != "protected-by-application" {
		t.Fatalf("state changed: %q", state)
	}
}

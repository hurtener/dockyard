package server

import (
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RequestState is an opaque continuation value echoed by an MRTR client.
// Treat inbound values as attacker-controlled. Applications that place trusted
// data here are responsible for integrity, confidentiality, principal binding,
// expiry, argument binding, and replay controls appropriate to that data.
type RequestState string

// ToolCall carries the typed tool arguments and any MRTR continuation data.
type ToolCall[In any] struct {
	Input          In
	InputResponses map[string]InputResponse
	RequestState   RequestState
}

// InputRequest is one typed request the client must fulfill before retrying.
type InputRequest interface{ inputRequest() }

// ElicitationRequest asks the client to collect structured user input.
type ElicitationRequest struct {
	Mode            string          `json:"mode,omitempty"`
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	URL             string          `json:"url,omitempty"`
	ElicitationID   string          `json:"elicitationId,omitempty"`
}

func (ElicitationRequest) inputRequest() {}

// SamplingRequest contains sampling/createMessage request data. Complex MCP
// content remains JSON data, while the request kind and scalar controls stay
// explicit and SDK-independent.
type SamplingRequest struct {
	IncludeContext   string          `json:"includeContext,omitempty"`
	MaxTokens        int64           `json:"maxTokens"`
	Messages         json.RawMessage `json:"messages"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	ModelPreferences json.RawMessage `json:"modelPreferences,omitempty"`
	StopSequences    []string        `json:"stopSequences,omitempty"`
	SystemPrompt     string          `json:"systemPrompt,omitempty"`
	Temperature      float64         `json:"temperature,omitempty"`
}

func (SamplingRequest) inputRequest() {}

// RootsRequest asks the client for its exposed roots.
type RootsRequest struct{}

func (RootsRequest) inputRequest() {}

// InputResponse is one typed response supplied on an MRTR retry.
type InputResponse interface{ inputResponse() }

// ElicitationResponse contains the user's response to an elicitation request.
type ElicitationResponse struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

func (ElicitationResponse) inputResponse() {}

// SamplingResponse contains an opaque sampling result.
type SamplingResponse struct{ Data json.RawMessage }

func (SamplingResponse) inputResponse() {}

// Root identifies one root exposed by the client.
type Root struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

// RootsResponse contains the roots exposed by the client.
type RootsResponse struct{ Roots []Root }

func (RootsResponse) inputResponse() {}

func decodeInputResponses(in mcpsdk.InputResponseMap) (map[string]InputResponse, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]InputResponse, len(in))
	for key, response := range in {
		switch response := response.(type) {
		case *mcpsdk.ElicitResult:
			out[key] = ElicitationResponse{Action: response.Action, Content: response.Content}
		// MRTR's closed input-response union normatively includes deprecated sampling results.
		//nolint:staticcheck // Required by the MCP 2026-07-28 MRTR closed union.
		case *mcpsdk.CreateMessageResult, *mcpsdk.CreateMessageWithToolsResult:
			data, err := json.Marshal(response)
			if err != nil {
				return nil, fmt.Errorf("input response %q: %w", key, err)
			}
			out[key] = SamplingResponse{Data: data}
		case *mcpsdk.ListRootsResult: //nolint:staticcheck // Required by the MCP 2026-07-28 MRTR closed union.
			roots := make([]Root, len(response.Roots))
			for i, root := range response.Roots {
				roots[i] = Root{URI: root.URI, Name: root.Name}
			}
			out[key] = RootsResponse{Roots: roots}
		default:
			return nil, fmt.Errorf("input response %q has unsupported type %T", key, response)
		}
	}
	return out, nil
}

func encodeInputRequests(in map[string]InputRequest) (mcpsdk.InputRequestMap, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(mcpsdk.InputRequestMap, len(in))
	for key, request := range in {
		var sdkRequest mcpsdk.InputRequest
		switch request := request.(type) {
		case ElicitationRequest:
			var schema any
			if len(request.RequestedSchema) > 0 {
				if err := json.Unmarshal(request.RequestedSchema, &schema); err != nil {
					return nil, fmt.Errorf("input request %q schema: %w", key, err)
				}
			}
			sdkRequest = &mcpsdk.ElicitParams{Mode: request.Mode, Message: request.Message, RequestedSchema: schema, URL: request.URL, ElicitationID: request.ElicitationID}
		case SamplingRequest:
			// MRTR's closed input-request union normatively includes deprecated sampling requests.
			//nolint:staticcheck // Required by the MCP 2026-07-28 MRTR closed union.
			var p mcpsdk.CreateMessageParams
			data, err := json.Marshal(request)
			if err != nil {
				return nil, fmt.Errorf("input request %q: %w", key, err)
			}
			if err := json.Unmarshal(data, &p); err != nil {
				return nil, fmt.Errorf("input request %q: %w", key, err)
			}
			sdkRequest = &p
		case RootsRequest:
			sdkRequest = &mcpsdk.ListRootsParams{} //nolint:staticcheck // Required by the MCP 2026-07-28 MRTR closed union.
		default:
			return nil, fmt.Errorf("input request %q has unsupported type %T", key, request)
		}
		out[key] = sdkRequest
	}
	return out, nil
}

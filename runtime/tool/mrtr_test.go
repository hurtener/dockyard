package tool

import (
	"context"
	"testing"

	"github.com/hurtener/dockyard/runtime/server"
)

func TestContinuationHandlerReceivesAndReturnsMRTRData(t *testing.T) {
	t.Parallel()
	type input struct {
		Value string `json:"value"`
	}
	type output struct {
		Approved bool `json:"approved"`
	}

	rt, err := newContinuationHandlerRuntime("approve", func(_ context.Context, call Call[input]) (Result[output], error) {
		if call.RequestState != "bound-state" {
			t.Fatalf("request state = %q", call.RequestState)
		}
		response, ok := call.InputResponses["approval"].(ElicitationResponse)
		if !ok || response.Action != "accept" {
			t.Fatalf("response = %#v", call.InputResponses["approval"])
		}
		return Result[output]{
			InputRequests: map[string]InputRequest{"roots": RootsRequest{}},
			RequestState:  "next-state",
		}, nil
	}, nil, DefaultOutputSizeBudget)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	got, err := rt.serve(context.Background(), server.ToolCall[input]{
		Input:          input{Value: "x"},
		InputResponses: map[string]server.InputResponse{"approval": server.ElicitationResponse{Action: "accept"}},
		RequestState:   "bound-state",
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if got.RequestState != "next-state" {
		t.Fatalf("returned state = %q", got.RequestState)
	}
	if _, ok := got.InputRequests["roots"].(RootsRequest); !ok {
		t.Fatalf("returned request = %T", got.InputRequests["roots"])
	}
}

package tool

import "github.com/hurtener/dockyard/runtime/server"

// RequestState is an opaque continuation value echoed by an MRTR client.
type RequestState = server.RequestState

// InputRequest is one typed request the client must fulfill before retrying.
type InputRequest = server.InputRequest

// ElicitationRequest asks the client to collect structured user input.
type ElicitationRequest = server.ElicitationRequest

// SamplingRequest contains sampling/createMessage request data.
type SamplingRequest = server.SamplingRequest

// RootsRequest asks the client for its exposed roots.
type RootsRequest = server.RootsRequest

// InputResponse is one typed response supplied on an MRTR retry.
type InputResponse = server.InputResponse

// ElicitationResponse contains the user's response to an elicitation request.
type ElicitationResponse = server.ElicitationResponse

// SamplingResponse contains an opaque sampling result.
type SamplingResponse = server.SamplingResponse

// RootsResponse contains the roots exposed by the client.
type RootsResponse = server.RootsResponse

// Root identifies one root exposed by the client.
type Root = server.Root

// Call is the app-facing invocation including independent MRTR retry data.
type Call[In any] struct {
	Input          In
	InputResponses map[string]InputResponse
	RequestState   RequestState
}

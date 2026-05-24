package tool_test

import (
	"context"
	"fmt"

	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// EchoInput is the input contract for a contract-first echo tool — a
// typed Go struct. The JSON Schema the host sees is generated from
// THIS struct, not hand-written (Dockyard P1, RFC §6).
type EchoInput struct {
	// Message is the text to echo back to the model.
	Message string `json:"message"`
}

// EchoOutput is the matching output contract.
type EchoOutput struct {
	// Echo is the echoed text.
	Echo string `json:"echo"`
}

// ExampleNew shows the canonical contract-first tool declaration: bind
// the typed input + output contracts to New, set the description and
// the handler with the fluent builder, then Register the tool on a
// runtime/server.Server. The schema the host sees is generated from
// EchoInput / EchoOutput via internal/codegen.
func ExampleNew() {
	srv, _ := server.New(server.Info{Name: "echo-example", Version: "0.1.0"}, nil)

	err := tool.New[EchoInput, EchoOutput]("echo").
		Describe("Echo the input message back to the caller.").
		Handler(func(_ context.Context, in EchoInput) (tool.Result[EchoOutput], error) {
			return tool.Result[EchoOutput]{
				Text:       "echoed: " + in.Message,
				Structured: EchoOutput{Echo: in.Message},
			}, nil
		}).
		Register(srv)
	if err != nil {
		fmt.Println("register:", err)
		return
	}

	fmt.Println("registered tools:", srv.Tools())
	// Output: registered tools: [echo]
}

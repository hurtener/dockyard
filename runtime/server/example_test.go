package server_test

import (
	"context"
	"fmt"

	"github.com/hurtener/dockyard/runtime/server"
)

// ExampleNew is the minimal "construct + serve over stdio" recipe.
// Construct the server with an Info identity, optionally pass Options
// (logger, obs emitter, Tasks engine), then call srv.ServeStdio under a
// context the caller cancels on shutdown.
func ExampleNew() {
	srv, err := server.New(server.Info{
		Name:    "example-server",
		Title:   "Example Server",
		Version: "0.1.0",
	}, nil)
	if err != nil {
		fmt.Println("new:", err)
		return
	}
	fmt.Println(srv.Info().Name)
	// Output: example-server
}

// ExampleAddPrompt shows the Phase 28 prompts API: a thin pass-through
// to the SDK's prompt registration with the same panic recovery + the
// obs/v1 prompt.get lifecycle every Dockyard handler gets.
//
// Prompts are templates the host PULLS via prompts/get; a host might
// surface them as `/slash` commands. The argument map is flat strings
// (MCP's prompt-argument shape) — Dockyard's contract-first pattern
// does not extend to prompts (D-152), so AddPrompt is a focused
// pass-through rather than a typed-contract builder.
func ExampleAddPrompt() {
	srv, _ := server.New(server.Info{Name: "prompts-example", Version: "0.1.0"}, nil)

	err := server.AddPrompt(srv, server.PromptDef{
		Name:        "summarize",
		Title:       "Summarise a passage",
		Description: "Two-sentence summary fit for an engineering peer.",
		Arguments: []server.PromptArgument{
			{Name: "passage", Required: true},
		},
	}, func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
		return server.PromptResult{
			Messages: []server.PromptMessage{
				{Role: "system", Text: "You are a careful summariser."},
				{Role: "user", Text: "Please summarise:\n" + req.Arguments["passage"]},
			},
		}, nil
	})
	if err != nil {
		fmt.Println("add prompt:", err)
		return
	}
	fmt.Println("prompts:", srv.Prompts())
	// Output: prompts: [summarize]
}

// Package contracts holds this server's tool input and output contracts.
//
// These typed Go structs are the SOURCE OF TRUTH for the tool's schema
// (Dockyard P1 — contract-first, RFC §6). The JSON Schema and TypeScript
// alongside this file are GENERATED from these structs by `dockyard generate`;
// never hand-edit a generated file. Change a contract here, then regenerate.
package contracts

// GreetInput is the greet tool's typed input contract.
type GreetInput struct {
	// Name is who to greet. Required.
	Name string `json:"name"`
	// Greeting is the salutation to use; defaults to "Hello" when empty.
	Greeting string `json:"greeting,omitempty"`
}

// GreetOutput is the greet tool's typed output contract — the structured,
// UI-facing payload.
type GreetOutput struct {
	// Message is the assembled greeting.
	Message string `json:"message"`
	// Length is the rune length of Message.
	Length int `json:"length"`
}

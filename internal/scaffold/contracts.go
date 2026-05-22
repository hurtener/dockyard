package scaffold

// This file holds the example tool's contract types as REAL Go types, so the
// scaffold can generate their JSON Schema by reflection through
// internal/codegen (P1 — the schema is generated from the Go struct, never
// hand-written; RFC §6.1).
//
// The exact same struct definitions are emitted into the scaffolded project's
// internal/contracts package as contractsGoSource below. The two are kept in
// lockstep by TestScaffoldContractsMatchModel — an accidental divergence fails
// CI — so the scaffold ships a project whose generated schema genuinely
// matches its contract source.

// GreetInput is the example tool's typed input contract. Keep it minimal: one
// required string, one optional one — enough to show the contract-first shape
// (a typed struct, JSON tags) without modelling a real domain.
type GreetInput struct {
	// Name is who to greet. Required.
	Name string `json:"name"`
	// Greeting is the salutation to use; defaults to "Hello" when empty.
	Greeting string `json:"greeting,omitempty"`
}

// GreetOutput is the example tool's typed output contract — the structured,
// UI-facing payload (RFC §6.3).
type GreetOutput struct {
	// Message is the assembled greeting.
	Message string `json:"message"`
	// Length is the rune length of Message — a trivial derived field so the
	// output struct is not a single-field shell.
	Length int `json:"length"`
}

// contractsGoSource is the verbatim Go source written to the scaffolded
// project's internal/contracts/contracts.go. It MUST declare the same
// GreetInput / GreetOutput structs as the Go types above; TestScaffold-
// ContractsMatchModel proves it does. The package clause makes it a real,
// compilable contracts package in the generated project.
const contractsGoSource = `// Package contracts holds this server's tool input and output contracts.
//
// These typed Go structs are the SOURCE OF TRUTH for the tool's schema
// (Dockyard P1 — contract-first, RFC §6). The JSON Schema and TypeScript
// alongside this file are GENERATED from these structs by ` + "`dockyard generate`" + `;
// never hand-edit a generated file. Change a contract here, then regenerate.
package contracts

// GreetInput is the greet tool's typed input contract.
type GreetInput struct {
	// Name is who to greet. Required.
	Name string ` + "`json:\"name\"`" + `
	// Greeting is the salutation to use; defaults to "Hello" when empty.
	Greeting string ` + "`json:\"greeting,omitempty\"`" + `
}

// GreetOutput is the greet tool's typed output contract — the structured,
// UI-facing payload.
type GreetOutput struct {
	// Message is the assembled greeting.
	Message string ` + "`json:\"message\"`" + `
	// Length is the rune length of Message.
	Length int ` + "`json:\"length\"`" + `
}
`

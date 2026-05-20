// Package tool is Dockyard's contract-first typed tool builder — the app-facing
// API an author uses to declare an MCP tool (RFC §6, brief 04 §3).
//
// A tool is defined by two Go structs: its input contract and its output
// contract. They are the single source of truth (P1, RFC §6.1). The builder
// generates the tool's JSON Schema from those structs via internal/codegen and
// registers the tool on a runtime/server with the generated schema, so the
// schema a host sees is provably the one the contract describes — never a
// hand-written one that can silently drift (the mcp-use failure mode, brief
// §2.6).
//
// Usage:
//
//	err := tool.New[ShowRevenueInput, ShowRevenueOutput]("show_revenue").
//		Describe("Render the revenue dashboard").
//		UI("revenue_card").
//		Handler(handleShowRevenue).
//		Register(srv)
//
// The handler returns a Result[Out]: its Text is model-facing (content[]) and
// its Structured value is the typed, UI-facing payload (structuredContent),
// per RFC §6.3.
//
// Note on shape: brief 04's sketch writes app.Tool("x").Input[T]().Output[T]().
// Go does not permit type parameters on methods, so the contract types are
// bound once by the package-level generic constructor New[In, Out]; the rest of
// the chain is plain methods. The fluent, contract-first ergonomics are
// preserved. See docs/decisions.md D-029.
package tool

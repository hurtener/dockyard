// Package server is the Dockyard app-runtime MCP server core.
//
// It is the importable library half of Dockyard (RFC §3, §5): a generated
// Dockyard app's main.go is thin and delegates the protocol weight to this
// package. The package wraps the official MCP SDK
// (github.com/modelcontextprotocol/go-sdk, pinned per brief 03) rather than
// re-implementing the protocol — the SDK is the settled foundation (RFC §5.1,
// D-002) and Dockyard never forks it.
//
// Phase 01 establishes the skeleton: constructing a server, registering a
// typed tool via AddTool, and serving over stdio.
//
// Phase 04 adds the contract-first registration seam the runtime/tool builder
// composes (RFC §6, P1): AddToolWithSchemas registers a typed tool with
// caller-supplied input and output JSON Schemas — the schemas internal/codegen
// generates from the Go contract structs — so the registered tool's schema is
// provably the generated contract, not whatever the SDK would infer separately.
// Its handler returns a ToolOutput[Out] (a ToolOutputFunc), which splits the two
// channels of an MCP CallToolResult (RFC §6.3): Text is model-facing and lands
// in content[], Structured is the typed UI payload and lands in
// structuredContent, and Meta lands in _meta.
//
// The streamable-HTTP transport, the security knobs, the Apps and Tasks
// extension layers, and the obs/v1 stream land in later phases (RFC §5.2, §5.3,
// §7, §8, §11); the seams here are kept deliberately small so those phases
// extend without reshaping this package.
package server

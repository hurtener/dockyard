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
// Phase 07 completes the MCP server core (RFC §5): typed resource registration
// (AddResource), the streamable-HTTP transport (HTTPHandler) alongside stdio,
// and the in-memory transport (ServeInMemory) for tests and the inspector.
// HTTP security — DNS-rebinding protection, Origin/Content-Type verification,
// and cross-origin protection — is set explicitly via HTTPSecurity and never
// inherited from an SDK default, because the SDK has flipped those defaults
// between releases (RFC §5.2, AGENTS.md §7, brief 03 §2.3). Phase 07 also
// retires the temporary exported MCP() SDK seam (D-021, D-042): the
// Dockyard-owned registration and transport surface is now complete.
//
// Panic safety is a toolchain-enforced guarantee, not a docstring instruction
// (AGENTS.md §5, §13; D-053): every tool and resource handler invocation is
// routed through guardHandler, which recovers a panicking app-author handler
// into a typed error (ErrHandlerPanic) so a bad handler on a live tools/call or
// resources/read degrades into a clean error result instead of crashing the
// server process. AddResourceTemplate (D-054) registers an RFC 6570 URI-template
// family — the typed surface the Apps layer's ui:// auto-discovery composes —
// with the same panic-recovered handler invocation as AddResource.
//
// The Apps and Tasks extension layers and the obs/v1 stream land in later
// phases (RFC §5.3, §7, §8, §11); the seams here are kept deliberately small so
// those phases extend without reshaping this package.
package server

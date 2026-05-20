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
// typed tool, and serving over stdio. The streamable-HTTP transport, the
// security knobs, the Apps and Tasks extension layers, and the obs/v1 stream
// land in later phases (RFC §5.2, §5.3, §7, §8, §11); the seams here are kept
// deliberately small so those phases extend without reshaping this package.
package server

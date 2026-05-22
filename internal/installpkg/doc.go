// Package installpkg implements the `dockyard install` verb — registering a
// built Dockyard server with an MCP host (RFC §14).
//
// `dockyard install claude|cursor` writes the host's MCP config file so the
// host launches this Dockyard server as a local stdio subprocess
// ({"command": "<binary path>"}), then verifies the server boots by spawning
// it and driving one real MCP `initialize` handshake.
//
// Two security-relevant properties hold:
//
//   - The write is NON-DESTRUCTIVE. installpkg loads the host's existing
//     config, merges this server's entry into the mcpServers map, and writes
//     the result back — every unrelated host entry is preserved. The prior
//     file is backed up first.
//   - The boot check is NOT a production MCP client (P4). It is a throwaway,
//     localhost, dev-only spawn of the built server with a bounded timeout: it
//     proves the host config launches a working server and then tears the
//     process down. Harbor owns the production MCP client.
//
// The per-host config-file locations are kept behind a small hostProfile
// struct (claude, cursor) — a filesystem-path derivation, not a hardcoded
// capability matrix (CLAUDE.md §6).
//
// installpkg is internal — the reusable, testable seam the `dockyard install`
// cobra verb and the Phase 20 integration test consume.
package installpkg

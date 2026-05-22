package installpkg

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bootCheckTimeout bounds the install boot check: the spawned server has this
// long to accept a connection and complete the MCP initialize handshake before
// the check fails. It is a generous-but-bounded window — a healthy server over
// stdio handshakes in well under a second.
const bootCheckTimeout = 15 * time.Second

// bootCheck verifies the host config launches a working server: it spawns the
// built server binary exactly as the host config does — as a local stdio
// subprocess — drives one real MCP `initialize` handshake, and tears the
// process down.
//
// This is the lone client-shaped surface `dockyard install` touches. It is NOT
// a production MCP client (P4): it is a throwaway, localhost, dev-only spawn
// with a bounded timeout. The MCP go-sdk client is used directly here for the
// same reason the runtime's own tests use it — this is the test-only,
// dev-mode-gated client carve-out, not a shipped client.
//
// A non-nil error means the server did not boot cleanly; the caller surfaces
// it so the developer knows the install wrote a config but the server is not
// yet runnable.
func bootCheck(ctx context.Context, binaryPath string) error {
	ctx, cancel := context.WithTimeout(ctx, bootCheckTimeout)
	defer cancel()

	// The server is spawned exactly as the host config launches it: the bare
	// command, communicating over stdio. CommandTransport owns the child's
	// lifecycle — closing the session terminates the process.
	cmd := exec.Command(binaryPath) //nolint:gosec // binaryPath is a Dockyard-built artifact the caller selected
	transport := &mcpsdk.CommandTransport{Command: cmd}

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "dockyard-install-bootcheck", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("%w: server did not complete the MCP initialize handshake: %w",
			ErrBootCheck, err)
	}
	// A successful Connect has already completed initialize. Close the session
	// — this terminates the throwaway server process; no orphan is left.
	if err := session.Close(); err != nil {
		return fmt.Errorf("%w: boot check connected but did not close cleanly: %w",
			ErrBootCheck, err)
	}
	return nil
}

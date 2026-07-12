package installpkg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bootCheckTimeout bounds the install boot check: the spawned server has this
// long to accept a connection and complete MCP discovery before
// the check fails. It is a generous-but-bounded window — a healthy server over
// stdio handshakes in well under a second.
const bootCheckTimeout = 15 * time.Second

// bootCheck verifies the host config launches a working server: it spawns the
// built server binary exactly as the host config does — as a local stdio
// subprocess, connects with modern server/discover negotiation (falling back
// to legacy initialize only when the server explicitly signals it), and tears
// the process down.
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
	transport := &modernFirstTransport{base: &mcpsdk.CommandTransport{Command: cmd}}

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "dockyard-install-bootcheck", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("%w: server did not complete MCP discovery or a recognized legacy fallback: %w",
			ErrBootCheck, err)
	}
	// A successful Connect completed modern discovery or a recognized legacy
	// fallback. Closing terminates the throwaway process; no orphan is left.
	if err := session.Close(); err != nil {
		return fmt.Errorf("%w: boot check connected but did not close cleanly: %w",
			ErrBootCheck, err)
	}
	return nil
}

// modernFirstTransport preserves Client.Connect's modern-first negotiation
// while guarding against the pinned SDK's overly broad fallback on unrelated
// server/discover errors. Remove this adapter once the SDK narrows that policy.
type modernFirstTransport struct {
	base mcpsdk.Transport
}

func (t *modernFirstTransport) Connect(ctx context.Context) (mcpsdk.Connection, error) {
	conn, err := t.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &modernFirstConnection{Connection: conn}, nil
}

type modernFirstConnection struct {
	mcpsdk.Connection
	mu              sync.Mutex
	discoverID      any
	fallbackAllowed bool
	discoverErr     error
}

func (c *modernFirstConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	if req, ok := msg.(*jsonrpc.Request); ok {
		c.mu.Lock()
		switch req.Method {
		case "server/discover":
			c.discoverID = req.ID.Raw()
		case "initialize":
			if c.discoverID != nil && !c.fallbackAllowed {
				err := c.discoverErr
				if err == nil {
					err = errors.New("server/discover did not return a recognized legacy fallback signal")
				}
				c.mu.Unlock()
				return err
			}
		}
		c.mu.Unlock()
	}
	return c.Connection.Write(ctx, msg)
}

func (c *modernFirstConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if err != nil {
		return nil, err
	}
	resp, ok := msg.(*jsonrpc.Response)
	if !ok {
		return msg, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.discoverID == nil || !reflect.DeepEqual(resp.ID.Raw(), c.discoverID) {
		return msg, nil
	}
	c.fallbackAllowed = recognizedLegacyFallback(resp)
	if resp.Error != nil && !c.fallbackAllowed {
		c.discoverErr = fmt.Errorf("server/discover failed without a legacy fallback signal: %w", resp.Error)
	}
	return msg, nil
}

func recognizedLegacyFallback(resp *jsonrpc.Response) bool {
	if resp.Error != nil {
		var rpcErr *jsonrpc.Error
		return errors.As(resp.Error, &rpcErr) &&
			(rpcErr.Code == jsonrpc.CodeMethodNotFound || rpcErr.Code == mcpsdk.CodeUnsupportedProtocolVersion)
	}
	var result struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	if json.Unmarshal(resp.Result, &result) != nil {
		return false
	}
	for _, version := range result.SupportedVersions {
		if version == "2026-07-28" {
			return false
		}
	}
	return len(result.SupportedVersions) > 0
}

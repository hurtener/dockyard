package apps

import (
	"fmt"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/server"
)

// ExtensionID is the MCP Apps extension identifier, exactly as registered in
// the MCP capability registry (SEP-1865).
const ExtensionID = protocolcodec.ExtensionApps

// MIMETypeApp is the only MCP Apps resource MIME type defined by the MVP spec
// (text/html;profile=mcp-app). Routing it through one constant means a future
// profile type is a one-line change (brief 01 §5, sharp edge 5).
const MIMETypeApp = protocolcodec.MIMETypeApp

// ExtensionCapability returns the server.ExtensionCapability that advertises the
// io.modelcontextprotocol/ui extension during the initialize handshake
// (RFC §7.1, brief 01 §2.7). It advertises mimeTypes ["text/html;profile=mcp-app"]
// — the single MVP resource type.
//
// Pass the result in server.Options.Extensions when constructing the server:
//
//	cap, err := apps.ExtensionCapability()
//	srv, err := server.New(info, &server.Options{Extensions: []server.ExtensionCapability{cap}})
//
// The capability JSON is produced by internal/protocolcodec; this package never
// hand-builds the wire shape (P3, RFC §5.4). A host that does not advertise the
// extension still gets a fully working plain MCP server — advertising it costs
// nothing and gates no behaviour (RFC §7.5).
func ExtensionCapability() (server.ExtensionCapability, error) {
	codec := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)
	raw, err := codec.EncodeAppsExtensionCapability(protocolcodec.AppsExtensionCapability{
		MIMETypes: []string{MIMETypeApp},
	})
	if err != nil {
		return server.ExtensionCapability{}, fmt.Errorf(
			"dockyard/runtime/apps: encode Apps extension capability: %w", err)
	}
	return server.ExtensionCapability{Name: ExtensionID, Settings: raw}, nil
}

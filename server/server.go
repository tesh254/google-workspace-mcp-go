// Package server initializes and configures the MCP server.
package server

import (
	"log/slog"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ServerName = "google-workspace-mcp"

// ServerVersion is the server version. It is a variable rather than a
// constant so that release builds can stamp the real version at link time
// via -X github.com/magks/google-workspace-mcp-go/server.ServerVersion.
var ServerVersion = "0.1.0"

// ModernProtocolVersion is the only MCP protocol version this server speaks.
// The server is stateless per spec revision 2026-07-28: there is no
// initialize handshake, every request carries its version and capabilities
// in _meta, and clients discover the server via server/discover.
//
// The spec version is spelled out here because the go-sdk keeps its version
// constants internal.
const ModernProtocolVersion = "2026-07-28"

// Config holds server configuration from CLI flags.
type Config struct {
	Tools      []string
	ToolTier   string
	Transport  string
	SingleUser bool
	ReadOnly   bool
}

// New creates a new MCP server with the given configuration.
//
// It always speaks modern-protocol-version 2026-07-28: clients negotiate per
// request, and legacy initialize-handshake clients are rejected with an
// error that lists this version.
func New(cfg Config) *mcp.Server {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	return mcp.NewServer(
		&mcp.Implementation{Name: ServerName, Version: ServerVersion},
		&mcp.ServerOptions{
			Logger:                    logger,
			SupportedProtocolVersions: []string{ModernProtocolVersion},
		},
	)
}

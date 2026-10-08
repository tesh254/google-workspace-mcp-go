package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/auth"
	"github.com/magks/google-workspace-mcp-go/server"
	"github.com/magks/google-workspace-mcp-go/tools"
)

// validTools is the set of accepted --tools values.
var validTools = map[string]bool{
	"gmail": true, "drive": true, "calendar": true,
	"docs": true, "sheets": true, "chat": true,
	"forms": true, "slides": true, "tasks": true,
	"contacts": true, "search": true, "appscript": true,
}

// validTiers is the set of accepted --tool-tier values.
var validTiers = map[string]bool{
	"core": true, "extended": true, "complete": true,
}

// validTransports is the set of accepted --transport values.
var validTransports = map[string]bool{
	"stdio": true, "streamable-http": true,
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}

	s := server.New(cfg)
	tools.RegisterAllTools(s, cfg)
	tools.FilterTools(s, cfg)

	switch cfg.Transport {
	case "streamable-http":
		return serveStreamableHTTP(cfg, s)
	default:
		// Run blocks until the client disconnects or the process is
		// interrupted; on a clean disconnect it returns nil.
		return s.Run(context.Background(), &mcp.StdioTransport{})
	}
}

// serveStreamableHTTP serves the MCP server over the stateless Streamable
// HTTP transport per protocol revision 2026-07-28:
//   - Stateless mode: no session IDs (Mcp-Session-Id is never set), each
//     request is processed independently; GET/DELETE on the MCP endpoint
//     return 405.
//   - The handler validates that the MCP-Protocol-Version header matches
//     the _meta protocol version in the body and rejects unsupported
//     versions per the spec.
//
// The endpoint is additionally protected against cross-origin and DNS
// rebinding requests with the cross-origin protection middleware; the spec
// requires Origin validation for local HTTP servers.
func serveStreamableHTTP(cfg server.Config, s *mcp.Server) error {
	addr := fmt.Sprintf("%s:%d", cfg.HTTPHost, cfg.HTTPPort)
	var handler http.Handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		Stateless: true,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	// Bearer authentication per the MCP authorization framework, enabled
	// when MCP_AUTH_JWKS_URL is configured. JWTs are verified against the
	// authorization server's JWKS; failures respond 401 with a
	// WWW-Authenticate header advertising the protected resource metadata.
	if authJWKS := os.Getenv("MCP_AUTH_JWKS_URL"); authJWKS != "" {
		middleware, err := auth.BearerMiddleware(&auth.BearerConfig{
			JWKSURL:             authJWKS,
			Issuer:              os.Getenv("MCP_AUTH_ISSUER"),
			Audience:            os.Getenv("MCP_AUTH_AUDIENCE"),
			ResourceMetadataURL: os.Getenv("MCP_RESOURCE_METADATA_URL"),
			Scopes:              strings.Fields(os.Getenv("MCP_AUTH_SCOPES")),
		})
		if err != nil {
			return fmt.Errorf("configuring bearer auth: %w", err)
		}
		handler = middleware(handler)
		fmt.Fprintln(os.Stderr, "bearer-token authentication enabled (MCP_AUTH_JWKS_URL set)")
	}

	// Cross-origin / DNS rebinding protection wraps the outermost layer.
	protected := http.NewCrossOriginProtection().Handler(handler)
	fmt.Fprintf(os.Stderr, "serving streamable-http (stateless) on http://%s/mcp\n", addr)
	return http.ListenAndServe(addr, protected)
}

// parseFlags parses CLI arguments into a server.Config.
func parseFlags(args []string) (server.Config, error) {
	fs := flag.NewFlagSet("google-workspace-mcp-go", flag.ContinueOnError)

	var toolsRaw string
	fs.StringVar(&toolsRaw, "tools", "", "space-separated list of services to enable (e.g. gmail drive calendar)")
	var toolTier string
	fs.StringVar(&toolTier, "tool-tier", "", "tool tier: core, extended, or complete")
	var transport string
	fs.StringVar(&transport, "transport", "stdio", "transport mode: stdio or streamable-http")
	var singleUser bool
	fs.BoolVar(&singleUser, "single-user", false, "enable single-user mode")
	var readOnly bool
	fs.BoolVar(&readOnly, "read-only", false, "enable read-only mode (no write tools)")
	var httpHost string
	fs.StringVar(&httpHost, "http-host", "127.0.0.1", "host to bind for streamable-http transport (bind to localhost by default)")
	var httpPort int
	fs.IntVar(&httpPort, "http-port", 8080, "port to bind for streamable-http transport")

	if err := fs.Parse(args); err != nil {
		return server.Config{}, err
	}

	// Validate and collect tools.
	var tools []string
	if toolsRaw != "" {
		for _, t := range strings.Fields(toolsRaw) {
			if !validTools[t] {
				return server.Config{}, fmt.Errorf("unknown tool %q; valid tools: gmail, drive, calendar, docs, sheets, chat, forms, slides, tasks, contacts, search, appscript", t)
			}
			tools = append(tools, t)
		}
	}

	// Validate tool tier.
	if toolTier != "" && !validTiers[toolTier] {
		return server.Config{}, fmt.Errorf("unknown tool-tier %q; valid tiers: core, extended, complete", toolTier)
	}

	// Validate transport.
	if !validTransports[transport] {
		return server.Config{}, fmt.Errorf("unknown transport %q; valid transports: stdio, streamable-http", transport)
	}

	return server.Config{
		Tools:      tools,
		ToolTier:   toolTier,
		Transport:  transport,
		SingleUser: singleUser,
		ReadOnly:   readOnly,
		HTTPHost:   httpHost,
		HTTPPort:   httpPort,
	}, nil
}

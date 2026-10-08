// Package tools provides tool registration and handlers for all Google Workspace services.
package tools

import (
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/server"
)

// RegisterAllTools registers tools for all enabled services based on config.
// If cfg.Tools is empty, all services are registered.
func RegisterAllTools(s *mcp.Server, cfg server.Config) {
	enabled := make(map[string]bool, len(cfg.Tools))
	for _, t := range cfg.Tools {
		enabled[t] = true
	}
	allTools := len(cfg.Tools) == 0

	type registrar struct {
		name string
		fn   func(*mcp.Server, server.Config)
	}

	registrars := []registrar{
		{"gmail", RegisterGmailTools},
		{"drive", RegisterDriveTools},
		{"calendar", RegisterCalendarTools},
		{"docs", RegisterDocsTools},
		{"sheets", RegisterSheetsTools},
		{"chat", RegisterChatTools},
		{"forms", RegisterFormsTools},
		{"slides", RegisterSlidesTools},
		{"tasks", RegisterTasksTools},
		{"contacts", RegisterContactsTools},
		{"search", RegisterSearchTools},
		{"appscript", RegisterAppScriptTools},
	}

	for _, r := range registrars {
		if allTools || enabled[r.name] {
			r.fn(s, cfg)
		}
	}

	// Register meta-tools (not tied to a specific service).
	// start_google_auth is under gmail/complete in tier config but is
	// registered independently. It loads when gmail is enabled or all tools are loaded.
	if allTools || enabled["gmail"] {
		RegisterAuthTools(s)
	}
}

package tools

import (
	"context"
	"os"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/auth"
	google "github.com/magks/google-workspace-mcp-go/internal/google"
)

// startGoogleAuthIn is the input schema for start_google_auth.
type startGoogleAuthIn struct {
	ServiceName     string `json:"service_name" jsonschema:"Name of the Google service requiring authentication (e.g., 'Google Calendar', 'Gmail')"`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"User's Google email address for authentication"`
}

// RegisterAuthTools registers the start_google_auth meta-tool.
// It is filtered out when MCP_ENABLE_OAUTH21=true.
func RegisterAuthTools(s *mcp.Server) {
	if isOAuth21Enabled() {
		return
	}

	store := auth.NewCredentialStore()

	mcp.AddTool(
		s,
		&mcp.Tool{
			Name: "start_google_auth",
			Description: "Manually initiate Google OAuth authentication flow. " +
				"NOTE: This is a legacy OAuth 2.0 tool and is disabled when OAuth 2.1 is enabled. " +
				"The authentication system automatically handles credential checks and prompts for " +
				"authentication when needed. Only use this tool if: " +
				"1. You need to re-authenticate with different credentials. " +
				"2. You want to proactively authenticate before using other tools. " +
				"3. The automatic authentication flow failed and you need to retry. " +
				"In most cases, simply try calling the Google Workspace tool you need - it will " +
				"automatically handle authentication if required.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, in startGoogleAuthIn) (*mcp.CallToolResult, any, error) {
			if strings.TrimSpace(in.ServiceName) == "" {
				return nil, nil, toolErr("service_name is required")
			}

			userEmail, emailErr := resolveEmail(in.UserGoogleEmail)
			if emailErr != nil {
				return nil, nil, toolErr("user_google_email is required")
			}

			msg, err := auth.StartAuthFlow(ctx, in.ServiceName, userEmail, store, func(email string) {
				// Invalidate cached client so next tool call picks up new credentials.
				google.DefaultClientCache().Invalidate(email)
			})
			if err != nil {
				return nil, nil, toolErr("**Authentication Error:** %v", err)
			}

			return textResult(msg)
		},
	)
}

// isOAuth21Enabled checks if the MCP_ENABLE_OAUTH21 env var is set to "true".
func isOAuth21Enabled() bool {
	return strings.ToLower(os.Getenv("MCP_ENABLE_OAUTH21")) == "true"
}

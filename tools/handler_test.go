package tools

import (
	"context"
	"strings"
	"testing"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/server"
)

// newToolTestSession connects an in-memory client to an MCP server with all
// tools registered, a temp credential dir (empty, so no single-user fallback),
// and a fake email env var so that resolveEmail succeeds.
func newToolTestSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", t.TempDir())
	t.Setenv("USER_GOOGLE_EMAIL", "test@example.com")
	cfg := server.Config{}
	s := server.New(cfg)
	RegisterAllTools(s, cfg)
	FilterTools(s, cfg)

	ctx := context.Background()
	sTransport, cTransport := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, sTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "tools-test", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(ctx, cTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callTool sends a tools/call request through the client session and returns
// the text content and isError flag. It fails the test if the call returns a
// protocol-level error rather than a tool-level result.
func callTool(t *testing.T, cs *mcp.ClientSession, toolName string, args map[string]any) (text string, isError bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("protocol error calling %s: %v", toolName, err)
	}
	if len(res.Content) == 0 {
		return "", res.IsError
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text, res.IsError
}

// --- Smoke tests ---

func TestHandlerNonexistentTool(t *testing.T) {
	cs := newToolTestSession(t)
	_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "this_tool_does_not_exist",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent tool, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error, got %q", err.Error())
	}
}

func TestHandlerMissingRequiredParam(t *testing.T) {
	cs := newToolTestSession(t)
	// search_gmail_messages requires "query" param.
	text, isError := callTool(t, cs, "search_gmail_messages", nil)
	if !isError {
		t.Fatal("expected isError=true for missing query param")
	}
	if !strings.Contains(strings.ToLower(text), "query") {
		t.Errorf("expected error to mention 'query', got %q", text)
	}
}

package tools

import (
	"context"
	"strings"
	"testing"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/internal/google"
)

// chatTestGetClient wires a real credential store backed by an empty temp
// directory (no stored credentials) plus a fixed user email env var, matching
// the previous newToolTestServer environment: every handler call using it
// fails at authentication.
func chatTestGetClient(t *testing.T) httpClientFunc {
	t.Helper()
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", t.TempDir())
	t.Setenv("USER_GOOGLE_EMAIL", "test@example.com")
	return clientFuncFromCache(google.DefaultClientCache())
}

// chatCallReq builds a minimal CallToolRequest for direct handler invocation.
func chatCallReq(name string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: name}}
}

// --- list_spaces ---
// list_spaces has no strictly required params (email resolved via env),
// so the first error path is auth failure.

func TestChatHandlerListSpacesAuthFailure(t *testing.T) {
	getClient := chatTestGetClient(t)
	handler := handleListSpaces(getClient)
	_, _, err := handler(context.Background(), chatCallReq("list_spaces"), listSpacesIn{})
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "authentication") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", err.Error())
	}
}

// --- get_messages ---
// Required-argument error paths are now enforced by the SDK's input schema,
// so the missing-space_id test is dropped.

func TestChatHandlerGetMessagesAuthFailure(t *testing.T) {
	getClient := chatTestGetClient(t)
	handler := handleGetMessages(getClient)
	_, _, err := handler(context.Background(), chatCallReq("get_messages"), getMessagesIn{
		SpaceID: "spaces/abc123",
	})
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "authentication") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", err.Error())
	}
}

// --- send_message ---
// Required-argument error paths are now enforced by the SDK's input schema,
// so the missing-space_id and missing-message_text tests are dropped.

// --- search_messages ---
// Required-argument error paths are now enforced by the SDK's input schema,
// so the missing-query test is dropped.

func TestChatHandlerSearchMessagesAuthFailure(t *testing.T) {
	getClient := chatTestGetClient(t)
	handler := handleSearchMessages(getClient)
	_, _, err := handler(context.Background(), chatCallReq("search_messages"), searchMessagesIn{
		Query: "test message",
	})
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "authentication") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", err.Error())
	}
}

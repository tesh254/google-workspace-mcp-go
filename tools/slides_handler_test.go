package tools

import (
	"context"
	"strings"
	"testing"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/internal/google"
)

// slidesTestGetClient wires a real credential store backed by an empty temp
// directory (no stored credentials) plus a fixed user email env var, matching
// the previous newToolTestServer environment: every handler call using it
// fails at authentication.
func slidesTestGetClient(t *testing.T) httpClientFunc {
	t.Helper()
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", t.TempDir())
	t.Setenv("USER_GOOGLE_EMAIL", "test@example.com")
	return clientFuncFromCache(google.DefaultClientCache())
}

// slidesCallReq builds a minimal CallToolRequest for direct handler invocation.
func slidesCallReq(name string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: name}}
}

// --- create_presentation ---
// create_presentation has no strictly required params (title defaults,
// email resolved via env), so the first error path is auth failure.

func TestSlidesHandlerCreatePresentationAuthFailure(t *testing.T) {
	getClient := slidesTestGetClient(t)
	handler := handleCreatePresentation(getClient)
	_, _, err := handler(context.Background(), slidesCallReq("create_presentation"), createPresentationIn{})
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "authentication") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", err.Error())
	}
}

// --- get_presentation ---

func TestSlidesHandlerGetPresentationAuthFailure(t *testing.T) {
	getClient := slidesTestGetClient(t)
	handler := handleGetPresentation(getClient)
	_, _, err := handler(context.Background(), slidesCallReq("get_presentation"), getPresentationIn{
		PresentationID: "pres123",
	})
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "authentication") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", err.Error())
	}
}

// --- batch_update_presentation ---

func TestSlidesHandlerBatchUpdatePresentationMissingRequests(t *testing.T) {
	getClient := slidesTestGetClient(t)
	handler := handleBatchUpdatePresentation(getClient)
	_, _, err := handler(context.Background(), slidesCallReq("batch_update_presentation"), batchUpdatePresentationIn{
		PresentationID: "pres123",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "requests") {
		t.Errorf("expected error mentioning 'requests', got %q", err.Error())
	}
}

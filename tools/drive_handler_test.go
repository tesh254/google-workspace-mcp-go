package tools

import (
	"context"
	"strings"
	"testing"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/internal/google"
)

// driveTestGetClient wires a real credential store backed by an empty temp
// directory (no stored credentials) plus a fixed user email env var, matching
// the previous newToolTestServer environment: every handler call using it
// fails at authentication.
func driveTestGetClient(t *testing.T) httpClientFunc {
	t.Helper()
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", t.TempDir())
	t.Setenv("USER_GOOGLE_EMAIL", "test@example.com")
	return clientFuncFromCache(google.DefaultClientCache())
}

// driveCallReq builds a minimal CallToolRequest for direct handler invocation.
func driveCallReq(name string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: name}}
}

// --- search_drive_files ---

func TestDriveHandlerSearchAuthFailure(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleSearchDriveFiles(getClient)
	_, _, err := handler(context.Background(), driveCallReq("search_drive_files"), searchDriveFilesIn{
		Query: "test document",
	})
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", err.Error())
	}
}

// --- get_drive_file_content ---

func TestDriveHandlerGetFileContentAuthFailure(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleGetDriveFileContent(getClient)
	_, _, err := handler(context.Background(), driveCallReq("get_drive_file_content"), getDriveFileContentIn{
		FileID: "abc123",
	})
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", err.Error())
	}
}

// --- list_drive_items ---
// list_drive_items has no strictly required params (folder_id defaults to "root"),
// so the first error path is auth failure.

func TestDriveHandlerListDriveItemsAuthFailure(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleListDriveItems(getClient)
	_, _, err := handler(context.Background(), driveCallReq("list_drive_items"), listDriveItemsIn{})
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", err.Error())
	}
}

// --- create_drive_file ---

func TestDriveHandlerCreateFileMissingContentAndURL(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleCreateDriveFile(getClient)
	_, _, err := handler(context.Background(), driveCallReq("create_drive_file"), createDriveFileIn{
		FileName: "test.txt",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "content") && !strings.Contains(lower, "fileurl") {
		t.Errorf("expected error mentioning 'content' or 'fileUrl', got %q", err.Error())
	}
}

// --- import_to_google_doc ---

func TestDriveHandlerImportToGoogleDocMissingSource(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleImportToGoogleDoc(getClient)
	_, _, err := handler(context.Background(), driveCallReq("import_to_google_doc"), importToGoogleDocIn{
		FileName: "test.md",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "content") && !strings.Contains(lower, "file_path") && !strings.Contains(lower, "file_url") {
		t.Errorf("expected error mentioning source params, got %q", err.Error())
	}
}

func TestDriveHandlerImportToGoogleDocMultipleSources(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleImportToGoogleDoc(getClient)
	_, _, err := handler(context.Background(), driveCallReq("import_to_google_doc"), importToGoogleDocIn{
		FileName: "test.md",
		Content:  "# Hello",
		FilePath: "/tmp/test.md",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "only one") && !strings.Contains(lower, "provide only") {
		t.Errorf("expected error about providing only one source, got %q", err.Error())
	}
}

func TestDriveHandlerImportToGoogleDocUnsupportedFormat(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleImportToGoogleDoc(getClient)
	_, _, err := handler(context.Background(), driveCallReq("import_to_google_doc"), importToGoogleDocIn{
		FileName:     "test.xyz",
		Content:      "some content",
		SourceFormat: "xyz",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "unsupported") {
		t.Errorf("expected error mentioning 'unsupported', got %q", err.Error())
	}
}

// --- share_drive_file ---

func TestDriveHandlerShareFileMissingShareWith(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleShareDriveFile(getClient)
	// share_type defaults to "user", which requires share_with.
	// This param check happens before auth.
	_, _, err := handler(context.Background(), driveCallReq("share_drive_file"), shareDriveFileIn{
		FileID: "abc123",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "share_with") {
		t.Errorf("expected error mentioning 'share_with', got %q", err.Error())
	}
}

// --- Additional validation tests ---

func TestDriveHandlerShareFileInvalidRole(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleShareDriveFile(getClient)
	_, _, err := handler(context.Background(), driveCallReq("share_drive_file"), shareDriveFileIn{
		FileID:    "abc123",
		ShareWith: "user@example.com",
		Role:      "superadmin",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "invalid role") {
		t.Errorf("expected error mentioning 'invalid role', got %q", err.Error())
	}
}

func TestDriveHandlerShareFileInvalidShareType(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleShareDriveFile(getClient)
	_, _, err := handler(context.Background(), driveCallReq("share_drive_file"), shareDriveFileIn{
		FileID:    "abc123",
		ShareWith: "user@example.com",
		ShareType: "martian",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "invalid share_type") {
		t.Errorf("expected error mentioning 'invalid share_type', got %q", err.Error())
	}
}

// --- batch_share_drive_file ---

func TestDriveHandlerBatchShareFileMissingRecipients(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleBatchShareDriveFile(getClient)
	_, _, err := handler(context.Background(), driveCallReq("batch_share_drive_file"), batchShareDriveFileIn{
		FileID: "abc123",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "recipients") {
		t.Errorf("expected error mentioning 'recipients', got %q", err.Error())
	}
}

func TestDriveHandlerBatchShareFileEmptyRecipients(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleBatchShareDriveFile(getClient)
	_, _, err := handler(context.Background(), driveCallReq("batch_share_drive_file"), batchShareDriveFileIn{
		FileID:     "abc123",
		Recipients: []map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "recipients") && !strings.Contains(lower, "empty") {
		t.Errorf("expected error mentioning empty recipients, got %q", err.Error())
	}
}

// --- update_drive_permission ---

func TestDriveHandlerUpdatePermissionMissingRoleAndExpiration(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleUpdateDrivePermission(getClient)
	_, _, err := handler(context.Background(), driveCallReq("update_drive_permission"), updateDrivePermissionIn{
		FileID:       "abc123",
		PermissionID: "perm456",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "role") && !strings.Contains(lower, "expiration") {
		t.Errorf("expected error mentioning 'role' or 'expiration_time', got %q", err.Error())
	}
}

func TestDriveHandlerUpdatePermissionInvalidRole(t *testing.T) {
	getClient := driveTestGetClient(t)
	handler := handleUpdateDrivePermission(getClient)
	_, _, err := handler(context.Background(), driveCallReq("update_drive_permission"), updateDrivePermissionIn{
		FileID:       "abc123",
		PermissionID: "perm456",
		Role:         "superadmin",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "invalid role") {
		t.Errorf("expected error mentioning 'invalid role', got %q", err.Error())
	}
}

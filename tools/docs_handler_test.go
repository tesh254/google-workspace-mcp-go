package tools

import (
	"strings"
	"testing"
)

// --- search_docs ---

func TestDocsHandlerSearchDocsAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "search_docs", map[string]any{
		"query": "test doc",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- get_doc_content ---

func TestDocsHandlerGetDocContentAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_doc_content", map[string]any{
		"document_id": "doc123",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- list_docs_in_folder ---
// list_docs_in_folder has no strictly required params (folder_id defaults to "root"),
// so the first error path is auth failure.

func TestDocsHandlerListDocsInFolderAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "list_docs_in_folder", nil)
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- create_doc ---

func TestDocsHandlerCreateDocAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "create_doc", map[string]any{
		"title": "Test Document",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- modify_doc_text ---

func TestDocsHandlerModifyDocTextMissingTextAndFormatting(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "modify_doc_text", map[string]any{
		"document_id": "doc123",
		"start_index": 1,
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "text") && !strings.Contains(lower, "formatting") {
		t.Errorf("expected error mentioning 'text' or 'formatting', got %q", text)
	}
}

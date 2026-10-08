package tools

import (
	"strings"
	"testing"
)

// --- create_form ---

func TestFormsHandlerCreateFormAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "create_form", map[string]any{
		"title": "Test Form",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "authentication") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- get_form ---

func TestFormsHandlerGetFormAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_form", map[string]any{
		"form_id": "form123",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "authentication") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

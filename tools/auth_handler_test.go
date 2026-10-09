package tools

import (
	"strings"
	"testing"
)

// --- start_google_auth ---

func TestAuthHandlerStartGoogleAuthMissingServiceName(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "start_google_auth", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "service_name") {
		t.Errorf("expected error mentioning 'service_name', got %q", text)
	}
}

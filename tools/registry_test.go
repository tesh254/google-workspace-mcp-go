package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- resolveEmail ---

func TestResolveEmailParamTakesPriority(t *testing.T) {
	// Set env var to prove it's ignored when param is present.
	t.Setenv("USER_GOOGLE_EMAIL", "env@example.com")

	got, err := resolveEmail("param@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "param@example.com" {
		t.Errorf("got %q, want %q", got, "param@example.com")
	}
}

func TestResolveEmailFallsBackToEnvVar(t *testing.T) {
	t.Setenv("USER_GOOGLE_EMAIL", "env@example.com")
	// Point credential dir to empty temp dir so fallback 3 doesn't trigger.
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", t.TempDir())

	got, err := resolveEmail("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env@example.com" {
		t.Errorf("got %q, want %q", got, "env@example.com")
	}
}

func TestResolveEmailFallsBackToSingleCredFile(t *testing.T) {
	t.Setenv("USER_GOOGLE_EMAIL", "")
	credDir := t.TempDir()
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", credDir)

	// Create a single credential file.
	if err := os.WriteFile(filepath.Join(credDir, "single@example.com.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveEmail("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "single@example.com" {
		t.Errorf("got %q, want %q", got, "single@example.com")
	}
}

func TestResolveEmailErrorWhenNoSource(t *testing.T) {
	t.Setenv("USER_GOOGLE_EMAIL", "")
	// Point to empty dir — no credential files.
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", t.TempDir())

	_, err := resolveEmail("")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "user_google_email is required") {
		t.Errorf("error should mention user_google_email is required, got: %v", err)
	}
}

func TestResolveEmailWhitespaceOnlyParamTreatedAsEmpty(t *testing.T) {
	t.Setenv("USER_GOOGLE_EMAIL", "env@example.com")

	got, err := resolveEmail("   \t  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to env var since param is whitespace-only.
	if got != "env@example.com" {
		t.Errorf("got %q, want %q", got, "env@example.com")
	}
}

func TestResolveEmailWhitespaceOnlyEnvVarTreatedAsEmpty(t *testing.T) {
	t.Setenv("USER_GOOGLE_EMAIL", "   \t  ")
	credDir := t.TempDir()
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", credDir)

	// Create a single credential file so fallback 3 works.
	if err := os.WriteFile(filepath.Join(credDir, "cred@example.com.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveEmail("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should skip env var and fall back to credential file.
	if got != "cred@example.com" {
		t.Errorf("got %q, want %q", got, "cred@example.com")
	}
}

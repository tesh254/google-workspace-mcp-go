package tools

import (
	"strings"
	"testing"
)

// Search tools use GOOGLE_PSE_API_KEY and GOOGLE_PSE_ENGINE_ID env vars
// rather than Google OAuth credentials. The error path is:
// resolveEmail → RequireString params → newCustomSearchService (env var check).

// --- search_custom ---

func TestSearchHandlerSearchCustomMissingQuery(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "search_custom", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "q") {
		t.Errorf("expected error mentioning 'q', got %q", text)
	}
}

func TestSearchHandlerSearchCustomMissingAPIKey(t *testing.T) {
	cs := newToolTestSession(t)
	// Ensure PSE env vars are not set
	t.Setenv("GOOGLE_PSE_API_KEY", "")
	t.Setenv("GOOGLE_PSE_ENGINE_ID", "")
	text, isError := callTool(t, cs, "search_custom", map[string]any{
		"q": "test query",
	})
	if !isError {
		t.Fatal("expected isError=true for missing API key")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "google_pse_api_key") {
		t.Errorf("expected error mentioning 'GOOGLE_PSE_API_KEY', got %q", text)
	}
}

// --- get_search_engine_info ---
// No required params beyond email; first real error is missing API key.

func TestSearchHandlerGetSearchEngineInfoMissingAPIKey(t *testing.T) {
	cs := newToolTestSession(t)
	t.Setenv("GOOGLE_PSE_API_KEY", "")
	t.Setenv("GOOGLE_PSE_ENGINE_ID", "")
	text, isError := callTool(t, cs, "get_search_engine_info", nil)
	if !isError {
		t.Fatal("expected isError=true for missing API key")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "google_pse_api_key") {
		t.Errorf("expected error mentioning 'GOOGLE_PSE_API_KEY', got %q", text)
	}
}

// --- search_custom_siterestrict ---

func TestSearchHandlerSearchCustomSiterestrictMissingQuery(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "search_custom_siterestrict", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "q") {
		t.Errorf("expected error mentioning 'q', got %q", text)
	}
}

func TestSearchHandlerSearchCustomSiterestrictMissingSites(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "search_custom_siterestrict", map[string]any{
		"q": "test query",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "sites") {
		t.Errorf("expected error mentioning 'sites', got %q", text)
	}
}

func TestSearchHandlerSearchCustomSiterestrictMissingAPIKey(t *testing.T) {
	cs := newToolTestSession(t)
	t.Setenv("GOOGLE_PSE_API_KEY", "")
	t.Setenv("GOOGLE_PSE_ENGINE_ID", "")
	text, isError := callTool(t, cs, "search_custom_siterestrict", map[string]any{
		"q":     "test query",
		"sites": []any{"example.com"},
	})
	if !isError {
		t.Fatal("expected isError=true for missing API key")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "google_pse_api_key") {
		t.Errorf("expected error mentioning 'GOOGLE_PSE_API_KEY', got %q", text)
	}
}

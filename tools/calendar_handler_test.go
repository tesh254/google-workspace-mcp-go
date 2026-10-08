package tools

import (
	"strings"
	"testing"
)

// Required-argument error paths (missing time_min/time_max, summary,
// start_time, end_time, event_id, etc.) are no longer tested here: with the
// official go-sdk, input validation against the tool's schema happens in the
// SDK wrapper before the handler runs. Only handler-level validation paths
// remain below.

// --- list_calendars ---
// list_calendars has no strictly required params (email resolved via env),
// so the first error path is auth failure.

func TestCalendarHandlerListCalendarsAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "list_calendars", nil)
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- get_events ---
// get_events has no strictly required params (defaults to primary calendar),
// so the first error path is auth failure.

func TestCalendarHandlerGetEventsAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_events", nil)
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- query_freebusy ---

func TestCalendarHandlerQueryFreebusyAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "query_freebusy", map[string]any{
		"time_min": "2026-01-01T00:00:00Z",
		"time_max": "2026-01-02T00:00:00Z",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- create_event ---

func TestCalendarHandlerCreateEventAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "create_event", map[string]any{
		"summary":    "Test Event",
		"start_time": "2026-01-01T10:00:00Z",
		"end_time":   "2026-01-01T11:00:00Z",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- modify_event ---

func TestCalendarHandlerModifyEventAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "modify_event", map[string]any{
		"event_id": "evt123",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- delete_event ---

func TestCalendarHandlerDeleteEventAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "delete_event", map[string]any{
		"event_id": "evt123",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

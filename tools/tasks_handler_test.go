package tools

import (
	"strings"
	"testing"
)

// Required-argument error paths (missing task_list_id, task_id, title) are no
// longer tested: with the official go-sdk, input validation against the
// tool's schema happens in the SDK wrapper before the handler runs. Only
// handler-level paths remain below.

// --- list_task_lists ---
// list_task_lists has no strictly required params (email resolved via env),
// so the first error path is auth failure.

func TestTasksHandlerListTaskListsAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "list_task_lists", nil)
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- get_task_list ---

func TestTasksHandlerGetTaskListAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_task_list", map[string]any{
		"task_list_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- clear_completed_tasks ---

func TestTasksHandlerClearCompletedTasksAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "clear_completed_tasks", map[string]any{
		"task_list_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

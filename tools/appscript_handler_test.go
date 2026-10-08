package tools

import (
	"strings"
	"testing"
)

// --- list_script_projects ---
// No required params beyond email; first error is auth failure.

func TestAppScriptHandlerListScriptProjectsAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "list_script_projects", nil)
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- get_script_project ---

func TestAppScriptHandlerGetScriptProjectMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_script_project", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

func TestAppScriptHandlerGetScriptProjectAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_script_project", map[string]any{
		"script_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- get_script_content ---

func TestAppScriptHandlerGetScriptContentMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_script_content", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

func TestAppScriptHandlerGetScriptContentMissingFileName(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_script_content", map[string]any{
		"script_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "file_name") {
		t.Errorf("expected error mentioning 'file_name', got %q", text)
	}
}

// --- list_deployments ---

func TestAppScriptHandlerListDeploymentsMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "list_deployments", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

// --- list_script_processes ---
// No required params beyond email; first error is auth failure.

func TestAppScriptHandlerListScriptProcessesAuthFailure(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "list_script_processes", nil)
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// --- list_versions ---

func TestAppScriptHandlerListVersionsMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "list_versions", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

// --- get_version ---

func TestAppScriptHandlerGetVersionMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_version", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

func TestAppScriptHandlerGetVersionMissingVersionNumber(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_version", map[string]any{
		"script_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "version_number") {
		t.Errorf("expected error mentioning 'version_number', got %q", text)
	}
}

// --- get_script_metrics ---

func TestAppScriptHandlerGetScriptMetricsMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_script_metrics", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

// --- create_script_project ---

func TestAppScriptHandlerCreateScriptProjectMissingTitle(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "create_script_project", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "title") {
		t.Errorf("expected error mentioning 'title', got %q", text)
	}
}

// --- update_script_content ---

func TestAppScriptHandlerUpdateScriptContentMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "update_script_content", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

func TestAppScriptHandlerUpdateScriptContentMissingFiles(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "update_script_content", map[string]any{
		"script_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "files") {
		t.Errorf("expected error mentioning 'files', got %q", text)
	}
}

// --- run_script_function ---

func TestAppScriptHandlerRunScriptFunctionMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "run_script_function", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

func TestAppScriptHandlerRunScriptFunctionMissingFunctionName(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "run_script_function", map[string]any{
		"script_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "function_name") {
		t.Errorf("expected error mentioning 'function_name', got %q", text)
	}
}

// --- create_deployment ---

func TestAppScriptHandlerCreateDeploymentMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "create_deployment", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

func TestAppScriptHandlerCreateDeploymentMissingDescription(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "create_deployment", map[string]any{
		"script_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "description") {
		t.Errorf("expected error mentioning 'description', got %q", text)
	}
}

// --- update_deployment ---

func TestAppScriptHandlerUpdateDeploymentMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "update_deployment", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

func TestAppScriptHandlerUpdateDeploymentMissingDeploymentID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "update_deployment", map[string]any{
		"script_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "deployment_id") {
		t.Errorf("expected error mentioning 'deployment_id', got %q", text)
	}
}

// --- delete_deployment ---

func TestAppScriptHandlerDeleteDeploymentMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "delete_deployment", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

func TestAppScriptHandlerDeleteDeploymentMissingDeploymentID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "delete_deployment", map[string]any{
		"script_id": "abc123",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "deployment_id") {
		t.Errorf("expected error mentioning 'deployment_id', got %q", text)
	}
}

// --- delete_script_project ---

func TestAppScriptHandlerDeleteScriptProjectMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "delete_script_project", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

// --- create_version ---

func TestAppScriptHandlerCreateVersionMissingScriptID(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "create_version", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "script_id") {
		t.Errorf("expected error mentioning 'script_id', got %q", text)
	}
}

// --- generate_trigger_code ---
// generate_trigger_code does NOT require auth — it generates code locally.
// It requires trigger_type and function_name.

func TestAppScriptHandlerGenerateTriggerCodeMissingTriggerType(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "generate_trigger_code", nil)
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "trigger_type") {
		t.Errorf("expected error mentioning 'trigger_type', got %q", text)
	}
}

func TestAppScriptHandlerGenerateTriggerCodeMissingFunctionName(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "generate_trigger_code", map[string]any{
		"trigger_type": "time_daily",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "function_name") {
		t.Errorf("expected error mentioning 'function_name', got %q", text)
	}
}

func TestAppScriptHandlerGenerateTriggerCodeSuccess(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "generate_trigger_code", map[string]any{
		"trigger_type":  "time_daily",
		"function_name": "myFunction",
	})
	if isError {
		t.Fatalf("expected success, got error: %s", text)
	}
	// Should contain generated code
	if !strings.Contains(text, "myFunction") {
		t.Errorf("expected output to contain 'myFunction', got %q", text)
	}
	if !strings.Contains(text, "TRIGGER") {
		t.Errorf("expected output to contain 'TRIGGER' header, got %q", text)
	}
}

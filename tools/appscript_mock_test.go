package tools

import (
	"strings"
	"testing"
)

// --- list_script_projects ---

func TestAppScriptMockListScriptProjects(t *testing.T) {
	t.Run("success_with_projects", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/drive/v3/files": `{
				"files": [
					{"id":"script001","name":"My Script","createdTime":"2026-01-10T10:00:00Z","modifiedTime":"2026-02-15T08:00:00Z"},
					{"id":"script002","name":"Auto Mailer","createdTime":"2026-01-05T12:00:00Z","modifiedTime":"2026-02-10T14:00:00Z"}
				]
			}`,
		})
		handler := handleListScriptProjects(testClientFunc(ts))
		text := callHandlerOK(t, handler, "list_script_projects", listScriptProjectsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Found 2 Apps Script projects") {
			t.Errorf("expected 'Found 2 Apps Script projects', got:\n%s", text)
		}
		if !strings.Contains(text, "My Script") {
			t.Errorf("expected 'My Script' in output")
		}
		if !strings.Contains(text, "Auto Mailer") {
			t.Errorf("expected 'Auto Mailer' in output")
		}
		if !strings.Contains(text, "script001") {
			t.Errorf("expected script ID in output")
		}
	})

	t.Run("success_no_projects", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/drive/v3/files": `{"files":[]}`,
		})
		handler := handleListScriptProjects(testClientFunc(ts))
		text := callHandlerOK(t, handler, "list_script_projects", listScriptProjectsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "No Apps Script projects found") {
			t.Errorf("expected 'No Apps Script projects found', got:\n%s", text)
		}
	})
}

// --- create_script_project ---

func TestAppScriptMockCreateScriptProject(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/projects": map[string]any{
				"scriptId": "newscript001",
				"title":    "New Automation",
			},
		})
		handler := handleCreateScriptProject(testClientFunc(ts))
		text := callHandlerOK(t, handler, "create_script_project", createScriptProjectIn{
			Title:           "New Automation",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Created Apps Script project") {
			t.Errorf("expected 'Created Apps Script project', got:\n%s", text)
		}
		if !strings.Contains(text, "New Automation") {
			t.Errorf("expected title in output")
		}
		if !strings.Contains(text, "newscript001") {
			t.Errorf("expected script ID in output")
		}
	})
}

// --- generate_trigger_code (pure code generation, no API) ---

func TestAppScriptMockGenerateTriggerCode(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		// This handler doesn't need a fake server — it generates code locally.
		text := callHandlerOK(t, generateTriggerCodeHandler, "generate_trigger_code", generateTriggerCodeIn{
			TriggerType:  "time_daily",
			FunctionName: "sendDailyReport",
		})
		if !strings.Contains(text, "sendDailyReport") {
			t.Errorf("expected function name in output, got:\n%s", text)
		}
		if !strings.Contains(text, "function") {
			t.Errorf("expected 'function' keyword in generated code, got:\n%s", text)
		}
	})
}

// --- API error responses ---

func TestAppScriptMockAPIError(t *testing.T) {
	t.Run("list_projects_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/drive/v3/files": {code: 403, body: `{"error": {"code": 403, "message": "Forbidden"}}`},
		})
		handler := handleListScriptProjects(testClientFunc(ts))
		text := callHandlerErr(t, handler, "list_script_projects", listScriptProjectsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "listing script projects") {
			t.Errorf("expected listing error, got:\n%s", text)
		}
	})

	t.Run("create_project_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v1/projects": {code: 500, body: `{"error": {"code": 500, "message": "Internal Server Error"}}`},
		})
		handler := handleCreateScriptProject(testClientFunc(ts))
		text := callHandlerErr(t, handler, "create_script_project", createScriptProjectIn{
			Title:           "Bad Script",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "creating script project") {
			t.Errorf("expected creation error, got:\n%s", text)
		}
	})
}

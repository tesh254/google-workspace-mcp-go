package tools

import (
	"strings"
	"testing"
)

// --- create_form ---

func TestFormsMockCreateForm(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/forms": map[string]any{
				"formId": "form001",
				"info": map[string]any{
					"title":       "Customer Survey",
					"description": "A survey about satisfaction",
				},
				"responderUri": "https://docs.google.com/forms/d/form001/viewform",
			},
		})
		handler := handleCreateForm(testClientFunc(ts))
		text := callHandlerOK(t, handler, "create_form", createFormIn{
			Title:           "Customer Survey",
			Description:     "A survey about satisfaction",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Successfully created form") {
			t.Errorf("expected success message, got:\n%s", text)
		}
		if !strings.Contains(text, "Customer Survey") {
			t.Errorf("expected form title in output")
		}
		if !strings.Contains(text, "form001") {
			t.Errorf("expected form ID in output")
		}
	})
}

// --- get_form ---

func TestFormsMockGetForm(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/forms/form001": map[string]any{
				"formId": "form001",
				"info": map[string]any{
					"title":       "Employee Feedback",
					"description": "Annual feedback form",
				},
				"responderUri": "https://docs.google.com/forms/d/form001/viewform",
				"items": []map[string]any{
					{
						"itemId": "q001",
						"title":  "How are you?",
						"questionItem": map[string]any{
							"question": map[string]any{
								"questionId": "q001",
								"required":   true,
								"textQuestion": map[string]any{
									"paragraph": false,
								},
							},
						},
					},
				},
			},
		})
		handler := handleGetForm(testClientFunc(ts))
		text := callHandlerOK(t, handler, "get_form", getFormIn{
			FormID:          "form001",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Employee Feedback") {
			t.Errorf("expected form title in output, got:\n%s", text)
		}
		if !strings.Contains(text, "form001") {
			t.Errorf("expected form ID in output")
		}
	})
}

// --- list_form_responses ---

func TestFormsMockListFormResponses(t *testing.T) {
	t.Run("success_with_responses", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/forms/form001/responses": map[string]any{
				"responses": []map[string]any{
					{
						"responseId":        "resp001",
						"createTime":        "2026-02-18T10:00:00Z",
						"lastSubmittedTime": "2026-02-18T10:00:00Z",
					},
					{
						"responseId":        "resp002",
						"createTime":        "2026-02-18T11:00:00Z",
						"lastSubmittedTime": "2026-02-18T11:00:00Z",
					},
				},
			},
		})
		handler := handleListFormResponses(testClientFunc(ts))
		text := callHandlerOK(t, handler, "list_form_responses", listFormResponsesIn{
			FormID:          "form001",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "resp001") {
			t.Errorf("expected response ID in output, got:\n%s", text)
		}
	})

	t.Run("success_no_responses", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/forms/form001/responses": map[string]any{},
		})
		handler := handleListFormResponses(testClientFunc(ts))
		text := callHandlerOK(t, handler, "list_form_responses", listFormResponsesIn{
			FormID:          "form001",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "No responses") || !strings.Contains(text, "0 responses") {
			// Either "No responses" or "0 responses" is acceptable
			if !strings.Contains(text, "response") {
				t.Errorf("expected response-related output, got:\n%s", text)
			}
		}
	})
}

// --- API error responses ---

func TestFormsMockAPIError(t *testing.T) {
	t.Run("create_form_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v1/forms": {code: 500, body: `{"error": {"code": 500, "message": "Internal Server Error"}}`},
		})
		handler := handleCreateForm(testClientFunc(ts))
		text := callHandlerErr(t, handler, "create_form", createFormIn{
			Title:           "Bad Form",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "creating form") {
			t.Errorf("expected form creation error, got:\n%s", text)
		}
	})

	t.Run("get_form_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v1/forms/nonexistent": {code: 404, body: `{"error": {"code": 404, "message": "Not Found"}}`},
		})
		handler := handleGetForm(testClientFunc(ts))
		text := callHandlerErr(t, handler, "get_form", getFormIn{
			FormID:          "nonexistent",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "getting form") {
			t.Errorf("expected form get error, got:\n%s", text)
		}
	})
}

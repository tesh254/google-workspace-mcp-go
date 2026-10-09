package tools

import (
	"strings"
	"testing"
)

// --- create_presentation ---

func TestSlidesMockCreatePresentation(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/presentations": map[string]any{
				"presentationId": "pres001",
				"title":          "Q1 Review",
				"slides": []map[string]any{
					{"objectId": "slide001"},
				},
			},
		})
		handler := handleCreatePresentation(testClientFunc(ts))
		text := callHandlerOK(t, handler, "create_presentation", createPresentationIn{
			Title:           "Q1 Review",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Presentation Created Successfully") {
			t.Errorf("expected success message, got:\n%s", text)
		}
		if !strings.Contains(text, "Q1 Review") {
			t.Errorf("expected title in output")
		}
		if !strings.Contains(text, "pres001") {
			t.Errorf("expected presentation ID in output")
		}
		if !strings.Contains(text, "1 slide(s) created") {
			t.Errorf("expected slide count in output, got:\n%s", text)
		}
	})
}

// --- get_presentation ---

func TestSlidesMockGetPresentation(t *testing.T) {
	t.Run("success_with_slides", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/presentations/pres001": map[string]any{
				"presentationId": "pres001",
				"title":          "Company Update",
				"pageSize": map[string]any{
					"width":  map[string]any{"magnitude": 9144000, "unit": "EMU"},
					"height": map[string]any{"magnitude": 5143500, "unit": "EMU"},
				},
				"slides": []map[string]any{
					{
						"objectId": "slide001",
						"pageElements": []map[string]any{
							{
								"objectId": "elem001",
								"shape": map[string]any{
									"shapeType": "TEXT_BOX",
									"text": map[string]any{
										"textElements": []map[string]any{
											{
												"startIndex": 0,
												"endIndex":   13,
												"textRun": map[string]any{
													"content": "Welcome Slide",
												},
											},
										},
									},
								},
							},
						},
					},
					{
						"objectId":     "slide002",
						"pageElements": []map[string]any{},
					},
				},
			},
		})
		handler := handleGetPresentation(testClientFunc(ts))
		text := callHandlerOK(t, handler, "get_presentation", getPresentationIn{
			PresentationID:  "pres001",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Company Update") {
			t.Errorf("expected title in output, got:\n%s", text)
		}
		if !strings.Contains(text, "pres001") {
			t.Errorf("expected presentation ID in output")
		}
		if !strings.Contains(text, "Slide 1") {
			t.Errorf("expected 'Slide 1' in output")
		}
		if !strings.Contains(text, "Slide 2") {
			t.Errorf("expected 'Slide 2' in output")
		}
	})
}

// --- API error responses ---

func TestSlidesMockAPIError(t *testing.T) {
	t.Run("create_presentation_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v1/presentations": {code: 500, body: `{"error": {"code": 500, "message": "Internal Server Error"}}`},
		})
		handler := handleCreatePresentation(testClientFunc(ts))
		text := callHandlerErr(t, handler, "create_presentation", createPresentationIn{
			Title:           "Bad Pres",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "creating presentation") {
			t.Errorf("expected presentation creation error, got:\n%s", text)
		}
	})

	t.Run("get_presentation_not_found", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v1/presentations/nonexistent": {code: 404, body: `{"error": {"code": 404, "message": "Not Found"}}`},
		})
		handler := handleGetPresentation(testClientFunc(ts))
		text := callHandlerErr(t, handler, "get_presentation", getPresentationIn{
			PresentationID:  "nonexistent",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "getting presentation") {
			t.Errorf("expected get presentation error, got:\n%s", text)
		}
	})
}

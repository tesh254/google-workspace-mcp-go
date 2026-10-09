package tools

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// --- list_spaces ---

func TestChatMockListSpaces(t *testing.T) {
	t.Run("success_with_spaces", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/spaces": map[string]any{
				"spaces": []map[string]any{
					{"name": "spaces/AAAA", "displayName": "General", "spaceType": "SPACE"},
					{"name": "spaces/BBBB", "displayName": "Engineering", "spaceType": "SPACE"},
				},
			},
		})
		handler := handleListSpaces(testClientFunc(ts))
		text := callHandlerOK(t, handler, "list_spaces", listSpacesIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Found 2 Chat spaces") {
			t.Errorf("expected 'Found 2 Chat spaces', got:\n%s", text)
		}
		if !strings.Contains(text, "General") {
			t.Errorf("expected 'General' in output")
		}
		if !strings.Contains(text, "Engineering") {
			t.Errorf("expected 'Engineering' in output")
		}
		if !strings.Contains(text, "spaces/AAAA") {
			t.Errorf("expected space ID in output")
		}
	})

	t.Run("success_no_spaces", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/spaces": map[string]any{
				"spaces": []map[string]any{},
			},
		})
		handler := handleListSpaces(testClientFunc(ts))
		text := callHandlerOK(t, handler, "list_spaces", listSpacesIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "No Chat spaces found") {
			t.Errorf("expected 'No Chat spaces found', got:\n%s", text)
		}
	})
}

// --- send_message ---

func TestChatMockSendMessage(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := driveFakeServer(t, map[string]any{
			"/v1/spaces/AAAA/messages": func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"name":"spaces/AAAA/messages/msg001","text":"Hello team!","createTime":"2026-02-18T10:00:00Z"}`)
			},
			"/v1/spaces/AAAA": func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"name":"spaces/AAAA","displayName":"General","spaceType":"SPACE"}`)
			},
		})
		handler := handleSendMessage(testClientFunc(ts))
		text := callHandlerOK(t, handler, "send_message", sendMessageIn{
			SpaceID:         "spaces/AAAA",
			MessageText:     "Hello team!",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Message sent") {
			t.Errorf("expected 'Message sent', got:\n%s", text)
		}
		if !strings.Contains(text, "spaces/AAAA/messages/msg001") {
			t.Errorf("expected message ID in output, got:\n%s", text)
		}
	})
}

// --- API error responses ---

func TestChatMockAPIError(t *testing.T) {
	t.Run("list_spaces_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v1/spaces": {code: 403, body: `{"error": {"code": 403, "message": "Forbidden"}}`},
		})
		handler := handleListSpaces(testClientFunc(ts))
		text := callHandlerErr(t, handler, "list_spaces", listSpacesIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "listing spaces") {
			t.Errorf("expected listing spaces error, got:\n%s", text)
		}
	})
}

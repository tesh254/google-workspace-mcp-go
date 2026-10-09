package tools

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Direct handler invocations replace the previous contactsTestServer: each
// test wires handleX to a fake People API served via testClientFunc/fakeAPIServer.

// --- list_contacts ---

func TestContactsMockListContacts(t *testing.T) {
	t.Run("success_with_contacts", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/people/me/connections": map[string]any{
				"connections": []map[string]any{
					{
						"resourceName": "people/c123",
						"names":        []map[string]any{{"displayName": "Alice Smith"}},
						"emailAddresses": []map[string]any{
							{"value": "alice@example.com"},
						},
						"phoneNumbers": []map[string]any{
							{"value": "+1-555-0100"},
						},
					},
					{
						"resourceName": "people/c456",
						"names":        []map[string]any{{"displayName": "Bob Jones"}},
						"emailAddresses": []map[string]any{
							{"value": "bob@example.com"},
						},
					},
				},
				"totalPeople": 2,
			},
		})
		text := callHandlerOK(t, handleListContacts(testClientFunc(ts)), "list_contacts", listContactsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Contacts for test@example.com") {
			t.Errorf("expected contacts header, got:\n%s", text)
		}
		if !strings.Contains(text, "Alice Smith") {
			t.Errorf("expected 'Alice Smith' in output")
		}
		if !strings.Contains(text, "Bob Jones") {
			t.Errorf("expected 'Bob Jones' in output")
		}
		if !strings.Contains(text, "alice@example.com") {
			t.Errorf("expected email in output")
		}
	})

	t.Run("success_no_contacts", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/people/me/connections": map[string]any{
				"connections": []map[string]any{},
				"totalPeople": 0,
			},
		})
		text := callHandlerOK(t, handleListContacts(testClientFunc(ts)), "list_contacts", listContactsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "No contacts found") {
			t.Errorf("expected 'No contacts found', got:\n%s", text)
		}
	})
}

// --- create_contact ---

func TestContactsMockCreateContact(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := driveFakeServer(t, map[string]any{
			"/v1/people:createContact": func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{
					"resourceName": "people/c789",
					"names": [{"displayName": "Carol White"}],
					"emailAddresses": [{"value": "carol@example.com"}]
				}`)
			},
		})
		text := callHandlerOK(t, handleCreateContact(testClientFunc(ts)), "create_contact", createContactIn{
			GivenName:       "Carol",
			FamilyName:      "White",
			Email:           "carol@example.com",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Contact Created") {
			t.Errorf("expected 'Contact Created', got:\n%s", text)
		}
		if !strings.Contains(text, "Carol") {
			t.Errorf("expected name in output")
		}
	})
}

// --- list_contact_groups ---

func TestContactsMockListContactGroups(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v1/contactGroups": map[string]any{
				"contactGroups": []map[string]any{
					{"resourceName": "contactGroups/myContacts", "name": "My Contacts", "groupType": "SYSTEM_CONTACT_GROUP", "memberCount": 25},
					{"resourceName": "contactGroups/friends", "name": "Friends", "groupType": "USER_CONTACT_GROUP", "memberCount": 10},
				},
			},
		})
		text := callHandlerOK(t, handleListContactGroups(testClientFunc(ts)), "list_contact_groups", listContactGroupsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "My Contacts") {
			t.Errorf("expected 'My Contacts' in output, got:\n%s", text)
		}
		if !strings.Contains(text, "Friends") {
			t.Errorf("expected 'Friends' in output")
		}
	})
}

// --- API error responses ---

func TestContactsMockAPIError(t *testing.T) {
	t.Run("list_contacts_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v1/people/me/connections": {code: 403, body: `{"error": {"code": 403, "message": "Forbidden"}}`},
		})
		text := callHandlerErr(t, handleListContacts(testClientFunc(ts)), "list_contacts", listContactsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "listing contacts") {
			t.Errorf("expected listing contacts error, got:\n%s", text)
		}
	})
}

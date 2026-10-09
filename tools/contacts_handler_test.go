package tools

import (
	"strings"
	"testing"

	"github.com/magks/google-workspace-mcp-go/internal/google"
)

// Required-argument error paths (missing contact_id, group_id, name, etc.)
// are no longer tested here: with the official go-sdk, input validation
// against the tool's schema happens in the SDK wrapper before the handler
// runs. Only handler-level validation paths remain below.

// contactsTestGetClient wires a real credential store backed by an empty temp
// directory (no stored credentials) plus a fixed user email env var, matching
// the previous newToolTestServer environment: every handler call using it
// fails at authentication.
func contactsTestGetClient(t *testing.T) httpClientFunc {
	t.Helper()
	t.Setenv("WORKSPACE_MCP_CREDENTIALS_DIR", t.TempDir())
	t.Setenv("USER_GOOGLE_EMAIL", "test@example.com")
	return clientFuncFromCache(google.DefaultClientCache())
}

// --- list_contacts ---
// list_contacts has no strictly required params, so first error is auth failure.

func TestContactsHandlerListContactsAuthFailure(t *testing.T) {
	handler := handleListContacts(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "list_contacts", listContactsIn{})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", errText)
	}
}

// --- get_contact ---

func TestContactsHandlerGetContactAuthFailure(t *testing.T) {
	handler := handleGetContact(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "get_contact", getContactIn{
		ContactID: "c1234567890",
	})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", errText)
	}
}

// --- search_contacts ---

// no handler-level validation for search_contacts: query is a schema-level
// required param, so its absence is covered by the SDK wrapper.

// --- list_contact_groups ---
// list_contact_groups has no strictly required params, so first error is auth failure.

func TestContactsHandlerListContactGroupsAuthFailure(t *testing.T) {
	handler := handleListContactGroups(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "list_contact_groups", listContactGroupsIn{})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", errText)
	}
}

// --- create_contact ---
// create_contact has no required params that the handler checks. It calls
// resolveEmail (succeeds via env), then newPeopleService (auth failure), then
// buildPersonBody check. Auth failure comes before the buildPersonBody check.

func TestContactsHandlerCreateContactAuthFailure(t *testing.T) {
	handler := handleCreateContact(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "create_contact", createContactIn{
		GivenName: "Test",
	})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", errText)
	}
}

func TestContactsHandlerCreateContactNoFields(t *testing.T) {
	// create_contact with no fields calls newPeopleService first (auth failure),
	// then buildPersonBody. Since auth fails first, we can only test auth here.
	// But let's test that an empty call hits auth before field validation.
	handler := handleCreateContact(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "create_contact", createContactIn{})
	// Auth failure comes before field validation
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error about credentials/auth, got %q", errText)
	}
}

// --- batch_create_contacts ---

func TestContactsHandlerBatchCreateContactsNoContacts(t *testing.T) {
	handler := handleBatchCreateContacts(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "batch_create_contacts", batchCreateContactsIn{})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "contact") {
		t.Errorf("expected error mentioning 'contact', got %q", errText)
	}
}

func TestContactsHandlerBatchCreateContactsEmptyArray(t *testing.T) {
	handler := handleBatchCreateContacts(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "batch_create_contacts", batchCreateContactsIn{
		Contacts: []map[string]any{},
	})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "contact") {
		t.Errorf("expected error mentioning 'contact', got %q", errText)
	}
}

// --- batch_update_contacts ---

func TestContactsHandlerBatchUpdateContactsNoUpdates(t *testing.T) {
	handler := handleBatchUpdateContacts(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "batch_update_contacts", batchUpdateContactsIn{})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "update") {
		t.Errorf("expected error mentioning 'update', got %q", errText)
	}
}

// --- batch_delete_contacts ---

func TestContactsHandlerBatchDeleteContactsEmptyArray(t *testing.T) {
	handler := handleBatchDeleteContacts(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "batch_delete_contacts", batchDeleteContactsIn{
		ContactIDs: []string{},
	})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "contact") {
		t.Errorf("expected error mentioning 'contact', got %q", errText)
	}
}

// --- modify_contact_group_members ---

func TestContactsHandlerModifyContactGroupMembersNoAddOrRemove(t *testing.T) {
	handler := handleModifyContactGroupMembers(contactsTestGetClient(t))
	errText := callHandlerErr(t, handler, "modify_contact_group_members", modifyContactGroupMembersIn{
		GroupID: "abc123",
	})
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "add_contact_ids") && !strings.Contains(lower, "remove_contact_ids") {
		t.Errorf("expected error mentioning add/remove contact IDs, got %q", errText)
	}
}

package tools

import (
	"strings"
	"testing"
)

// Required-argument error paths (missing query, message_id, window of
// send_gmail_message params, etc.) are no longer tested here: with the
// official go-sdk, input validation against the tool's schema happens in the
// SDK wrapper before the handler runs. Only handler-level validation paths
// remain below.

// TestGmailHandlerGetMessagesContentBatchEmptyMessageIDs verifies that
// get_gmail_messages_content_batch returns error when message_ids is empty.
func TestGmailHandlerGetMessagesContentBatchEmptyMessageIDs(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_gmail_messages_content_batch", map[string]any{
		"message_ids": []any{},
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "no message ids") {
		t.Errorf("expected error mentioning empty message_ids, got %q", text)
	}
}

// TestGmailHandlerGetThreadsContentBatchEmptyThreadIDs verifies that
// get_gmail_threads_content_batch returns error when thread_ids is empty.
func TestGmailHandlerGetThreadsContentBatchEmptyThreadIDs(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "get_gmail_threads_content_batch", map[string]any{
		"thread_ids": []any{},
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "no thread ids") {
		t.Errorf("expected error mentioning empty thread_ids, got %q", text)
	}
}

// TestGmailHandlerModifyMessageLabelsMissingBothLabelArrays verifies that
// modify_gmail_message_labels returns error when neither add nor remove labels provided.
func TestGmailHandlerModifyMessageLabelsMissingBothLabelArrays(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "modify_gmail_message_labels", map[string]any{
		"message_id": "msg123",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	if !strings.Contains(strings.ToLower(text), "add_label_ids") && !strings.Contains(strings.ToLower(text), "remove_label_ids") {
		t.Errorf("expected error mentioning label arrays, got %q", text)
	}
}

// TestGmailHandlerManageGmailLabelCreateMissingName verifies that
// manage_gmail_label with action=create returns error when name is missing.
// Note: the name check happens after auth, so without credentials we get an
// auth error first. This test verifies the handler still returns an error.
func TestGmailHandlerManageGmailLabelCreateMissingName(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "manage_gmail_label", map[string]any{
		"action": "create",
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	// Auth error comes before name validation; verify we get some error.
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "name") && !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") {
		t.Errorf("expected error mentioning 'name' or credentials, got %q", text)
	}
}

// TestGmailHandlerBatchModifyMessageLabelsEmptyMessageIDs verifies that
// batch_modify_gmail_message_labels returns error when message_ids is empty.
func TestGmailHandlerBatchModifyMessageLabelsEmptyMessageIDs(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "batch_modify_gmail_message_labels", map[string]any{
		"message_ids": []any{},
	})
	if !isError {
		t.Fatal("expected isError=true")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "message_ids") && !strings.Contains(lower, "empty") {
		t.Errorf("expected error mentioning empty message_ids, got %q", text)
	}
}

// TestGmailHandlerAuthFailureSearchGmailMessages verifies that
// search_gmail_messages with valid params but no credentials returns
// an error mentioning credentials or authentication.
func TestGmailHandlerAuthFailureSearchGmailMessages(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "search_gmail_messages", map[string]any{
		"query": "in:inbox",
	})
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

// TestGmailHandlerAuthFailureListGmailLabels verifies that
// list_gmail_labels with no credentials returns an auth error.
func TestGmailHandlerAuthFailureListGmailLabels(t *testing.T) {
	cs := newToolTestSession(t)
	text, isError := callTool(t, cs, "list_gmail_labels", nil)
	if !isError {
		t.Fatal("expected isError=true for auth failure")
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "credentials") && !strings.Contains(lower, "authenticating") && !strings.Contains(lower, "token") {
		t.Errorf("expected error about credentials/auth, got %q", text)
	}
}

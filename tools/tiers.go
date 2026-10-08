package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/server"
)

// tierTools maps each service to its tier breakdown (core, extended, complete).
// This matches the Python server's tool_tiers.yaml exactly.
var tierTools = map[string]map[string][]string{
	"gmail": {
		"core": {
			"search_gmail_messages",
			"get_gmail_message_content",
			"get_gmail_messages_content_batch",
			"send_gmail_message",
		},
		"extended": {
			"get_gmail_attachment_content",
			"get_gmail_thread_content",
			"modify_gmail_message_labels",
			"list_gmail_labels",
			"manage_gmail_label",
			"draft_gmail_message",
			"list_gmail_filters",
			"create_gmail_filter",
			"delete_gmail_filter",
		},
		"complete": {
			"get_gmail_threads_content_batch",
			"batch_modify_gmail_message_labels",
			"start_google_auth",
		},
	},
	"drive": {
		"core": {
			"search_drive_files",
			"get_drive_file_content",
			"get_drive_file_download_url",
			"create_drive_file",
			"import_to_google_doc",
			"share_drive_file",
			"get_drive_shareable_link",
		},
		"extended": {
			"list_drive_items",
			"copy_drive_file",
			"update_drive_file",
			"update_drive_permission",
			"remove_drive_permission",
			"transfer_drive_ownership",
			"batch_share_drive_file",
		},
		"complete": {
			"get_drive_file_permissions",
			"check_drive_file_public_access",
		},
	},
	"calendar": {
		"core": {
			"list_calendars",
			"get_events",
			"create_event",
			"modify_event",
		},
		"extended": {
			"delete_event",
			"query_freebusy",
		},
		"complete": {},
	},
	"docs": {
		"core": {
			"get_doc_content",
			"create_doc",
			"modify_doc_text",
		},
		"extended": {
			"export_doc_to_pdf",
			"search_docs",
			"find_and_replace_doc",
			"list_docs_in_folder",
			"insert_doc_elements",
			"update_paragraph_style",
		},
		"complete": {
			"insert_doc_image",
			"update_doc_headers_footers",
			"batch_update_doc",
			"inspect_doc_structure",
			"create_table_with_data",
			"debug_table_structure",
			"read_document_comments",
			"create_document_comment",
			"reply_to_document_comment",
			"resolve_document_comment",
		},
	},
	"sheets": {
		"core": {
			"create_spreadsheet",
			"read_sheet_values",
			"modify_sheet_values",
		},
		"extended": {
			"list_spreadsheets",
			"get_spreadsheet_info",
		},
		"complete": {
			"create_sheet",
			"format_sheet_range",
			"add_conditional_formatting",
			"update_conditional_formatting",
			"delete_conditional_formatting",
			"read_spreadsheet_comments",
			"create_spreadsheet_comment",
			"reply_to_spreadsheet_comment",
			"resolve_spreadsheet_comment",
		},
	},
	"chat": {
		"core": {
			"send_message",
			"get_messages",
			"search_messages",
		},
		"extended": {
			"list_spaces",
		},
		"complete": {},
	},
	"forms": {
		"core": {
			"create_form",
			"get_form",
		},
		"extended": {
			"list_form_responses",
		},
		"complete": {
			"set_publish_settings",
			"get_form_response",
			"batch_update_form",
		},
	},
	"slides": {
		"core": {
			"create_presentation",
			"get_presentation",
		},
		"extended": {
			"batch_update_presentation",
			"get_page",
			"get_page_thumbnail",
		},
		"complete": {
			"read_presentation_comments",
			"create_presentation_comment",
			"reply_to_presentation_comment",
			"resolve_presentation_comment",
		},
	},
	"tasks": {
		"core": {
			"get_task",
			"list_tasks",
			"create_task",
			"update_task",
		},
		"extended": {
			"delete_task",
		},
		"complete": {
			"list_task_lists",
			"get_task_list",
			"create_task_list",
			"update_task_list",
			"delete_task_list",
			"move_task",
			"clear_completed_tasks",
		},
	},
	"contacts": {
		"core": {
			"search_contacts",
			"get_contact",
			"list_contacts",
			"create_contact",
		},
		"extended": {
			"update_contact",
			"delete_contact",
			"list_contact_groups",
			"get_contact_group",
		},
		"complete": {
			"batch_create_contacts",
			"batch_update_contacts",
			"batch_delete_contacts",
			"create_contact_group",
			"update_contact_group",
			"delete_contact_group",
			"modify_contact_group_members",
		},
	},
	"search": {
		"core": {
			"search_custom",
		},
		"extended": {
			"search_custom_siterestrict",
		},
		"complete": {
			"get_search_engine_info",
		},
	},
	"appscript": {
		"core": {
			"list_script_projects",
			"get_script_project",
			"get_script_content",
			"create_script_project",
			"update_script_content",
			"run_script_function",
			"generate_trigger_code",
		},
		"extended": {
			"create_deployment",
			"list_deployments",
			"update_deployment",
			"delete_deployment",
			"delete_script_project",
			"list_versions",
			"create_version",
			"get_version",
			"list_script_processes",
			"get_script_metrics",
		},
		"complete": {},
	},
}

// readOnlyTools is the set of tools that are allowed in --read-only mode.
// All other tools require write scopes and are removed when --read-only is active.
var readOnlyTools = map[string]bool{
	// Gmail
	"search_gmail_messages":            true,
	"get_gmail_message_content":        true,
	"get_gmail_messages_content_batch": true,
	"get_gmail_attachment_content":     true,
	"get_gmail_thread_content":         true,
	"get_gmail_threads_content_batch":  true,
	"list_gmail_labels":                true,
	"list_gmail_filters":               true,
	// Drive
	"search_drive_files":             true,
	"get_drive_file_content":         true,
	"get_drive_file_download_url":    true,
	"list_drive_items":               true,
	"get_drive_file_permissions":     true,
	"check_drive_file_public_access": true,
	"get_drive_shareable_link":       true,
	// Calendar
	"list_calendars": true,
	"get_events":     true,
	"query_freebusy": true,
	// Docs
	"search_docs":           true,
	"get_doc_content":       true,
	"list_docs_in_folder":   true,
	"inspect_doc_structure": true,
	"debug_table_structure": true,
	"export_doc_to_pdf":     true,
	// Sheets
	"list_spreadsheets":    true,
	"get_spreadsheet_info": true,
	"read_sheet_values":    true,
	// Chat
	"list_spaces":     true,
	"get_messages":    true,
	"search_messages": true,
	// Forms
	"get_form":            true,
	"get_form_response":   true,
	"list_form_responses": true,
	// Slides
	"get_presentation":   true,
	"get_page":           true,
	"get_page_thumbnail": true,
	// Tasks
	"get_task":        true,
	"list_tasks":      true,
	"list_task_lists": true,
	"get_task_list":   true,
	// Contacts
	"search_contacts":     true,
	"get_contact":         true,
	"list_contacts":       true,
	"list_contact_groups": true,
	"get_contact_group":   true,
	// Search (all read-only)
	"search_custom":              true,
	"get_search_engine_info":     true,
	"search_custom_siterestrict": true,
	// Apps Script
	"list_script_projects":  true,
	"get_script_project":    true,
	"get_script_content":    true,
	"list_deployments":      true,
	"list_script_processes": true,
	"list_versions":         true,
	"get_version":           true,
	"get_script_metrics":    true,
	// Comments (read only)
	"read_document_comments":     true,
	"read_spreadsheet_comments":  true,
	"read_presentation_comments": true,
}

// allowedToolsForTier returns the set of tool names allowed for the given tier.
// "core" = core only, "extended" = core + extended, "complete" (or empty) = all.
func allowedToolsForTier(tier string) map[string]bool {
	if tier == "" || tier == "complete" {
		return nil // nil means allow all
	}

	allowed := make(map[string]bool)
	for _, service := range tierTools {
		for _, name := range service["core"] {
			allowed[name] = true
		}
		if tier == "extended" {
			for _, name := range service["extended"] {
				allowed[name] = true
			}
		}
	}
	return allowed
}

// FilterTools removes tools from the server based on tier and read-only settings.
// It is called after RegisterAllTools to apply post-registration filtering.
func FilterTools(s *mcp.Server, cfg server.Config) {
	tierAllowed := allowedToolsForTier(cfg.ToolTier)

	var toRemove []string

	// Collect all known tool names from tier definitions.
	allKnown := make(map[string]bool)
	for _, service := range tierTools {
		for _, names := range service {
			for _, name := range names {
				allKnown[name] = true
			}
		}
	}

	for name := range allKnown {
		remove := false

		// Tier filtering: remove tools not in the allowed tier set.
		if tierAllowed != nil && !tierAllowed[name] {
			remove = true
		}

		// Read-only filtering: remove tools that require write scopes.
		if cfg.ReadOnly && !readOnlyTools[name] {
			remove = true
		}

		if remove {
			toRemove = append(toRemove, name)
		}
	}

	if len(toRemove) > 0 {
		s.RemoveTools(toRemove...)
	}
}

package tools

import (
	"context"
	"testing"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/server"
)

// registeredToolNames returns the names of all tools registered on the server.
func registeredToolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	resp, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := make(map[string]bool, len(resp.Tools))
	for _, tool := range resp.Tools {
		names[tool.Name] = true
	}
	return names
}

// newTestSession creates an MCP server, registers all tools, applies
// filtering, and connects an in-memory client for tools/list checks.
func newTestSession(t *testing.T, cfg server.Config) *mcp.ClientSession {
	t.Helper()
	s := server.New(cfg)
	RegisterAllTools(s, cfg)
	FilterTools(s, cfg)

	ctx := context.Background()
	sTransport, cTransport := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, sTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "tiers-test", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(ctx, cTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestNoFilterLoadsAllTools(t *testing.T) {
	cs := newTestSession(t, server.Config{})
	names := registeredToolNames(t, cs)
	// 12 comment tools + 15 Gmail + 16 Drive + 6 Calendar + 15 Docs + 10 Sheets + 4 Chat + 6 Forms + 5 Slides + 12 Tasks + 15 Contacts + 3 Search + 17 AppScript + 1 start_google_auth = 137.
	if len(names) != 137 {
		t.Errorf("expected 137 tools with no filter, got %d: %v", len(names), names)
	}
}

func TestTierCoreFiltering(t *testing.T) {
	cs := newTestSession(t, server.Config{ToolTier: "core"})
	names := registeredToolNames(t, cs)
	// Gmail core (4) + Drive core (7) + Calendar core (4) + Docs core (3) + Sheets core (3) + Chat core (3) + Forms core (2) + Slides core (2) + Tasks core (4) + Contacts core (4) + Search core (1) + AppScript core (7) = 44.
	if len(names) != 44 {
		t.Errorf("expected 44 tools with core tier, got %d: %v", len(names), names)
	}
}

func TestTierExtendedFiltering(t *testing.T) {
	cs := newTestSession(t, server.Config{ToolTier: "extended"})
	names := registeredToolNames(t, cs)
	// Gmail core+extended (13) + Drive core+extended (14) + Calendar core+extended (6) + Docs core+extended (9) + Sheets core+extended (5) + Chat core+extended (4) + Forms core+extended (3) + Slides core+extended (5) + Tasks core+extended (5) + Contacts core+extended (8) + Search core+extended (2) + AppScript core+extended (17) = 91.
	if len(names) != 91 {
		t.Errorf("expected 91 tools with extended tier, got %d: %v", len(names), names)
	}
}

func TestTierCompleteFiltering(t *testing.T) {
	cs := newTestSession(t, server.Config{ToolTier: "complete"})
	names := registeredToolNames(t, cs)
	// Complete tier = all tools, same as no filter.
	if len(names) != 137 {
		t.Errorf("expected 137 tools with complete tier, got %d: %v", len(names), names)
	}
}

func TestReadOnlyFiltering(t *testing.T) {
	cs := newTestSession(t, server.Config{ReadOnly: true})
	names := registeredToolNames(t, cs)
	// 3 read comment + 8 Gmail read + 7 Drive read + 3 Calendar read + 6 Docs read + 3 Sheets read + 3 Chat read + 3 Forms read + 3 Slides read + 4 Tasks read + 5 Contacts read + 3 Search read + 8 AppScript read = 59.
	if len(names) != 59 {
		t.Errorf("expected 59 tools in read-only mode, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"read_document_comments",
		"read_spreadsheet_comments",
		"read_presentation_comments",
		"search_gmail_messages",
		"get_gmail_message_content",
		"get_gmail_messages_content_batch",
		"get_gmail_attachment_content",
		"get_gmail_thread_content",
		"get_gmail_threads_content_batch",
		"list_gmail_labels",
		"list_gmail_filters",
		"search_drive_files",
		"get_drive_file_content",
		"get_drive_file_download_url",
		"list_drive_items",
		"get_drive_file_permissions",
		"check_drive_file_public_access",
		"get_drive_shareable_link",
		"list_calendars",
		"get_events",
		"query_freebusy",
		"search_docs",
		"get_doc_content",
		"list_docs_in_folder",
		"inspect_doc_structure",
		"debug_table_structure",
		"export_doc_to_pdf",
		"list_spreadsheets",
		"get_spreadsheet_info",
		"read_sheet_values",
		"list_spaces",
		"get_messages",
		"search_messages",
		"get_form",
		"get_form_response",
		"list_form_responses",
		"get_presentation",
		"get_page",
		"get_page_thumbnail",
		"get_task",
		"list_tasks",
		"list_task_lists",
		"get_task_list",
		"search_contacts",
		"get_contact",
		"list_contacts",
		"list_contact_groups",
		"get_contact_group",
		"search_custom",
		"get_search_engine_info",
		"search_custom_siterestrict",
		"list_script_projects",
		"get_script_project",
		"get_script_content",
		"list_deployments",
		"list_script_processes",
		"list_versions",
		"get_version",
		"get_script_metrics",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present in read-only mode", expected)
		}
	}
}

func TestReadOnlyPlusTierComposition(t *testing.T) {
	// Read-only + complete tier: all read-only tools survive.
	cs := newTestSession(t, server.Config{ReadOnly: true, ToolTier: "complete"})
	names := registeredToolNames(t, cs)
	if len(names) != 59 {
		t.Errorf("expected 59 tools with read-only + complete tier, got %d: %v", len(names), names)
	}

	// Read-only + core tier: Gmail core read-only (3) + Drive core read-only (4)
	// + Calendar core read-only (2) + Docs core read-only (1: get_doc_content) + Sheets core read-only (1: read_sheet_values)
	// + Chat core read-only (2: get_messages, search_messages) + Forms core read-only (1: get_form)
	// + Slides core read-only (1: get_presentation) + Tasks core read-only (2: get_task, list_tasks)
	// + Contacts core read-only (3: search_contacts, get_contact, list_contacts) + Search core read-only (1: search_custom)
	// + AppScript core read-only (3: list_script_projects, get_script_project, get_script_content) = 24.
	cs2 := newTestSession(t, server.Config{ReadOnly: true, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 24 {
		t.Errorf("expected 24 tools with read-only + core tier, got %d: %v", len(names2), names2)
	}
}

func TestToolsFilterComposesWithServiceFilter(t *testing.T) {
	// --tools docs with no tier: 15 Docs tools + 4 comment tools = 19.
	cs := newTestSession(t, server.Config{Tools: []string{"docs"}})
	names := registeredToolNames(t, cs)
	if len(names) != 19 {
		t.Errorf("expected 19 tools with --tools docs, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"read_document_comments",
		"create_document_comment",
		"reply_to_document_comment",
		"resolve_document_comment",
		"search_docs",
		"get_doc_content",
		"list_docs_in_folder",
		"create_doc",
		"inspect_doc_structure",
		"debug_table_structure",
		"export_doc_to_pdf",
		"modify_doc_text",
		"find_and_replace_doc",
		"insert_doc_elements",
		"insert_doc_image",
		"update_doc_headers_footers",
		"batch_update_doc",
		"create_table_with_data",
		"update_paragraph_style",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}
}

func TestToolsFilterPlusTier(t *testing.T) {
	// --tools docs --tool-tier core: get_doc_content + create_doc + modify_doc_text = 3.
	cs := newTestSession(t, server.Config{Tools: []string{"docs"}, ToolTier: "core"})
	names := registeredToolNames(t, cs)
	if len(names) != 3 {
		t.Errorf("expected 3 tools with --tools docs --tool-tier core, got %d: %v", len(names), names)
	}
}

func TestToolsFilterPlusReadOnly(t *testing.T) {
	// --tools docs --read-only: 6 Docs read-only + read_document_comments = 7.
	cs := newTestSession(t, server.Config{Tools: []string{"docs"}, ReadOnly: true})
	names := registeredToolNames(t, cs)
	if len(names) != 7 {
		t.Errorf("expected 7 tools with --tools docs --read-only, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"read_document_comments",
		"search_docs",
		"get_doc_content",
		"list_docs_in_folder",
		"inspect_doc_structure",
		"debug_table_structure",
		"export_doc_to_pdf",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}
}

func TestToolsGmailFiltering(t *testing.T) {
	// --tools gmail: all 15 Gmail tools (7 read + 8 write) + 1 start_google_auth = 16.
	cs := newTestSession(t, server.Config{Tools: []string{"gmail"}})
	names := registeredToolNames(t, cs)
	if len(names) != 16 {
		t.Errorf("expected 16 tools with --tools gmail, got %d: %v", len(names), names)
	}

	// --tools gmail --tool-tier core: 4 core Gmail tools.
	cs2 := newTestSession(t, server.Config{Tools: []string{"gmail"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 4 {
		t.Errorf("expected 4 tools with --tools gmail --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools gmail --read-only: 7 Gmail read tools + list_gmail_filters = 8.
	cs3 := newTestSession(t, server.Config{Tools: []string{"gmail"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 8 {
		t.Errorf("expected 8 tools with --tools gmail --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsDriveFiltering(t *testing.T) {
	// --tools drive: 16 Drive tools (7 read + 9 write).
	cs := newTestSession(t, server.Config{Tools: []string{"drive"}})
	names := registeredToolNames(t, cs)
	if len(names) != 16 {
		t.Errorf("expected 16 tools with --tools drive, got %d: %v", len(names), names)
	}

	// --tools drive --tool-tier core: 7 Drive core tools.
	cs2 := newTestSession(t, server.Config{Tools: []string{"drive"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 7 {
		t.Errorf("expected 7 tools with --tools drive --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools drive --read-only: 7 Drive read-only tools.
	cs3 := newTestSession(t, server.Config{Tools: []string{"drive"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 7 {
		t.Errorf("expected 7 tools with --tools drive --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsCalendarFiltering(t *testing.T) {
	// --tools calendar: all 6 Calendar tools.
	cs := newTestSession(t, server.Config{Tools: []string{"calendar"}})
	names := registeredToolNames(t, cs)
	if len(names) != 6 {
		t.Errorf("expected 6 tools with --tools calendar, got %d: %v", len(names), names)
	}

	// --tools calendar --tool-tier core: 4 core Calendar tools.
	cs2 := newTestSession(t, server.Config{Tools: []string{"calendar"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 4 {
		t.Errorf("expected 4 tools with --tools calendar --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools calendar --read-only: 3 Calendar read-only tools.
	cs3 := newTestSession(t, server.Config{Tools: []string{"calendar"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 3 {
		t.Errorf("expected 3 tools with --tools calendar --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsDocsFiltering(t *testing.T) {
	// --tools docs: 15 Docs tools + 4 comment tools = 19.
	cs := newTestSession(t, server.Config{Tools: []string{"docs"}})
	names := registeredToolNames(t, cs)
	if len(names) != 19 {
		t.Errorf("expected 19 tools with --tools docs, got %d: %v", len(names), names)
	}

	// --tools docs --tool-tier core: get_doc_content + create_doc + modify_doc_text = 3.
	cs2 := newTestSession(t, server.Config{Tools: []string{"docs"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 3 {
		t.Errorf("expected 3 tools with --tools docs --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools docs --read-only: 6 Docs read-only + read_document_comments = 7.
	cs3 := newTestSession(t, server.Config{Tools: []string{"docs"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 7 {
		t.Errorf("expected 7 tools with --tools docs --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsSheetsFiltering(t *testing.T) {
	// --tools sheets: 10 Sheets tools + 4 comment tools = 14.
	cs := newTestSession(t, server.Config{Tools: []string{"sheets"}})
	names := registeredToolNames(t, cs)
	if len(names) != 14 {
		t.Errorf("expected 14 tools with --tools sheets, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"list_spreadsheets",
		"get_spreadsheet_info",
		"read_sheet_values",
		"modify_sheet_values",
		"format_sheet_range",
		"add_conditional_formatting",
		"update_conditional_formatting",
		"delete_conditional_formatting",
		"create_spreadsheet",
		"create_sheet",
		"read_spreadsheet_comments",
		"create_spreadsheet_comment",
		"reply_to_spreadsheet_comment",
		"resolve_spreadsheet_comment",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}

	// --tools sheets --tool-tier core: 3 core Sheets tools.
	cs2 := newTestSession(t, server.Config{Tools: []string{"sheets"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 3 {
		t.Errorf("expected 3 tools with --tools sheets --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools sheets --read-only: 3 Sheets read-only + read_spreadsheet_comments = 4.
	cs3 := newTestSession(t, server.Config{Tools: []string{"sheets"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 4 {
		t.Errorf("expected 4 tools with --tools sheets --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsChatFiltering(t *testing.T) {
	// --tools chat: all 4 Chat tools.
	cs := newTestSession(t, server.Config{Tools: []string{"chat"}})
	names := registeredToolNames(t, cs)
	if len(names) != 4 {
		t.Errorf("expected 4 tools with --tools chat, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"list_spaces",
		"get_messages",
		"send_message",
		"search_messages",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}

	// --tools chat --tool-tier core: 3 core Chat tools (send_message, get_messages, search_messages).
	cs2 := newTestSession(t, server.Config{Tools: []string{"chat"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 3 {
		t.Errorf("expected 3 tools with --tools chat --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools chat --read-only: 3 Chat read-only tools (list_spaces, get_messages, search_messages).
	cs3 := newTestSession(t, server.Config{Tools: []string{"chat"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 3 {
		t.Errorf("expected 3 tools with --tools chat --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsFormsFiltering(t *testing.T) {
	// --tools forms: all 6 Forms tools.
	cs := newTestSession(t, server.Config{Tools: []string{"forms"}})
	names := registeredToolNames(t, cs)
	if len(names) != 6 {
		t.Errorf("expected 6 tools with --tools forms, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"create_form",
		"get_form",
		"set_publish_settings",
		"get_form_response",
		"list_form_responses",
		"batch_update_form",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}

	// --tools forms --tool-tier core: 2 core Forms tools (create_form, get_form).
	cs2 := newTestSession(t, server.Config{Tools: []string{"forms"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 2 {
		t.Errorf("expected 2 tools with --tools forms --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools forms --read-only: 3 Forms read-only tools (get_form, get_form_response, list_form_responses).
	cs3 := newTestSession(t, server.Config{Tools: []string{"forms"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 3 {
		t.Errorf("expected 3 tools with --tools forms --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsSlidesFiltering(t *testing.T) {
	// --tools slides: 5 Slides tools + 4 comment tools = 9.
	cs := newTestSession(t, server.Config{Tools: []string{"slides"}})
	names := registeredToolNames(t, cs)
	if len(names) != 9 {
		t.Errorf("expected 9 tools with --tools slides, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"create_presentation",
		"get_presentation",
		"batch_update_presentation",
		"get_page",
		"get_page_thumbnail",
		"read_presentation_comments",
		"create_presentation_comment",
		"reply_to_presentation_comment",
		"resolve_presentation_comment",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}

	// --tools slides --tool-tier core: 2 core Slides tools (create_presentation, get_presentation).
	cs2 := newTestSession(t, server.Config{Tools: []string{"slides"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 2 {
		t.Errorf("expected 2 tools with --tools slides --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools slides --read-only: 3 Slides read-only (get_presentation, get_page, get_page_thumbnail) + read_presentation_comments = 4.
	cs3 := newTestSession(t, server.Config{Tools: []string{"slides"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 4 {
		t.Errorf("expected 4 tools with --tools slides --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsTasksFiltering(t *testing.T) {
	// --tools tasks: all 12 Tasks tools.
	cs := newTestSession(t, server.Config{Tools: []string{"tasks"}})
	names := registeredToolNames(t, cs)
	if len(names) != 12 {
		t.Errorf("expected 12 tools with --tools tasks, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"list_task_lists",
		"get_task_list",
		"create_task_list",
		"update_task_list",
		"delete_task_list",
		"list_tasks",
		"get_task",
		"create_task",
		"update_task",
		"delete_task",
		"move_task",
		"clear_completed_tasks",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}

	// --tools tasks --tool-tier core: 4 core Tasks tools (get_task, list_tasks, create_task, update_task).
	cs2 := newTestSession(t, server.Config{Tools: []string{"tasks"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 4 {
		t.Errorf("expected 4 tools with --tools tasks --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools tasks --read-only: 4 Tasks read-only tools (get_task, list_tasks, list_task_lists, get_task_list).
	cs3 := newTestSession(t, server.Config{Tools: []string{"tasks"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 4 {
		t.Errorf("expected 4 tools with --tools tasks --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsContactsFiltering(t *testing.T) {
	// --tools contacts: 5 read + 10 write = 15 Contacts tools.
	cs := newTestSession(t, server.Config{Tools: []string{"contacts"}})
	names := registeredToolNames(t, cs)
	if len(names) != 15 {
		t.Errorf("expected 15 tools with --tools contacts, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"list_contacts",
		"get_contact",
		"search_contacts",
		"list_contact_groups",
		"get_contact_group",
		"create_contact",
		"update_contact",
		"delete_contact",
		"batch_create_contacts",
		"batch_update_contacts",
		"batch_delete_contacts",
		"create_contact_group",
		"update_contact_group",
		"delete_contact_group",
		"modify_contact_group_members",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}

	// --tools contacts --tool-tier core: 4 core Contacts tools (search_contacts, get_contact, list_contacts, create_contact).
	cs2 := newTestSession(t, server.Config{Tools: []string{"contacts"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 4 {
		t.Errorf("expected 4 tools with --tools contacts --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools contacts --read-only: 5 Contacts read-only tools (all read tools remain, write tools removed).
	cs3 := newTestSession(t, server.Config{Tools: []string{"contacts"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 5 {
		t.Errorf("expected 5 tools with --tools contacts --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsSearchFiltering(t *testing.T) {
	// --tools search: 3 Search tools.
	cs := newTestSession(t, server.Config{Tools: []string{"search"}})
	names := registeredToolNames(t, cs)
	if len(names) != 3 {
		t.Errorf("expected 3 tools with --tools search, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"search_custom",
		"get_search_engine_info",
		"search_custom_siterestrict",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}

	// --tools search --tool-tier core: 1 core Search tool (search_custom).
	cs2 := newTestSession(t, server.Config{Tools: []string{"search"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 1 {
		t.Errorf("expected 1 tool with --tools search --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools search --read-only: all 3 Search tools (all read-only).
	cs3 := newTestSession(t, server.Config{Tools: []string{"search"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 3 {
		t.Errorf("expected 3 tools with --tools search --read-only, got %d: %v", len(names3), names3)
	}
}

func TestToolsAppScriptFiltering(t *testing.T) {
	// --tools appscript: 8 read + 9 write = 17 AppScript tools.
	cs := newTestSession(t, server.Config{Tools: []string{"appscript"}})
	names := registeredToolNames(t, cs)
	if len(names) != 17 {
		t.Errorf("expected 17 tools with --tools appscript, got %d: %v", len(names), names)
	}
	for _, expected := range []string{
		"list_script_projects",
		"get_script_project",
		"get_script_content",
		"list_deployments",
		"list_script_processes",
		"list_versions",
		"get_version",
		"get_script_metrics",
		"create_script_project",
		"update_script_content",
		"run_script_function",
		"create_deployment",
		"update_deployment",
		"delete_deployment",
		"delete_script_project",
		"create_version",
		"generate_trigger_code",
	} {
		if !names[expected] {
			t.Errorf("expected tool %q to be present", expected)
		}
	}

	// --tools appscript --tool-tier core: 7 core AppScript tools.
	cs2 := newTestSession(t, server.Config{Tools: []string{"appscript"}, ToolTier: "core"})
	names2 := registeredToolNames(t, cs2)
	if len(names2) != 7 {
		t.Errorf("expected 7 tools with --tools appscript --tool-tier core, got %d: %v", len(names2), names2)
	}

	// --tools appscript --read-only: 8 AppScript read-only tools (write tools removed).
	cs3 := newTestSession(t, server.Config{Tools: []string{"appscript"}, ReadOnly: true})
	names3 := registeredToolNames(t, cs3)
	if len(names3) != 8 {
		t.Errorf("expected 8 tools with --tools appscript --read-only, got %d: %v", len(names3), names3)
	}
}

func TestStartGoogleAuthOAuth21Disabled(t *testing.T) {
	// When MCP_ENABLE_OAUTH21 is not set, start_google_auth should be registered.
	t.Setenv("MCP_ENABLE_OAUTH21", "")
	cs := newTestSession(t, server.Config{})
	names := registeredToolNames(t, cs)
	if !names["start_google_auth"] {
		t.Error("expected start_google_auth to be registered when MCP_ENABLE_OAUTH21 is not set")
	}
}

func TestStartGoogleAuthOAuth21Enabled(t *testing.T) {
	// When MCP_ENABLE_OAUTH21=true, start_google_auth should NOT be registered.
	// newTestSession applies the env var before tool registration, so the
	// enable-decision is taken with MCP_ENABLE_OAUTH21 active.
	t.Setenv("MCP_ENABLE_OAUTH21", "true")
	cs := newTestSession(t, server.Config{})
	names := registeredToolNames(t, cs)
	if names["start_google_auth"] {
		t.Error("expected start_google_auth to NOT be registered when MCP_ENABLE_OAUTH21=true")
	}
	// Should have 136 tools (137 - 1 start_google_auth).
	if len(names) != 136 {
		t.Errorf("expected 136 tools with MCP_ENABLE_OAUTH21=true, got %d", len(names))
	}
}

func TestAllowedToolsForTier(t *testing.T) {
	tests := []struct {
		tier     string
		wantNil  bool
		wantTool string
		wantIn   bool
	}{
		{"", true, "", false},
		{"complete", true, "", false},
		{"core", false, "search_gmail_messages", true},
		{"core", false, "get_gmail_attachment_content", false},
		{"extended", false, "search_gmail_messages", true},
		{"extended", false, "get_gmail_attachment_content", true},
		{"extended", false, "get_gmail_threads_content_batch", false},
	}

	for _, tt := range tests {
		allowed := allowedToolsForTier(tt.tier)
		if tt.wantNil {
			if allowed != nil {
				t.Errorf("tier %q: expected nil, got %v", tt.tier, allowed)
			}
			continue
		}
		if allowed == nil {
			t.Errorf("tier %q: expected non-nil", tt.tier)
			continue
		}
		if got := allowed[tt.wantTool]; got != tt.wantIn {
			t.Errorf("tier %q, tool %q: want %v, got %v", tt.tier, tt.wantTool, tt.wantIn, got)
		}
	}
}

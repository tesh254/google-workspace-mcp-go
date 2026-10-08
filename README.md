# google-workspace-mcp-go

Lightweight Go implementation of the [Google Workspace MCP server](https://github.com/taylorwilsdon/google_workspace_mcp) — 137 tools across 12 Google services, single self-contained binary, designed for local AI tool use.

## Why this exists

The [original Python server](https://github.com/taylorwilsdon/google_workspace_mcp) is feature-complete and production-ready, with multi-user OAuth 2.1 support, HTTP transport, and distributed session storage. It's the right choice for shared/hosted deployments.

This Go rewrite exists for a different use case: **single-user, local AI tool use** — specifically Claude Code, Cursor, and similar MCP clients running on your machine. If you want Google Workspace integration with minimal memory usage and without the overhead of a Python runtime, this is for you.

|                      | Python (original)          | Go (this repo)                        |
| -------------------- | -------------------------- | ------------------------------------- |
| Tools                | 137                        | 137                                   |
| Binary/install size  | Python + virtualenv + deps | 27MB single binary                    |
| Startup time         | ~3s ([measured](BENCHMARKS.md))  | ~10ms ([measured](BENCHMARKS.md))   |
| Runtime requirements | Python 3.10+, uv/pip       | None                                  |
| Transport            | stdio, streamable HTTP     | stdio, streamable HTTP (stateless)    |
| Auth                 | OAuth 2.0 + OAuth 2.1      | Google OAuth 2.0 (+ optional bearer auth on HTTP) |
| Multi-user           | Yes (session mgmt, Valkey) | Per-request identity; creds on disk   |
| HTTP server mode     | Yes (sessions/Valkey)      | Yes (stateless, MCP spec 2026-07-28)  |

**Use the Go version if**: you run MCP tools locally in Claude Code or similar, want low memory usage, or want a simple dependency-free HTTP server without distributed session storage.

**Use the Python version if**: you need hosted multi-user deployments with Valkey-backed session management.

## Quick start

### 1. Google Cloud project setup

You need a Google Cloud project with OAuth credentials. If you already have one from using the [original Python server](https://github.com/taylorwilsdon/google_workspace_mcp), the same credentials work here.

**Create OAuth credentials:**

1. Go to the [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project (or select an existing one)
3. Navigate to **APIs & Services > OAuth consent screen** and configure it if you haven't already
4. Navigate to **APIs & Services > Credentials**
5. Click **Create Credentials > OAuth Client ID**
6. Choose **Desktop Application** as the application type
7. Note the **Client ID** and **Client Secret**

**Enable the APIs** you need (click to enable directly):

- [Gmail API](https://console.cloud.google.com/flows/enableapi?apiid=gmail.googleapis.com)
- [Google Drive API](https://console.cloud.google.com/flows/enableapi?apiid=drive.googleapis.com)
- [Google Calendar API](https://console.cloud.google.com/flows/enableapi?apiid=calendar-json.googleapis.com)
- [Google Docs API](https://console.cloud.google.com/flows/enableapi?apiid=docs.googleapis.com)
- [Google Sheets API](https://console.cloud.google.com/flows/enableapi?apiid=sheets.googleapis.com)
- [Google Slides API](https://console.cloud.google.com/flows/enableapi?apiid=slides.googleapis.com)
- [Google Forms API](https://console.cloud.google.com/flows/enableapi?apiid=forms.googleapis.com)
- [Google Tasks API](https://console.cloud.google.com/flows/enableapi?apiid=tasks.googleapis.com)
- [Google Chat API](https://console.cloud.google.com/flows/enableapi?apiid=chat.googleapis.com)
- [People API (Contacts)](https://console.cloud.google.com/flows/enableapi?apiid=people.googleapis.com)
- [Apps Script API](https://console.cloud.google.com/flows/enableapi?apiid=script.googleapis.com)
- [Custom Search API](https://console.cloud.google.com/flows/enableapi?apiid=customsearch.googleapis.com) *(optional, for web search tools)*

You only need to enable the APIs for services you plan to use. For example, if you only need Gmail and Calendar, just enable those two.

### 2. Install

**Download a pre-built binary** (no Go required):

Go to the [Releases](https://github.com/magks/google-workspace-mcp-go/releases) page and download the archive for your platform:

| Platform | File |
|---|---|
| Linux (x86_64) | `google-workspace-mcp-go_*_linux_amd64.tar.gz` |
| Linux (ARM64) | `google-workspace-mcp-go_*_linux_arm64.tar.gz` |
| macOS (Apple Silicon) | `google-workspace-mcp-go_*_darwin_arm64.tar.gz` |
| macOS (Intel) | `google-workspace-mcp-go_*_darwin_amd64.tar.gz` |
| Windows (x86_64) | `google-workspace-mcp-go_*_windows_amd64.zip` |

```bash
# Example: Linux x86_64
tar xzf google-workspace-mcp-go_*_linux_amd64.tar.gz
chmod +x google-workspace-mcp-go
mv google-workspace-mcp-go ~/.local/bin/  # or anywhere on your PATH
```

**Or install with Go** (requires Go 1.24+):

```bash
go install github.com/magks/google-workspace-mcp-go@latest
```

This puts the binary in your `$GOPATH/bin` (usually `~/go/bin`).

**Or build from source**:

```bash
git clone https://github.com/magks/google-workspace-mcp-go.git
cd google-workspace-mcp-go
go build -o google-workspace-mcp-go .
```

### 3. Configure environment

```bash
export GOOGLE_OAUTH_CLIENT_ID="your-client-id.apps.googleusercontent.com"
export GOOGLE_OAUTH_CLIENT_SECRET="your-client-secret"
export USER_GOOGLE_EMAIL="you@gmail.com"  # optional but recommended
```

You can add these to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) so they persist across sessions.

### 4. Add to Claude Code

The binary reads configuration from environment variables. If you already have `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET`, and `USER_GOOGLE_EMAIL` exported in the shell where Claude Code runs, you don't need to repeat them in the MCP config — the binary picks them up automatically.

**Minimal config** (env vars already exported in your shell):

```json
{
  "mcpServers": {
    "google-workspace": {
      "command": "/path/to/google-workspace-mcp-go"
    }
  }
}
```

**Explicit config** (env vars set per-server, useful if you don't want to export globally):

```json
{
  "mcpServers": {
    "google-workspace": {
      "command": "/path/to/google-workspace-mcp-go",
      "env": {
        "GOOGLE_OAUTH_CLIENT_ID": "your-client-id.apps.googleusercontent.com",
        "GOOGLE_OAUTH_CLIENT_SECRET": "your-client-secret",
        "USER_GOOGLE_EMAIL": "you@gmail.com"
      }
    }
  }
}
```

To reduce the number of tools loaded (useful for staying within context limits), use `--tools` and `--tool-tier`:

```json
{
  "mcpServers": {
    "google-workspace": {
      "command": "/path/to/google-workspace-mcp-go",
      "args": ["--tools", "gmail drive calendar", "--tool-tier", "core"]
    }
  }
}
```

### 5. First run — authentication

On first use, you'll need to authenticate with Google:

1. Ask your AI assistant to call the `start_google_auth` tool with your email
2. A browser window opens to Google's OAuth consent screen
3. Authorize the requested permissions
4. Credentials are saved to `~/.google_workspace_mcp/credentials/your-email.json`
5. Subsequent runs use the saved credentials automatically (tokens refresh transparently)

> **Note:** `start_google_auth` requires `gmail` to be in your `--tools` list (or omit `--tools` to load all services). If you're using a restricted tool set without gmail, add it temporarily for initial auth.

## Configuration

### CLI flags

| Flag             | Description                                                              | Default    |
| ---------------- | ------------------------------------------------------------------------ | ---------- |
| `--tools`        | Space-separated list of services to enable (e.g. `gmail drive calendar`) | all        |
| `--tool-tier`    | Tool tier: `core`, `extended`, or `complete`                             | `complete` |
| `--read-only`    | Enable read-only mode (disable all write tools)                          | `false`    |
| `--transport`    | `stdio` (local clients) or `streamable-http`                             | `stdio`    |
| `--http-host`    | Bind host for `streamable-http` (keep on localhost unless fronting)      | `127.0.0.1`|
| `--http-port`    | Bind port for `streamable-http`                                          | `8080`     |
| `--single-user`  | Single-user mode (credential store holds one user)                       | `false`    |

### Environment variables

| Variable                        | Required | Description                                     |
| ------------------------------- | -------- | ----------------------------------------------- |
| `GOOGLE_OAUTH_CLIENT_ID`        | Yes      | OAuth 2.0 Client ID from Google Cloud Console   |
| `GOOGLE_OAUTH_CLIENT_SECRET`    | Yes      | OAuth 2.0 Client Secret                         |
| `USER_GOOGLE_EMAIL`             | No       | Default email (avoids needing it per tool call) |
| `WORKSPACE_MCP_CREDENTIALS_DIR` | No       | Custom credential storage directory             |
| `GOOGLE_PSE_API_KEY`            | No       | Google Programmable Search Engine API key       |
| `GOOGLE_PSE_ENGINE_ID`          | No       | Google Programmable Search Engine ID            |

### Tool tiers

Tiers let you control how many tools are registered, which affects context window usage in AI assistants:

| Tier       | Description                            | Tool count |
| ---------- | -------------------------------------- | ---------- |
| `core`     | Essential read/write tools per service | ~45        |
| `extended` | Core + advanced management tools       | ~95        |
| `complete` | All tools (default)                    | 137        |

### Available services

`gmail` `drive` `calendar` `docs` `sheets` `slides` `forms` `tasks` `chat` `contacts` `search` `appscript`

## Tools

<details>
<summary><strong>Full tool reference (137 tools across 12 services)</strong></summary>

### Gmail (15 tools)

| Tool                                | Tier     | Description                             |
| ----------------------------------- | -------- | --------------------------------------- |
| `search_gmail_messages`             | core     | Search messages with Gmail query syntax |
| `get_gmail_message_content`         | core     | Get full message content                |
| `get_gmail_messages_content_batch`  | core     | Batch retrieve up to 25 messages        |
| `send_gmail_message`                | core     | Send email with optional attachments    |
| `get_gmail_attachment_content`      | extended | Download attachment content             |
| `get_gmail_thread_content`          | extended | Get full conversation thread            |
| `modify_gmail_message_labels`       | extended | Add/remove labels from a message        |
| `list_gmail_labels`                 | extended | List all labels                         |
| `manage_gmail_label`                | extended | Create, update, or delete labels        |
| `draft_gmail_message`               | extended | Create draft email                      |
| `list_gmail_filters`                | extended | List mail filters                       |
| `create_gmail_filter`               | extended | Create new mail filter                  |
| `delete_gmail_filter`               | extended | Delete mail filter                      |
| `get_gmail_threads_content_batch`   | complete | Batch retrieve up to 25 threads         |
| `batch_modify_gmail_message_labels` | complete | Batch label operations                  |

**System tool** (registered with gmail, but not Gmail-specific):

| Tool                | Tier     | Description                       |
| ------------------- | -------- | --------------------------------- |
| `start_google_auth` | complete | Trigger OAuth authentication flow |

### Google Drive (16 tools)

| Tool                             | Tier     | Description                          |
| -------------------------------- | -------- | ------------------------------------ |
| `search_drive_files`             | core     | Search files with Drive query syntax |
| `get_drive_file_content`         | core     | Download file content                |
| `get_drive_file_download_url`    | core     | Get download URL                     |
| `create_drive_file`              | core     | Create new file                      |
| `import_to_google_doc`           | core     | Import file as Google Doc            |
| `share_drive_file`               | core     | Share file with users                |
| `get_drive_shareable_link`       | core     | Generate shareable link              |
| `list_drive_items`               | extended | List files in folder                 |
| `copy_drive_file`                | extended | Duplicate file                       |
| `update_drive_file`              | extended | Update file metadata/content         |
| `update_drive_permission`        | extended | Modify sharing permissions           |
| `remove_drive_permission`        | extended | Revoke access                        |
| `transfer_drive_ownership`       | extended | Transfer file ownership              |
| `batch_share_drive_file`         | extended | Batch sharing                        |
| `get_drive_file_permissions`     | complete | List all permissions                 |
| `check_drive_file_public_access` | complete | Check public sharing status          |

### Google Calendar (6 tools)

| Tool             | Tier     | Description                |
| ---------------- | -------- | -------------------------- |
| `list_calendars` | core     | List user's calendars      |
| `get_events`     | core     | Get events with time range |
| `create_event`   | core     | Create calendar event      |
| `modify_event`   | core     | Update event details       |
| `delete_event`   | extended | Delete event               |
| `query_freebusy` | extended | Check availability         |

### Google Docs (19 tools)

| Tool                         | Tier     | Description                 |
| ---------------------------- | -------- | --------------------------- |
| `get_doc_content`            | core     | Get document text content   |
| `create_doc`                 | core     | Create new document         |
| `modify_doc_text`            | core     | Edit document text          |
| `export_doc_to_pdf`          | extended | Export as PDF               |
| `search_docs`                | extended | Search documents            |
| `find_and_replace_doc`       | extended | Find and replace text       |
| `list_docs_in_folder`        | extended | List docs in Drive folder   |
| `insert_doc_elements`        | extended | Insert formatted elements   |
| `update_paragraph_style`     | extended | Change paragraph formatting |
| `insert_doc_image`           | complete | Insert image                |
| `update_doc_headers_footers` | complete | Edit headers/footers        |
| `batch_update_doc`           | complete | Batch document operations   |
| `inspect_doc_structure`      | complete | Analyze document structure  |
| `create_table_with_data`     | complete | Create and populate table   |
| `debug_table_structure`      | complete | Inspect table layout        |
| `read_document_comments`     | complete | Read all comments           |
| `create_document_comment`    | complete | Add comment                 |
| `reply_to_document_comment`  | complete | Reply to comment            |
| `resolve_document_comment`   | complete | Resolve comment             |

### Google Sheets (14 tools)

| Tool                            | Tier     | Description                  |
| ------------------------------- | -------- | ---------------------------- |
| `create_spreadsheet`            | core     | Create new spreadsheet       |
| `read_sheet_values`             | core     | Read cell values             |
| `modify_sheet_values`           | core     | Write/update cells           |
| `list_spreadsheets`             | extended | List user's spreadsheets     |
| `get_spreadsheet_info`          | extended | Get spreadsheet metadata     |
| `create_sheet`                  | complete | Add worksheet tab            |
| `format_sheet_range`            | complete | Format cell ranges           |
| `add_conditional_formatting`    | complete | Add conditional format rules |
| `update_conditional_formatting` | complete | Modify format rules          |
| `delete_conditional_formatting` | complete | Remove format rules          |
| `read_spreadsheet_comments`     | complete | Read all comments            |
| `create_spreadsheet_comment`    | complete | Add comment                  |
| `reply_to_spreadsheet_comment`  | complete | Reply to comment             |
| `resolve_spreadsheet_comment`   | complete | Resolve comment              |

### Google Slides (9 tools)

| Tool                            | Tier     | Description              |
| ------------------------------- | -------- | ------------------------ |
| `create_presentation`           | core     | Create presentation      |
| `get_presentation`              | core     | Get presentation content |
| `batch_update_presentation`     | extended | Batch slide operations   |
| `get_page`                      | extended | Get individual slide     |
| `get_page_thumbnail`            | extended | Get slide thumbnail      |
| `read_presentation_comments`    | complete | Read all comments        |
| `create_presentation_comment`   | complete | Add comment              |
| `reply_to_presentation_comment` | complete | Reply to comment         |
| `resolve_presentation_comment`  | complete | Resolve comment          |

### Google Forms (6 tools)

| Tool                   | Tier     | Description             |
| ---------------------- | -------- | ----------------------- |
| `create_form`          | core     | Create new form         |
| `get_form`             | core     | Get form details        |
| `list_form_responses`  | extended | List all responses      |
| `set_publish_settings` | complete | Configure publishing    |
| `get_form_response`    | complete | Get individual response |
| `batch_update_form`    | complete | Batch form updates      |

### Google Tasks (12 tools)

| Tool                    | Tier     | Description             |
| ----------------------- | -------- | ----------------------- |
| `get_task`              | core     | Get task details        |
| `list_tasks`            | core     | List tasks              |
| `create_task`           | core     | Create task             |
| `update_task`           | core     | Update task             |
| `delete_task`           | extended | Delete task             |
| `list_task_lists`       | complete | List task lists         |
| `get_task_list`         | complete | Get task list details   |
| `create_task_list`      | complete | Create task list        |
| `update_task_list`      | complete | Update task list        |
| `delete_task_list`      | complete | Delete task list        |
| `move_task`             | complete | Move task between lists |
| `clear_completed_tasks` | complete | Clear completed tasks   |

### Google Chat (4 tools)

| Tool              | Tier     | Description       |
| ----------------- | -------- | ----------------- |
| `send_message`    | core     | Send chat message |
| `get_messages`    | core     | Get messages      |
| `search_messages` | core     | Search messages   |
| `list_spaces`     | extended | List spaces/DMs   |

### Google Contacts (15 tools)

| Tool                           | Tier     | Description          |
| ------------------------------ | -------- | -------------------- |
| `search_contacts`              | core     | Search contacts      |
| `get_contact`                  | core     | Get contact details  |
| `list_contacts`                | core     | List contacts        |
| `create_contact`               | core     | Create contact       |
| `update_contact`               | extended | Update contact       |
| `delete_contact`               | extended | Delete contact       |
| `list_contact_groups`          | extended | List contact groups  |
| `get_contact_group`            | extended | Get group details    |
| `batch_create_contacts`        | complete | Batch create         |
| `batch_update_contacts`        | complete | Batch update         |
| `batch_delete_contacts`        | complete | Batch delete         |
| `create_contact_group`         | complete | Create group         |
| `update_contact_group`         | complete | Update group         |
| `delete_contact_group`         | complete | Delete group         |
| `modify_contact_group_members` | complete | Manage group members |

### Google Custom Search (3 tools)

| Tool                         | Tier     | Description                      |
| ---------------------------- | -------- | -------------------------------- |
| `search_custom`              | core     | Programmable Search Engine query |
| `search_custom_siterestrict` | extended | Site-restricted search           |
| `get_search_engine_info`     | complete | Get search engine config         |

### Google Apps Script (17 tools)

| Tool                    | Tier     | Description             |
| ----------------------- | -------- | ----------------------- |
| `list_script_projects`  | core     | List user's scripts     |
| `get_script_project`    | core     | Get script metadata     |
| `get_script_content`    | core     | Get script source code  |
| `create_script_project` | core     | Create new script       |
| `update_script_content` | core     | Update script code      |
| `run_script_function`   | core     | Execute script function |
| `generate_trigger_code` | core     | Generate trigger code   |
| `create_deployment`     | extended | Deploy script           |
| `list_deployments`      | extended | List deployments        |
| `update_deployment`     | extended | Update deployment       |
| `delete_deployment`     | extended | Remove deployment       |
| `delete_script_project` | extended | Delete script           |
| `list_versions`         | extended | List versions           |
| `create_version`        | extended | Create version          |
| `get_version`           | extended | Get version details     |
| `list_script_processes` | extended | List running processes  |
| `get_script_metrics`    | extended | Get script metrics      |

</details>

## Credential storage

Credentials are stored as JSON files in `~/.google_workspace_mcp/credentials/` (configurable via `WORKSPACE_MCP_CREDENTIALS_DIR`). Each file is named `{email}.json` and contains the OAuth2 tokens.

The credential directory is created with `0700` permissions. Individual credential files are created with `0600` permissions.

## Testing

```bash
# Run all tests (~1100 tests, takes ~1 second)
go test ./...

# Run with verbose output
go test -v ./...

# Run tests for a specific service
go test ./tools/ -run TestGmail
go test ./tools/ -run TestDrive

# Run integration tests against real Google APIs (requires credentials)
INTEGRATION_TEST_EMAIL="you@gmail.com" go test -tags integration ./tools/
```

The test suite has three layers, none requiring network access (except integration):

- **Unit tests** — pure function tests for formatting, parsing, and helper logic
- **Protocol tests** — send MCP `tools/call` messages through the server, verify parameter validation and error paths
- **Mock API tests** — full handler pipeline with `httptest.Server` returning canned Google API responses

Integration tests are gated behind the `integration` build tag and skip automatically when `INTEGRATION_TEST_EMAIL` is not set.

## Limitations

- **stdio transport only** — no HTTP server mode (use the [Python version](https://github.com/taylorwilsdon/google_workspace_mcp) for that)
- **Single-user** — no multi-user session management or OAuth 2.1
- **No attachment serving** — no HTTP endpoint for file downloads
- **Local use only** — designed for local MCP clients, not hosted deployments

## Acknowledgments

This project is a Go rewrite of [google_workspace_mcp](https://github.com/taylorwilsdon/google_workspace_mcp) by [Taylor Wilsdon](https://github.com/taylorwilsdon), originally written in Python. The original project provides the full-featured implementation with multi-user support, OAuth 2.1, HTTP transport, and more. Licensed under MIT.

## License

[MIT](LICENSE)

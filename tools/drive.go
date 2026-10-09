package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

// driveQueryPatterns detects structured Drive queries (vs free text).
var driveQueryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b\w+\s*(=|!=|>|<)\s*['"].*?['"]`),
	regexp.MustCompile(`(?i)\b\w+\s*(=|!=|>|<)\s*\d+`),
	regexp.MustCompile(`(?i)\bcontains\b`),
	regexp.MustCompile(`(?i)\bin\s+parents\b`),
	regexp.MustCompile(`(?i)\bhas\s*\{`),
	regexp.MustCompile(`(?i)\btrashed\s*=\s*(true|false)\b`),
	regexp.MustCompile(`(?i)\bstarred\s*=\s*(true|false)\b`),
	regexp.MustCompile(`(?i)['"][^'"]+['"]\s+in\s+parents`),
	regexp.MustCompile(`(?i)\bfullText\s+contains\b`),
	regexp.MustCompile(`(?i)\bname\s*(=|contains)\b`),
	regexp.MustCompile(`(?i)\bmimeType\s*(=|!=)\b`),
}

const driveFileFields = "nextPageToken, files(id, name, mimeType, webViewLink, iconLink, modifiedTime, size)"

// RegisterDriveTools registers all Drive tools with the MCP server.
func RegisterDriveTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	// Read tools (US-010)
	registerSearchDriveFiles(s, getClient)
	registerGetDriveFileContent(s, getClient)
	registerGetDriveFileDownloadURL(s, getClient)
	registerListDriveItems(s, getClient)
	registerGetDriveFilePermissions(s, getClient)
	registerCheckDriveFilePublicAccess(s, getClient)
	registerGetDriveShareableLink(s, getClient)

	// Write tools (US-011)
	registerCreateDriveFile(s, getClient)
	registerImportToGoogleDoc(s, getClient)
	registerUpdateDriveFile(s, getClient)
	registerCopyDriveFile(s, getClient)
	registerShareDriveFile(s, getClient)
	registerBatchShareDriveFile(s, getClient)
	registerUpdateDrivePermission(s, getClient)
	registerRemoveDrivePermission(s, getClient)
	registerTransferDriveOwnership(s, getClient)
}

// newDriveService creates a drive.Service for the given user email.
func newDriveService(ctx context.Context, getClient httpClientFunc, email string) (*drive.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := drive.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Drive service: %w", err)
	}
	return svc, nil
}

// isStructuredQuery checks if the query looks like a structured Drive API query.
func isStructuredQuery(query string) bool {
	for _, p := range driveQueryPatterns {
		if p.MatchString(query) {
			return true
		}
	}
	return false
}

// buildDriveListCall configures a Drive files.list call with common params.
func buildDriveListCall(svc *drive.Service, query string, pageSize int64, driveID string, includeAllDrives bool, corpora string) *drive.FilesListCall {
	call := svc.Files.List().
		Q(query).
		PageSize(pageSize).
		Fields(driveFileFields).
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(includeAllDrives)

	if driveID != "" {
		call = call.DriveId(driveID)
		if corpora != "" {
			call = call.Corpora(corpora)
		} else {
			call = call.Corpora("drive")
		}
	} else if corpora != "" {
		call = call.Corpora(corpora)
	}

	return call
}

// formatDriveFileList formats a list of Drive files for display.
func formatDriveFileList(files []*drive.File, header string) string {
	var b strings.Builder
	b.WriteString(header)
	for _, f := range files {
		sizeStr := ""
		if f.Size > 0 {
			sizeStr = fmt.Sprintf(", Size: %d", f.Size)
		}
		modified := f.ModifiedTime
		if modified == "" {
			modified = "N/A"
		}
		link := f.WebViewLink
		if link == "" {
			link = "#"
		}
		fmt.Fprintf(&b, "\n- Name: \"%s\" (ID: %s, Type: %s%s, Modified: %s) Link: %s",
			f.Name, f.Id, f.MimeType, sizeStr, modified, link)
	}
	return b.String()
}

// --- search_drive_files ---

func registerSearchDriveFiles(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "search_drive_files",
		Description: "Searches for files and folders within a user's Google Drive, including shared drives.",
	}
	mcp.AddTool(s, tool, handleSearchDriveFiles(getClient))
}

// searchDriveFilesIn is the input schema for search_drive_files.
type searchDriveFilesIn struct {
	UserGoogleEmail           string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	Query                     string `json:"query" jsonschema:"The search query string. Supports Google Drive search operators."`
	PageSize                  int    `json:"page_size,omitempty" jsonschema:"The maximum number of files to return. Defaults to 10."`
	DriveID                   string "json:\"drive_id,omitempty\" jsonschema:\"ID of the shared drive to search. If None, behavior depends on `corpora` and `include_items_from_all_drives`.\""
	IncludeItemsFromAllDrives *bool  `json:"include_items_from_all_drives,omitempty" jsonschema:"Whether shared drive items should be included in results. Defaults to True."`
	Corpora                   string `json:"corpora,omitempty" jsonschema:"Bodies of items to query (e.g., 'user', 'domain', 'drive', 'allDrives')."`
}

func handleSearchDriveFiles(getClient httpClientFunc) mcp.ToolHandlerFor[searchDriveFilesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in searchDriveFilesIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		query := in.Query
		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 10
		}
		driveID := in.DriveID
		includeAllDrives := true
		if in.IncludeItemsFromAllDrives != nil {
			includeAllDrives = *in.IncludeItemsFromAllDrives
		}
		corpora := in.Corpora

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// If query is free text (not structured), wrap in fullText contains.
		finalQuery := query
		if !isStructuredQuery(query) {
			escaped := strings.ReplaceAll(query, "'", "\\'")
			finalQuery = fmt.Sprintf("fullText contains '%s'", escaped)
		}

		resp, err := buildDriveListCall(svc, finalQuery, int64(pageSize), driveID, includeAllDrives, corpora).Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		if len(resp.Files) == 0 {
			return textResult(fmt.Sprintf("No files found for '%s'.", query))
		}

		header := fmt.Sprintf("Found %d files for %s matching '%s':", len(resp.Files), email, query)
		return textResult(formatDriveFileList(resp.Files, header))
	}
}

// --- get_drive_file_content ---

func registerGetDriveFileContent(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_drive_file_content",
		Description: "Retrieves the content of a specific Google Drive file by ID, supporting files in shared drives.\n\n• Native Google Docs, Sheets, Slides → exported as text / CSV.\n• Any other file → downloaded; tries UTF-8 decode, else notes binary.",
	}
	mcp.AddTool(s, tool, handleGetDriveFileContent(getClient))
}

// getDriveFileContentIn is the input schema for get_drive_file_content.
type getDriveFileContentIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	FileID          string `json:"file_id" jsonschema:"Drive file ID."`
}

func handleGetDriveFileContent(getClient httpClientFunc) mcp.ToolHandlerFor[getDriveFileContentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getDriveFileContentIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Resolve shortcuts and get metadata.
		resolvedID, meta, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		mimeType := meta.MimeType
		fileName := meta.Name
		if fileName == "" {
			fileName = "Unknown File"
		}

		// Determine export MIME type for Google native files.
		exportMIME := googleNativeExportMIME(mimeType)

		var data []byte
		if exportMIME != "" {
			resp, err := svc.Files.Export(fileID, exportMIME).Download()
			if err != nil {
				return nil, nil, toolErr("Drive API export error: %v", err)
			}
			defer resp.Body.Close()
			data, err = io.ReadAll(resp.Body)
			if err != nil {
				return nil, nil, toolErr("reading export: %v", err)
			}
		} else {
			resp, err := svc.Files.Get(fileID).SupportsAllDrives(true).Download()
			if err != nil {
				return nil, nil, toolErr("Drive API download error: %v", err)
			}
			defer resp.Body.Close()
			data, err = io.ReadAll(resp.Body)
			if err != nil {
				return nil, nil, toolErr("reading download: %v", err)
			}
		}

		// Try to decode as UTF-8 text.
		bodyText := tryDecodeUTF8(data, mimeType)

		link := meta.WebViewLink
		if link == "" {
			link = "#"
		}

		header := fmt.Sprintf("File: \"%s\" (ID: %s, Type: %s)\nLink: %s\n\n--- CONTENT ---\n",
			fileName, fileID, mimeType, link)

		return textResult(header + bodyText)
	}
}

// --- get_drive_file_download_url ---

func registerGetDriveFileDownloadURL(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name: "get_drive_file_download_url",
		Description: "Gets a download URL for a Google Drive file. The file is prepared and made available via HTTP URL.\n\n" +
			"For Google native files (Docs, Sheets, Slides), exports to a useful format:\n" +
			"• Google Docs → PDF (default) or DOCX if export_format='docx'\n" +
			"• Google Sheets → XLSX (default), PDF if export_format='pdf', or CSV if export_format='csv'\n" +
			"• Google Slides → PDF (default) or PPTX if export_format='pptx'\n\n" +
			"For other files, downloads the original file format.",
	}
	mcp.AddTool(s, tool, handleGetDriveFileDownloadURL(getClient))
}

// getDriveFileDownloadURLIn is the input schema for get_drive_file_download_url.
type getDriveFileDownloadURLIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID          string `json:"file_id" jsonschema:"The Google Drive file ID to get a download URL for."`
	ExportFormat    string `json:"export_format,omitempty" jsonschema:"Optional export format for Google native files. Options: 'pdf', 'docx', 'xlsx', 'csv', 'pptx'."`
}

func handleGetDriveFileDownloadURL(getClient httpClientFunc) mcp.ToolHandlerFor[getDriveFileDownloadURLIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getDriveFileDownloadURLIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID
		exportFormat := in.ExportFormat

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, meta, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID
		mimeType := meta.MimeType
		fileName := meta.Name
		if fileName == "" {
			fileName = "Unknown File"
		}

		// Determine export MIME type for Google native files.
		exportMIME, outputMIME := resolveExportFormat(mimeType, exportFormat)

		var data []byte
		if exportMIME != "" {
			resp, err := svc.Files.Export(fileID, exportMIME).Download()
			if err != nil {
				return nil, nil, toolErr("Drive API export error: %v", err)
			}
			defer resp.Body.Close()
			data, err = io.ReadAll(resp.Body)
			if err != nil {
				return nil, nil, toolErr("reading export: %v", err)
			}
		} else {
			outputMIME = mimeType
			resp, err := svc.Files.Get(fileID).SupportsAllDrives(true).Download()
			if err != nil {
				return nil, nil, toolErr("Drive API download error: %v", err)
			}
			defer resp.Body.Close()
			data, err = io.ReadAll(resp.Body)
			if err != nil {
				return nil, nil, toolErr("reading download: %v", err)
			}
		}

		sizeBytes := len(data)
		sizeKB := float64(sizeBytes) / 1024

		var b strings.Builder
		b.WriteString("File downloaded successfully!\n")
		fmt.Fprintf(&b, "File: %s\n", fileName)
		fmt.Fprintf(&b, "File ID: %s\n", fileID)
		fmt.Fprintf(&b, "Size: %.1f KB (%d bytes)\n", sizeKB, sizeBytes)
		fmt.Fprintf(&b, "MIME Type: %s\n", outputMIME)

		if exportMIME != "" {
			fmt.Fprintf(&b, "\nNote: Google native file exported to %s format.", outputMIME)
		}

		return textResult(b.String())
	}
}

// --- list_drive_items ---

func registerListDriveItems(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_drive_items",
		Description: "Lists files and folders, supporting shared drives.\nIf `drive_id` is specified, lists items within that shared drive. `folder_id` is then relative to that drive.\nIf `drive_id` is not specified, lists items from user's \"My Drive\" and accessible shared drives.",
	}
	mcp.AddTool(s, tool, handleListDriveItems(getClient))
}

// listDriveItemsIn is the input schema for list_drive_items.
type listDriveItemsIn struct {
	UserGoogleEmail           string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FolderID                  string `json:"folder_id,omitempty" jsonschema:"The ID of the Google Drive folder. Defaults to 'root'."`
	PageSize                  int    `json:"page_size,omitempty" jsonschema:"The maximum number of items to return. Defaults to 100."`
	DriveID                   string `json:"drive_id,omitempty" jsonschema:"ID of the shared drive."`
	IncludeItemsFromAllDrives *bool  `json:"include_items_from_all_drives,omitempty" jsonschema:"Whether items from all accessible shared drives should be included. Defaults to True."`
	Corpora                   string `json:"corpora,omitempty" jsonschema:"Corpus to query ('user', 'drive', 'allDrives')."`
}

func handleListDriveItems(getClient httpClientFunc) mcp.ToolHandlerFor[listDriveItemsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listDriveItemsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		folderID := in.FolderID
		if folderID == "" {
			folderID = "root"
		}
		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 100
		}
		driveID := in.DriveID
		includeAllDrives := true
		if in.IncludeItemsFromAllDrives != nil {
			includeAllDrives = *in.IncludeItemsFromAllDrives
		}
		corpora := in.Corpora

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Resolve folder shortcuts.
		resolvedFolderID, err := resolveFolderID(svc, folderID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		query := fmt.Sprintf("'%s' in parents and trashed=false", resolvedFolderID)

		resp, err := buildDriveListCall(svc, query, int64(pageSize), driveID, includeAllDrives, corpora).Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		if len(resp.Files) == 0 {
			return textResult(fmt.Sprintf("No items found in folder '%s'.", folderID))
		}

		header := fmt.Sprintf("Found %d items in folder '%s' for %s:", len(resp.Files), folderID, email)
		return textResult(formatDriveFileList(resp.Files, header))
	}
}

// --- get_drive_file_permissions ---

func registerGetDriveFilePermissions(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_drive_file_permissions",
		Description: "Gets detailed metadata about a Google Drive file including sharing permissions.",
	}
	mcp.AddTool(s, tool, handleGetDriveFilePermissions(getClient))
}

// getDriveFilePermissionsIn is the input schema for get_drive_file_permissions.
type getDriveFilePermissionsIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID          string `json:"file_id" jsonschema:"The ID of the file to check permissions for."`
}

func handleGetDriveFilePermissions(getClient httpClientFunc) mcp.ToolHandlerFor[getDriveFilePermissionsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getDriveFilePermissionsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Resolve shortcuts.
		resolvedID, _, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		fileMeta, err := svc.Files.Get(fileID).
			Fields("id, name, mimeType, size, modifiedTime, owners, permissions(id, type, role, emailAddress, domain, expirationTime, permissionDetails), webViewLink, webContentLink, shared, sharingUser, viewersCanCopyContent").
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "File: %s\n", fileMeta.Name)
		fmt.Fprintf(&b, "ID: %s\n", fileID)
		fmt.Fprintf(&b, "Type: %s\n", fileMeta.MimeType)
		if fileMeta.Size > 0 {
			fmt.Fprintf(&b, "Size: %d bytes\n", fileMeta.Size)
		} else {
			b.WriteString("Size: N/A\n")
		}
		modified := fileMeta.ModifiedTime
		if modified == "" {
			modified = "N/A"
		}
		fmt.Fprintf(&b, "Modified: %s\n", modified)

		b.WriteString("\nSharing Status:\n")
		fmt.Fprintf(&b, "  Shared: %v\n", fileMeta.Shared)

		if fileMeta.SharingUser != nil {
			name := fileMeta.SharingUser.DisplayName
			if name == "" {
				name = "Unknown"
			}
			addr := fileMeta.SharingUser.EmailAddress
			if addr == "" {
				addr = "Unknown"
			}
			fmt.Fprintf(&b, "  Shared by: %s (%s)\n", name, addr)
		}

		perms := fileMeta.Permissions
		if len(perms) > 0 {
			fmt.Fprintf(&b, "  Number of permissions: %d\n", len(perms))
			b.WriteString("  Permissions:\n")
			for _, p := range perms {
				fmt.Fprintf(&b, "    - %s\n", formatPermissionInfo(p))
			}
		} else {
			b.WriteString("  No additional permissions (private file)\n")
		}

		b.WriteString("\nURLs:\n")
		link := fileMeta.WebViewLink
		if link == "" {
			link = "N/A"
		}
		fmt.Fprintf(&b, "  View Link: %s\n", link)
		if fileMeta.WebContentLink != "" {
			fmt.Fprintf(&b, "  Direct Download Link: %s\n", fileMeta.WebContentLink)
		}

		hasPublic := checkPublicLinkPermission(perms)
		if hasPublic {
			b.WriteString("\nThis file is shared with 'Anyone with the link' - it can be inserted into Google Docs\n")
		} else {
			b.WriteString("\nThis file is NOT shared with 'Anyone with the link' - it cannot be inserted into Google Docs\n")
			b.WriteString("   To fix: Right-click the file in Google Drive > Share > Anyone with the link > Viewer\n")
		}

		return textResult(b.String())
	}
}

// --- check_drive_file_public_access ---

func registerCheckDriveFilePublicAccess(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "check_drive_file_public_access",
		Description: "Searches for a file by name and checks if it has public link sharing enabled.",
	}
	mcp.AddTool(s, tool, handleCheckDriveFilePublicAccess(getClient))
}

// checkDriveFilePublicAccessIn is the input schema for check_drive_file_public_access.
type checkDriveFilePublicAccessIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileName        string `json:"file_name" jsonschema:"The name of the file to check."`
}

func handleCheckDriveFilePublicAccess(getClient httpClientFunc) mcp.ToolHandlerFor[checkDriveFilePublicAccessIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in checkDriveFilePublicAccessIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileName := in.FileName

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		escaped := strings.ReplaceAll(fileName, "'", "\\'")
		query := fmt.Sprintf("name = '%s'", escaped)

		files, err := svc.Files.List().
			Q(query).
			PageSize(10).
			Fields("files(id, name, mimeType, webViewLink)").
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		if len(files.Files) == 0 {
			return textResult(fmt.Sprintf("No file found with name '%s'", fileName))
		}

		var b strings.Builder
		if len(files.Files) > 1 {
			fmt.Fprintf(&b, "Found %d files with name '%s':\n", len(files.Files), fileName)
			for _, f := range files.Files {
				fmt.Fprintf(&b, "  - %s (ID: %s)\n", f.Name, f.Id)
			}
			b.WriteString("\nChecking the first file...\n\n")
		}

		// Check permissions for the first file.
		fileID := files.Files[0].Id
		resolvedID, _, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		fileMeta, err := svc.Files.Get(fileID).
			Fields("id, name, mimeType, permissions, webViewLink, webContentLink, shared").
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		fmt.Fprintf(&b, "File: %s\n", fileMeta.Name)
		fmt.Fprintf(&b, "ID: %s\n", fileID)
		fmt.Fprintf(&b, "Type: %s\n", fileMeta.MimeType)
		fmt.Fprintf(&b, "Shared: %v\n\n", fileMeta.Shared)

		hasPublic := checkPublicLinkPermission(fileMeta.Permissions)
		if hasPublic {
			b.WriteString("PUBLIC ACCESS ENABLED - This file can be inserted into Google Docs\n")
			fmt.Fprintf(&b, "Use with insert_doc_image_url: https://drive.google.com/uc?export=view&id=%s\n", fileID)
		} else {
			b.WriteString("NO PUBLIC ACCESS - Cannot insert into Google Docs\n")
			b.WriteString("Fix: Drive > Share > 'Anyone with the link' > 'Viewer'\n")
		}

		return textResult(b.String())
	}
}

// --- get_drive_shareable_link ---

func registerGetDriveShareableLink(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_drive_shareable_link",
		Description: "Gets the shareable link for a Google Drive file or folder.",
	}
	mcp.AddTool(s, tool, handleGetDriveShareableLink(getClient))
}

// getDriveShareableLinkIn is the input schema for get_drive_shareable_link.
type getDriveShareableLinkIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID          string `json:"file_id" jsonschema:"The ID of the file or folder to get the shareable link for. Required."`
}

func handleGetDriveShareableLink(getClient httpClientFunc) mcp.ToolHandlerFor[getDriveShareableLinkIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getDriveShareableLinkIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, _, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		fileMeta, err := svc.Files.Get(fileID).
			Fields("id, name, mimeType, webViewLink, webContentLink, shared, permissions(id, type, role, emailAddress, domain, expirationTime)").
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "File: %s\n", fileMeta.Name)
		fmt.Fprintf(&b, "ID: %s\n", fileID)
		fmt.Fprintf(&b, "Type: %s\n", fileMeta.MimeType)
		fmt.Fprintf(&b, "Shared: %v\n", fileMeta.Shared)

		link := fileMeta.WebViewLink
		if link == "" {
			link = "N/A"
		}
		fmt.Fprintf(&b, "\nLinks:\n  View: %s\n", link)
		if fileMeta.WebContentLink != "" {
			fmt.Fprintf(&b, "  Download: %s\n", fileMeta.WebContentLink)
		}

		perms := fileMeta.Permissions
		if len(perms) > 0 {
			b.WriteString("\nCurrent permissions:\n")
			for _, p := range perms {
				fmt.Fprintf(&b, "  - %s\n", formatPermissionInfo(p))
			}
		}

		return textResult(b.String())
	}
}

// --- create_drive_file ---

func registerCreateDriveFile(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_drive_file",
		Description: "Creates a new file in Google Drive, supporting creation within shared drives.\nAccepts either direct content or a fileUrl to fetch the content from.",
	}
	mcp.AddTool(s, tool, handleCreateDriveFile(getClient))
}

// createDriveFileIn is the input schema for create_drive_file.
type createDriveFileIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileName        string `json:"file_name" jsonschema:"The name for the new file."`
	Content         string `json:"content,omitempty" jsonschema:"If provided, the content to write to the file."`
	FolderID        string `json:"folder_id,omitempty" jsonschema:"The ID of the parent folder. Defaults to 'root'."`
	MimeType        string `json:"mime_type,omitempty" jsonschema:"The MIME type of the file. Defaults to 'text/plain'."`
	FileURL         string `json:"fileUrl,omitempty" jsonschema:"If provided, fetches the file content from this URL. Supports file://, http://, and https:// protocols."`
}

func handleCreateDriveFile(getClient httpClientFunc) mcp.ToolHandlerFor[createDriveFileIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createDriveFileIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileName := in.FileName
		content := in.Content
		folderID := in.FolderID
		if folderID == "" {
			folderID = "root"
		}
		mimeType := in.MimeType
		if mimeType == "" {
			mimeType = "text/plain"
		}
		fileURL := in.FileURL

		if content == "" && fileURL == "" {
			return nil, nil, toolErr("You must provide either 'content' or 'fileUrl'.")
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedFolderID, err := resolveFolderID(svc, folderID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		fileMeta := &drive.File{
			Name:     fileName,
			Parents:  []string{resolvedFolderID},
			MimeType: mimeType,
		}

		// For now, only support direct content (fileUrl support requires HTTP client).
		var reader io.Reader
		if content != "" {
			reader = bytes.NewReader([]byte(content))
		} else {
			return nil, nil, toolErr("fileUrl support is not yet implemented in the Go server. Please use 'content' parameter instead.")
		}

		created, err := svc.Files.Create(fileMeta).
			Media(reader, googleapi.ContentType(mimeType)).
			Fields("id, name, webViewLink").
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		link := created.WebViewLink
		if link == "" {
			link = "No link available"
		}

		msg := fmt.Sprintf("Successfully created file '%s' (ID: %s) in folder '%s' for %s. Link: %s",
			created.Name, created.Id, folderID, email, link)
		return textResult(msg)
	}
}

// --- import_to_google_doc ---

// googleDocsImportFormats maps file extensions to MIME types for Docs conversion.
var googleDocsImportFormats = map[string]string{
	".md":       "text/markdown",
	".markdown": "text/markdown",
	".txt":      "text/plain",
	".text":     "text/plain",
	".html":     "text/html",
	".htm":      "text/html",
	".docx":     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".doc":      "application/msword",
	".rtf":      "application/rtf",
	".odt":      "application/vnd.oasis.opendocument.text",
}

const googleDocsMIMEType = "application/vnd.google-apps.document"

func registerImportToGoogleDoc(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "import_to_google_doc",
		Description: "Imports a file (Markdown, DOCX, TXT, HTML, RTF, ODT) into Google Docs format with automatic conversion.\n\nGoogle Drive automatically converts the source file to native Google Docs format, preserving formatting like headings, lists, bold, italic, etc.",
	}
	mcp.AddTool(s, tool, handleImportToGoogleDoc(getClient))
}

// importToGoogleDocIn is the input schema for import_to_google_doc.
type importToGoogleDocIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileName        string `json:"file_name" jsonschema:"The name for the new Google Doc (extension will be ignored)."`
	Content         string `json:"content,omitempty" jsonschema:"Text content for text-based formats (MD, TXT, HTML)."`
	FilePath        string `json:"file_path,omitempty" jsonschema:"Local file path for binary formats (DOCX, ODT). Supports file:// URLs."`
	FileURL         string `json:"file_url,omitempty" jsonschema:"Remote URL to fetch the file from (http/https)."`
	SourceFormat    string `json:"source_format,omitempty" jsonschema:"Source format hint ('md', 'markdown', 'docx', 'txt', 'html', 'rtf', 'odt'). Auto-detected from file_name extension if not provided."`
	FolderID        string `json:"folder_id,omitempty" jsonschema:"The ID of the parent folder. Defaults to 'root'."`
}

func handleImportToGoogleDoc(getClient httpClientFunc) mcp.ToolHandlerFor[importToGoogleDocIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in importToGoogleDocIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileName := in.FileName
		content := in.Content
		filePath := in.FilePath
		fileURL := in.FileURL
		sourceFormat := in.SourceFormat
		folderID := in.FolderID
		if folderID == "" {
			folderID = "root"
		}

		// Validate exactly one source provided.
		sourceCount := 0
		if content != "" {
			sourceCount++
		}
		if filePath != "" {
			sourceCount++
		}
		if fileURL != "" {
			sourceCount++
		}
		if sourceCount == 0 {
			return nil, nil, toolErr("You must provide one of: 'content', 'file_path', or 'file_url'.")
		}
		if sourceCount > 1 {
			return nil, nil, toolErr("Provide only one of: 'content', 'file_path', or 'file_url'.")
		}

		// Determine source MIME type.
		var sourceMIME string
		if sourceFormat != "" {
			key := "." + strings.ToLower(strings.TrimPrefix(sourceFormat, "."))
			mime, ok := googleDocsImportFormats[key]
			if !ok {
				return nil, nil, toolErr("Unsupported source_format: '%s'", sourceFormat)
			}
			sourceMIME = mime
		} else {
			sourceMIME = detectSourceFormat(fileName, content)
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedFolderID, err := resolveFolderID(svc, folderID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		// Strip extension from file name for the Doc title.
		docName := fileName
		if idx := strings.LastIndex(fileName, "."); idx > 0 {
			docName = fileName[:idx]
		}

		fileMeta := &drive.File{
			Name:     docName,
			Parents:  []string{resolvedFolderID},
			MimeType: googleDocsMIMEType,
		}

		var reader io.Reader
		if content != "" {
			reader = bytes.NewReader([]byte(content))
		} else {
			return nil, nil, toolErr("file_path and file_url support are not yet implemented in the Go server. Please use 'content' parameter instead.")
		}

		created, err := svc.Files.Create(fileMeta).
			Media(reader, googleapi.ContentType(sourceMIME)).
			Fields("id, name, webViewLink, mimeType").
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		link := created.WebViewLink
		if link == "" {
			link = "No link available"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully imported '%s' as Google Doc\n", docName)
		fmt.Fprintf(&b, "   Document ID: %s\n", created.Id)
		fmt.Fprintf(&b, "   Source format: %s\n", sourceMIME)
		fmt.Fprintf(&b, "   Folder: %s\n", folderID)
		fmt.Fprintf(&b, "   Link: %s", link)

		return textResult(b.String())
	}
}

// --- update_drive_file ---

func registerUpdateDriveFile(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "update_drive_file",
		Description: "Updates metadata and properties of a Google Drive file.",
	}
	mcp.AddTool(s, tool, handleUpdateDriveFile(getClient))
}

// updateDriveFileIn is the input schema for update_drive_file.
type updateDriveFileIn struct {
	UserGoogleEmail              string         `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID                       string         `json:"file_id" jsonschema:"The ID of the file to update. Required."`
	Name                         string         `json:"name,omitempty" jsonschema:"New name for the file."`
	Description                  string         `json:"description,omitempty" jsonschema:"New description for the file."`
	MimeType                     string         `json:"mime_type,omitempty" jsonschema:"New MIME type (note: changing type may require content upload)."`
	AddParents                   string         `json:"add_parents,omitempty" jsonschema:"Comma-separated folder IDs to add as parents."`
	RemoveParents                string         `json:"remove_parents,omitempty" jsonschema:"Comma-separated folder IDs to remove from parents."`
	Starred                      *bool          `json:"starred,omitempty" jsonschema:"Whether to star/unstar the file."`
	Trashed                      *bool          `json:"trashed,omitempty" jsonschema:"Whether to move file to/from trash."`
	WritersCanShare              *bool          `json:"writers_can_share,omitempty" jsonschema:"Whether editors can share the file."`
	CopyRequiresWriterPermission *bool          `json:"copy_requires_writer_permission,omitempty" jsonschema:"Whether copying requires writer permission."`
	Properties                   map[string]any `json:"properties,omitempty" jsonschema:"Custom key-value properties for the file."`
}

func handleUpdateDriveFile(getClient httpClientFunc) mcp.ToolHandlerFor[updateDriveFileIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateDriveFileIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, _, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		// Build the update body.
		updateBody := &drive.File{}
		hasUpdate := false

		if in.Name != "" {
			updateBody.Name = in.Name
			hasUpdate = true
		}
		if in.Description != "" {
			updateBody.Description = in.Description
			hasUpdate = true
		}
		if in.MimeType != "" {
			updateBody.MimeType = in.MimeType
			hasUpdate = true
		}
		if in.Starred != nil {
			b := *in.Starred
			updateBody.Starred = b
			hasUpdate = true
			if !b {
				updateBody.ForceSendFields = append(updateBody.ForceSendFields, "Starred")
			}
		}
		if in.Trashed != nil {
			b := *in.Trashed
			updateBody.Trashed = b
			hasUpdate = true
			if !b {
				updateBody.ForceSendFields = append(updateBody.ForceSendFields, "Trashed")
			}
		}
		if in.WritersCanShare != nil {
			b := *in.WritersCanShare
			updateBody.WritersCanShare = b
			hasUpdate = true
			if !b {
				updateBody.ForceSendFields = append(updateBody.ForceSendFields, "WritersCanShare")
			}
		}
		if in.CopyRequiresWriterPermission != nil {
			b := *in.CopyRequiresWriterPermission
			updateBody.CopyRequiresWriterPermission = b
			hasUpdate = true
			if !b {
				updateBody.ForceSendFields = append(updateBody.ForceSendFields, "CopyRequiresWriterPermission")
			}
		}
		if in.Properties != nil {
			props := in.Properties
			strProps := make(map[string]string, len(props))
			for k, val := range props {
				strProps[k] = fmt.Sprintf("%v", val)
			}
			updateBody.Properties = strProps
			hasUpdate = true
		}

		addParents := in.AddParents
		removeParents := in.RemoveParents

		if !hasUpdate && addParents == "" && removeParents == "" {
			return nil, nil, toolErr("No updates specified.")
		}

		call := svc.Files.Update(fileID, updateBody).
			SupportsAllDrives(true).
			Fields("id, name, webViewLink")

		if addParents != "" {
			call = call.AddParents(addParents)
		}
		if removeParents != "" {
			call = call.RemoveParents(removeParents)
		}

		updated, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		link := updated.WebViewLink
		if link == "" {
			link = "#"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully updated file: %s\n", updated.Name)
		fmt.Fprintf(&b, "   File ID: %s\n", fileID)
		fmt.Fprintf(&b, "View file: %s", link)

		return textResult(b.String())
	}
}

// --- copy_drive_file ---

func registerCopyDriveFile(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "copy_drive_file",
		Description: "Creates a copy of an existing Google Drive file.\n\nThe copy maintains all formatting and content from the original file.",
	}
	mcp.AddTool(s, tool, handleCopyDriveFile(getClient))
}

// copyDriveFileIn is the input schema for copy_drive_file.
type copyDriveFileIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID          string `json:"file_id" jsonschema:"The ID of the file to copy. Required."`
	NewName         string `json:"new_name,omitempty" jsonschema:"New name for the copied file. If not provided, uses \"Copy of [original name]\"."`
	ParentFolderID  string `json:"parent_folder_id,omitempty" jsonschema:"The ID of the folder where the copy should be created. Defaults to 'root' (My Drive)."`
}

func handleCopyDriveFile(getClient httpClientFunc) mcp.ToolHandlerFor[copyDriveFileIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in copyDriveFileIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID
		newName := in.NewName
		parentFolderID := in.ParentFolderID
		if parentFolderID == "" {
			parentFolderID = "root"
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, meta, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID
		originalName := meta.Name
		if originalName == "" {
			originalName = "Unknown File"
		}

		resolvedFolderID, err := resolveFolderID(svc, parentFolderID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		copyBody := &drive.File{}
		if newName != "" {
			copyBody.Name = newName
		} else {
			copyBody.Name = "Copy of " + originalName
		}
		if resolvedFolderID != "root" {
			copyBody.Parents = []string{resolvedFolderID}
		}

		copied, err := svc.Files.Copy(fileID, copyBody).
			SupportsAllDrives(true).
			Fields("id, name, webViewLink, mimeType, parents").
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully copied '%s'\n", originalName)
		fmt.Fprintf(&b, "\nOriginal file ID: %s\n", fileID)
		fmt.Fprintf(&b, "New file ID: %s\n", copied.Id)
		fmt.Fprintf(&b, "New file name: %s\n", copied.Name)
		fmt.Fprintf(&b, "File type: %s\n", copied.MimeType)
		fmt.Fprintf(&b, "Location: %s\n", parentFolderID)
		link := copied.WebViewLink
		if link == "" {
			link = "N/A"
		}
		fmt.Fprintf(&b, "\nView copied file: %s", link)

		return textResult(b.String())
	}
}

// --- share_drive_file ---

func registerShareDriveFile(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "share_drive_file",
		Description: "Shares a Google Drive file or folder with a user, group, domain, or anyone with the link.\n\nWhen sharing a folder, all files inside inherit the permission.",
	}
	mcp.AddTool(s, tool, handleShareDriveFile(getClient))
}

// shareDriveFileIn is the input schema for share_drive_file.
type shareDriveFileIn struct {
	UserGoogleEmail       string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID                string `json:"file_id" jsonschema:"The ID of the file or folder to share. Required."`
	ShareWith             string `json:"share_with,omitempty" jsonschema:"Email address (for user/group), domain name (for domain), or omit for 'anyone'."`
	Role                  string `json:"role,omitempty" jsonschema:"Permission role - 'reader', 'commenter', or 'writer'. Defaults to 'reader'."`
	ShareType             string `json:"share_type,omitempty" jsonschema:"Type of sharing - 'user', 'group', 'domain', or 'anyone'. Defaults to 'user'."`
	SendNotificationEmail *bool  `json:"send_notification,omitempty" jsonschema:"Whether to send a notification email. Defaults to true."`
	EmailMessage          string `json:"email_message,omitempty" jsonschema:"Custom message for the notification email."`
	ExpirationTime        string `json:"expiration_time,omitempty" jsonschema:"Expiration time in RFC 3339 format (e.g., \"2025-01-15T00:00:00Z\")."`
	AllowFileDiscovery    *bool  `json:"allow_file_discovery,omitempty" jsonschema:"For 'domain' or 'anyone' shares - whether the file can be found via search."`
}

func handleShareDriveFile(getClient httpClientFunc) mcp.ToolHandlerFor[shareDriveFileIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in shareDriveFileIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID
		shareWith := in.ShareWith
		role := in.Role
		if role == "" {
			role = "reader"
		}
		shareType := in.ShareType
		if shareType == "" {
			shareType = "user"
		}
		sendNotification := true
		if in.SendNotificationEmail != nil {
			sendNotification = *in.SendNotificationEmail
		}
		emailMessage := in.EmailMessage
		expirationTime := in.ExpirationTime

		// Validate role.
		if role != "reader" && role != "commenter" && role != "writer" {
			return nil, nil, toolErr("Invalid role '%s'. Must be 'reader', 'commenter', or 'writer'.", role)
		}
		// Validate share_type.
		if shareType != "user" && shareType != "group" && shareType != "domain" && shareType != "anyone" {
			return nil, nil, toolErr("Invalid share_type '%s'. Must be 'user', 'group', 'domain', or 'anyone'.", shareType)
		}

		if (shareType == "user" || shareType == "group") && shareWith == "" {
			return nil, nil, toolErr("share_with is required for share_type '%s'", shareType)
		}
		if shareType == "domain" && shareWith == "" {
			return nil, nil, toolErr("share_with (domain name) is required for share_type 'domain'")
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, meta, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		perm := &drive.Permission{
			Type: shareType,
			Role: role,
		}
		if shareType == "user" || shareType == "group" {
			perm.EmailAddress = shareWith
		} else if shareType == "domain" {
			perm.Domain = shareWith
		}
		if expirationTime != "" {
			perm.ExpirationTime = expirationTime
		}
		if shareType == "domain" || shareType == "anyone" {
			if in.AllowFileDiscovery != nil {
				b := *in.AllowFileDiscovery
				perm.AllowFileDiscovery = b
				if !b {
					perm.ForceSendFields = append(perm.ForceSendFields, "AllowFileDiscovery")
				}
			}
		}

		call := svc.Permissions.Create(fileID, perm).
			SupportsAllDrives(true).
			Fields("id, type, role, emailAddress, domain, expirationTime")

		if shareType == "user" || shareType == "group" {
			call = call.SendNotificationEmail(sendNotification)
			if emailMessage != "" {
				call = call.EmailMessage(emailMessage)
			}
		}

		created, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		fileName := meta.Name
		if fileName == "" {
			fileName = "Unknown"
		}
		link := meta.WebViewLink
		if link == "" {
			link = "N/A"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully shared '%s'\n", fileName)
		b.WriteString("\nPermission created:\n")
		fmt.Fprintf(&b, "  - %s\n", formatPermissionInfo(created))
		fmt.Fprintf(&b, "\nView link: %s", link)

		return textResult(b.String())
	}
}

// --- batch_share_drive_file ---

func registerBatchShareDriveFile(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "batch_share_drive_file",
		Description: "Shares a Google Drive file or folder with multiple users or groups in a single operation.\n\nEach recipient can have a different role and optional expiration time.\nRecipients are processed sequentially.",
	}
	mcp.AddTool(s, tool, handleBatchShareDriveFile(getClient))
}

// batchShareDriveFileIn is the input schema for batch_share_drive_file.
type batchShareDriveFileIn struct {
	UserGoogleEmail       string           `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID                string           `json:"file_id" jsonschema:"The ID of the file or folder to share. Required."`
	Recipients            []map[string]any `json:"recipients" jsonschema:"List of recipient objects. Each should have: email (str), role (str, optional, default 'reader'), share_type (str, optional, default 'user'), expiration_time (str, optional). For domain shares, use 'domain' field instead of 'email'."`
	SendNotificationEmail *bool            `json:"send_notification,omitempty" jsonschema:"Whether to send notification emails. Defaults to true."`
	EmailMessage          string           `json:"email_message,omitempty" jsonschema:"Custom message for notification emails."`
}

func handleBatchShareDriveFile(getClient httpClientFunc) mcp.ToolHandlerFor[batchShareDriveFileIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in batchShareDriveFileIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID
		sendNotification := true
		if in.SendNotificationEmail != nil {
			sendNotification = *in.SendNotificationEmail
		}
		emailMessage := in.EmailMessage

		if in.Recipients == nil {
			return nil, nil, toolErr("recipients is required")
		}
		if len(in.Recipients) == 0 {
			return nil, nil, toolErr("recipients list cannot be empty")
		}
		recipientsList := in.Recipients

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, meta, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		var results []string
		successCount := 0
		failureCount := 0

		for _, recipient := range recipientsList {
			if recipient == nil {
				results = append(results, "  - Skipped: invalid recipient format")
				failureCount++
				continue
			}

			shareType := "user"
			if v, ok := recipient["share_type"].(string); ok && v != "" {
				shareType = v
			}

			var identifier string
			if shareType == "domain" {
				domain, _ := recipient["domain"].(string)
				if domain == "" {
					results = append(results, "  - Skipped: missing domain for domain share")
					failureCount++
					continue
				}
				identifier = domain
			} else {
				recipientEmail, _ := recipient["email"].(string)
				if recipientEmail == "" {
					results = append(results, "  - Skipped: missing email address")
					failureCount++
					continue
				}
				identifier = recipientEmail
			}

			role := "reader"
			if v, ok := recipient["role"].(string); ok && v != "" {
				role = v
			}
			if role != "reader" && role != "commenter" && role != "writer" {
				results = append(results, fmt.Sprintf("  - %s: Failed - invalid role '%s'", identifier, role))
				failureCount++
				continue
			}

			perm := &drive.Permission{
				Type: shareType,
				Role: role,
			}
			if shareType == "domain" {
				perm.Domain = identifier
			} else {
				perm.EmailAddress = identifier
			}

			if v, ok := recipient["expiration_time"].(string); ok && v != "" {
				perm.ExpirationTime = v
			}

			call := svc.Permissions.Create(fileID, perm).
				SupportsAllDrives(true).
				Fields("id, type, role, emailAddress, domain, expirationTime")

			if shareType == "user" || shareType == "group" {
				call = call.SendNotificationEmail(sendNotification)
				if emailMessage != "" {
					call = call.EmailMessage(emailMessage)
				}
			}

			created, err := call.Do()
			if err != nil {
				results = append(results, fmt.Sprintf("  - %s: Failed - %v", identifier, err))
				failureCount++
			} else {
				results = append(results, fmt.Sprintf("  - %s", formatPermissionInfo(created)))
				successCount++
			}
		}

		fileName := meta.Name
		if fileName == "" {
			fileName = "Unknown"
		}
		link := meta.WebViewLink
		if link == "" {
			link = "N/A"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Batch share results for '%s'\n", fileName)
		fmt.Fprintf(&b, "\nSummary: %d succeeded, %d failed\n", successCount, failureCount)
		b.WriteString("\nResults:\n")
		for _, r := range results {
			b.WriteString(r)
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "\nView link: %s", link)

		return textResult(b.String())
	}
}

// --- update_drive_permission ---

func registerUpdateDrivePermission(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "update_drive_permission",
		Description: "Updates an existing permission on a Google Drive file or folder.",
	}
	mcp.AddTool(s, tool, handleUpdateDrivePermission(getClient))
}

// updateDrivePermissionIn is the input schema for update_drive_permission.
type updateDrivePermissionIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID          string `json:"file_id" jsonschema:"The ID of the file or folder. Required."`
	PermissionID    string `json:"permission_id" jsonschema:"The ID of the permission to update (from get_drive_file_permissions). Required."`
	Role            string `json:"role,omitempty" jsonschema:"New role - 'reader', 'commenter', or 'writer'."`
	ExpirationTime  string `json:"expiration_time,omitempty" jsonschema:"Expiration time in RFC 3339 format (e.g., \"2025-01-15T00:00:00Z\")."`
}

func handleUpdateDrivePermission(getClient httpClientFunc) mcp.ToolHandlerFor[updateDrivePermissionIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateDrivePermissionIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID
		permissionID := in.PermissionID
		role := in.Role
		expirationTime := in.ExpirationTime

		if role == "" && expirationTime == "" {
			return nil, nil, toolErr("Must provide at least one of: role, expiration_time")
		}
		if role != "" && role != "reader" && role != "commenter" && role != "writer" {
			return nil, nil, toolErr("Invalid role '%s'. Must be 'reader', 'commenter', or 'writer'.", role)
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, meta, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		// If role not provided, fetch current role (Google API requires it in update body).
		if role == "" {
			currentPerm, err := svc.Permissions.Get(fileID, permissionID).
				SupportsAllDrives(true).
				Fields("role").
				Do()
			if err != nil {
				return nil, nil, toolErr("Drive API error: %v", err)
			}
			role = currentPerm.Role
		}

		updateBody := &drive.Permission{
			Role: role,
		}
		if expirationTime != "" {
			updateBody.ExpirationTime = expirationTime
		}

		updated, err := svc.Permissions.Update(fileID, permissionID, updateBody).
			SupportsAllDrives(true).
			Fields("id, type, role, emailAddress, domain, expirationTime").
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		fileName := meta.Name
		if fileName == "" {
			fileName = "Unknown"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully updated permission on '%s'\n", fileName)
		b.WriteString("\nUpdated permission:\n")
		fmt.Fprintf(&b, "  - %s", formatPermissionInfo(updated))

		return textResult(b.String())
	}
}

// --- remove_drive_permission ---

func registerRemoveDrivePermission(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "remove_drive_permission",
		Description: "Removes a permission from a Google Drive file or folder, revoking access.",
	}
	mcp.AddTool(s, tool, handleRemoveDrivePermission(getClient))
}

// removeDrivePermissionIn is the input schema for remove_drive_permission.
type removeDrivePermissionIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID          string `json:"file_id" jsonschema:"The ID of the file or folder. Required."`
	PermissionID    string `json:"permission_id" jsonschema:"The ID of the permission to remove (from get_drive_file_permissions). Required."`
}

func handleRemoveDrivePermission(getClient httpClientFunc) mcp.ToolHandlerFor[removeDrivePermissionIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in removeDrivePermissionIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID
		permissionID := in.PermissionID

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, meta, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		err = svc.Permissions.Delete(fileID, permissionID).
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		fileName := meta.Name
		if fileName == "" {
			fileName = "Unknown"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully removed permission from '%s'\n", fileName)
		fmt.Fprintf(&b, "\nPermission ID '%s' has been revoked.", permissionID)

		return textResult(b.String())
	}
}

// --- transfer_drive_ownership ---

func registerTransferDriveOwnership(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "transfer_drive_ownership",
		Description: "Transfers ownership of a Google Drive file or folder to another user.\n\nThis is an irreversible operation. The current owner will become an editor.\nOnly works within the same Google Workspace domain or for personal accounts.",
	}
	mcp.AddTool(s, tool, handleTransferDriveOwnership(getClient))
}

// transferDriveOwnershipIn is the input schema for transfer_drive_ownership.
type transferDriveOwnershipIn struct {
	UserGoogleEmail     string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	FileID              string `json:"file_id" jsonschema:"The ID of the file or folder to transfer. Required."`
	NewOwnerEmail       string `json:"new_owner_email" jsonschema:"Email address of the new owner. Required."`
	MoveToNewOwnersRoot bool   `json:"move_to_new_owners_root,omitempty" jsonschema:"If true, moves the file to the new owner's My Drive root. Defaults to false."`
}

func handleTransferDriveOwnership(getClient httpClientFunc) mcp.ToolHandlerFor[transferDriveOwnershipIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in transferDriveOwnershipIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		fileID := in.FileID
		newOwnerEmail := in.NewOwnerEmail
		moveToRoot := in.MoveToNewOwnersRoot

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resolvedID, meta, err := resolveDriveItem(svc, fileID)
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}
		fileID = resolvedID

		// Get current owners.
		fileMeta, err := svc.Files.Get(fileID).
			Fields("owners").
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		var currentOwnerEmails []string
		if fileMeta.Owners != nil {
			for _, o := range fileMeta.Owners {
				if o.EmailAddress != "" {
					currentOwnerEmails = append(currentOwnerEmails, o.EmailAddress)
				}
			}
		}

		perm := &drive.Permission{
			Type:         "user",
			Role:         "owner",
			EmailAddress: newOwnerEmail,
		}

		_, err = svc.Permissions.Create(fileID, perm).
			TransferOwnership(true).
			MoveToNewOwnersRoot(moveToRoot).
			SupportsAllDrives(true).
			Fields("id, type, role, emailAddress").
			Do()
		if err != nil {
			return nil, nil, toolErr("Drive API error: %v", err)
		}

		fileName := meta.Name
		if fileName == "" {
			fileName = "Unknown"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully transferred ownership of '%s'\n", fileName)
		fmt.Fprintf(&b, "\nNew owner: %s\n", newOwnerEmail)
		if len(currentOwnerEmails) > 0 {
			fmt.Fprintf(&b, "Previous owner(s): %s\n", strings.Join(currentOwnerEmails, ", "))
		} else {
			b.WriteString("Previous owner(s): Unknown\n")
		}
		if moveToRoot {
			fmt.Fprintf(&b, "File moved to %s's My Drive root.\n", newOwnerEmail)
		}
		b.WriteString("\nNote: Previous owner now has editor access.")

		return textResult(b.String())
	}
}

// --- Drive helper functions ---

const shortcutMIMEType = "application/vnd.google-apps.shortcut"
const folderMIMEType = "application/vnd.google-apps.folder"

// resolveDriveItem resolves Drive shortcuts to the real item.
// Returns the resolved file ID and file metadata.
func resolveDriveItem(svc *drive.Service, fileID string) (string, *drive.File, error) {
	const maxDepth = 5
	currentID := fileID

	for depth := 0; ; depth++ {
		meta, err := svc.Files.Get(currentID).
			Fields("id, mimeType, name, webViewLink, parents, shortcutDetails(targetId, targetMimeType)").
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return "", nil, err
		}

		if meta.MimeType != shortcutMIMEType {
			return currentID, meta, nil
		}

		if meta.ShortcutDetails == nil || meta.ShortcutDetails.TargetId == "" {
			return "", nil, fmt.Errorf("shortcut '%s' is missing target details", currentID)
		}

		if depth >= maxDepth {
			return "", nil, fmt.Errorf("shortcut resolution exceeded %d hops starting from '%s'", maxDepth, fileID)
		}
		currentID = meta.ShortcutDetails.TargetId
	}
}

// resolveFolderID resolves a folder ID that might be a shortcut, ensuring the result is a folder.
func resolveFolderID(svc *drive.Service, folderID string) (string, error) {
	if folderID == "root" {
		return "root", nil
	}

	resolvedID, meta, err := resolveDriveItem(svc, folderID)
	if err != nil {
		return "", err
	}

	if meta.MimeType != folderMIMEType {
		return "", fmt.Errorf("resolved ID '%s' (from '%s') is not a folder; mimeType=%s", resolvedID, folderID, meta.MimeType)
	}

	return resolvedID, nil
}

// googleNativeExportMIME returns the export MIME type for Google native files.
func googleNativeExportMIME(mimeType string) string {
	switch mimeType {
	case "application/vnd.google-apps.document":
		return "text/plain"
	case "application/vnd.google-apps.spreadsheet":
		return "text/csv"
	case "application/vnd.google-apps.presentation":
		return "text/plain"
	default:
		return ""
	}
}

// resolveExportFormat determines the export MIME type based on file type and requested format.
// Returns (exportMIME, outputMIME).
func resolveExportFormat(mimeType, exportFormat string) (string, string) {
	switch mimeType {
	case "application/vnd.google-apps.document":
		if exportFormat == "docx" {
			m := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			return m, m
		}
		return "application/pdf", "application/pdf"

	case "application/vnd.google-apps.spreadsheet":
		switch exportFormat {
		case "csv":
			return "text/csv", "text/csv"
		case "pdf":
			return "application/pdf", "application/pdf"
		default:
			m := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
			return m, m
		}

	case "application/vnd.google-apps.presentation":
		if exportFormat == "pptx" {
			m := "application/vnd.openxmlformats-officedocument.presentationml.presentation"
			return m, m
		}
		return "application/pdf", "application/pdf"

	default:
		return "", mimeType
	}
}

// tryDecodeUTF8 attempts to decode bytes as UTF-8 text.
func tryDecodeUTF8(data []byte, mimeType string) string {
	// Check if the data is valid UTF-8 text.
	s := string(data)
	for _, r := range s {
		if r == '\uFFFD' && len(data) > 0 {
			return fmt.Sprintf("[Binary or unsupported text encoding for mimeType '%s' - %d bytes]", mimeType, len(data))
		}
	}
	return s
}

// checkPublicLinkPermission checks if any permission is "anyone" with read/write/comment access.
func checkPublicLinkPermission(perms []*drive.Permission) bool {
	for _, p := range perms {
		if p.Type == "anyone" && (p.Role == "reader" || p.Role == "writer" || p.Role == "commenter") {
			return true
		}
	}
	return false
}

// formatPermissionInfo formats a Drive permission for display.
func formatPermissionInfo(p *drive.Permission) string {
	var base string
	switch p.Type {
	case "anyone":
		base = fmt.Sprintf("Anyone with the link (%s) [id: %s]", p.Role, p.Id)
	case "user":
		email := p.EmailAddress
		if email == "" {
			email = "unknown"
		}
		base = fmt.Sprintf("User: %s (%s) [id: %s]", email, p.Role, p.Id)
	case "group":
		email := p.EmailAddress
		if email == "" {
			email = "unknown"
		}
		base = fmt.Sprintf("Group: %s (%s) [id: %s]", email, p.Role, p.Id)
	case "domain":
		domain := p.Domain
		if domain == "" {
			domain = "unknown"
		}
		base = fmt.Sprintf("Domain: %s (%s) [id: %s]", domain, p.Role, p.Id)
	default:
		base = fmt.Sprintf("%s (%s) [id: %s]", p.Type, p.Role, p.Id)
	}

	var extras []string
	if p.ExpirationTime != "" {
		extras = append(extras, "expires: "+p.ExpirationTime)
	}
	for _, detail := range p.PermissionDetails {
		if detail.Inherited && detail.InheritedFrom != "" {
			extras = append(extras, "inherited from: "+detail.InheritedFrom)
			break
		}
	}

	if len(extras) > 0 {
		return base + " | " + strings.Join(extras, ", ")
	}
	return base
}

// detectSourceFormat detects the source MIME type from file name extension.
func detectSourceFormat(fileName, content string) string {
	idx := strings.LastIndex(fileName, ".")
	if idx >= 0 {
		ext := strings.ToLower(fileName[idx:])
		if mime, ok := googleDocsImportFormats[ext]; ok {
			return mime
		}
	}
	// Heuristic: if content looks like markdown, use markdown.
	if content != "" && (strings.HasPrefix(content, "#") || strings.Contains(content, "```") || strings.Contains(content, "**")) {
		return "text/markdown"
	}
	return "text/plain"
}

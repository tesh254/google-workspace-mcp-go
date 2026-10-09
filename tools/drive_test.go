package tools

import (
	"strings"
	"testing"

	drive "google.golang.org/api/drive/v3"
)

// --- isStructuredQuery ---

func TestDriveIsStructuredQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{
			name:  "free text search",
			query: "quarterly report",
			want:  false,
		},
		{
			name:  "empty string",
			query: "",
			want:  false,
		},
		{
			name:  "name equals",
			query: "name = 'myfile.txt'",
			want:  true,
		},
		{
			name:  "name contains",
			query: "name contains 'report'",
			want:  true,
		},
		{
			name:  "mimeType equals",
			query: "mimeType = 'application/pdf'",
			want:  true,
		},
		{
			name:  "mimeType not equals",
			query: "mimeType != 'application/vnd.google-apps.folder'",
			want:  true,
		},
		{
			name:  "fullText contains",
			query: "fullText contains 'budget'",
			want:  true,
		},
		{
			name:  "trashed equals true",
			query: "trashed = true",
			want:  true,
		},
		{
			name:  "trashed equals false",
			query: "trashed = false",
			want:  true,
		},
		{
			name:  "starred equals true",
			query: "starred = true",
			want:  true,
		},
		{
			name:  "in parents",
			query: "'folderId123' in parents",
			want:  true,
		},
		{
			name:  "contains keyword",
			query: "something contains 'test'",
			want:  true,
		},
		{
			name:  "comparison with number",
			query: "size > 1000",
			want:  true,
		},
		{
			name:  "complex compound query",
			query: "name = 'test' and mimeType = 'application/pdf'",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStructuredQuery(tt.query)
			if got != tt.want {
				t.Errorf("isStructuredQuery(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

// --- formatDriveFileList ---

func TestDriveFormatDriveFileList(t *testing.T) {
	tests := []struct {
		name         string
		files        []*drive.File
		header       string
		wantContains []string
		wantExact    string
	}{
		{
			name:      "no files",
			files:     []*drive.File{},
			header:    "Found 0 files:",
			wantExact: "Found 0 files:",
		},
		{
			name: "single file with all fields",
			files: []*drive.File{
				{
					Id:           "file1",
					Name:         "report.pdf",
					MimeType:     "application/pdf",
					Size:         2048,
					ModifiedTime: "2024-01-01T00:00:00Z",
					WebViewLink:  "https://drive.google.com/file/d/file1/view",
				},
			},
			header: "Found 1 files:",
			wantContains: []string{
				"Found 1 files:",
				`Name: "report.pdf"`,
				"ID: file1",
				"Type: application/pdf",
				"Size: 2048",
				"Modified: 2024-01-01T00:00:00Z",
				"Link: https://drive.google.com/file/d/file1/view",
			},
		},
		{
			name: "file with zero size omits size",
			files: []*drive.File{
				{
					Id:           "file2",
					Name:         "doc.gdoc",
					MimeType:     "application/vnd.google-apps.document",
					Size:         0,
					ModifiedTime: "2024-06-15T10:30:00Z",
					WebViewLink:  "https://docs.google.com/document/d/file2",
				},
			},
			header: "Files:",
			wantContains: []string{
				"Type: application/vnd.google-apps.document",
			},
		},
		{
			name: "file with empty modified time shows N/A",
			files: []*drive.File{
				{
					Id:       "file3",
					Name:     "unknown.txt",
					MimeType: "text/plain",
				},
			},
			header: "Results:",
			wantContains: []string{
				"Modified: N/A",
				"Link: #",
			},
		},
		{
			name: "multiple files",
			files: []*drive.File{
				{Id: "f1", Name: "a.txt", MimeType: "text/plain", WebViewLink: "http://link1"},
				{Id: "f2", Name: "b.txt", MimeType: "text/plain", WebViewLink: "http://link2"},
			},
			header: "Found 2 files:",
			wantContains: []string{
				`Name: "a.txt"`,
				`Name: "b.txt"`,
				"ID: f1",
				"ID: f2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDriveFileList(tt.files, tt.header)
			if tt.wantExact != "" {
				if got != tt.wantExact {
					t.Errorf("got %q, want %q", got, tt.wantExact)
				}
				return
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\ngot: %s", want, got)
				}
			}
		})
	}
}

// --- googleNativeExportMIME ---

func TestDriveGoogleNativeExportMIME(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     string
	}{
		{
			name:     "Google Docs",
			mimeType: "application/vnd.google-apps.document",
			want:     "text/plain",
		},
		{
			name:     "Google Sheets",
			mimeType: "application/vnd.google-apps.spreadsheet",
			want:     "text/csv",
		},
		{
			name:     "Google Slides",
			mimeType: "application/vnd.google-apps.presentation",
			want:     "text/plain",
		},
		{
			name:     "regular PDF",
			mimeType: "application/pdf",
			want:     "",
		},
		{
			name:     "plain text",
			mimeType: "text/plain",
			want:     "",
		},
		{
			name:     "empty string",
			mimeType: "",
			want:     "",
		},
		{
			name:     "Google Forms (not exported)",
			mimeType: "application/vnd.google-apps.form",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := googleNativeExportMIME(tt.mimeType)
			if got != tt.want {
				t.Errorf("googleNativeExportMIME(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}

// --- resolveExportFormat ---

func TestDriveResolveExportFormat(t *testing.T) {
	tests := []struct {
		name           string
		mimeType       string
		exportFormat   string
		wantExportMIME string
		wantOutputMIME string
	}{
		// Google Docs
		{
			name:           "docs default export (pdf)",
			mimeType:       "application/vnd.google-apps.document",
			exportFormat:   "",
			wantExportMIME: "application/pdf",
			wantOutputMIME: "application/pdf",
		},
		{
			name:           "docs export as docx",
			mimeType:       "application/vnd.google-apps.document",
			exportFormat:   "docx",
			wantExportMIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			wantOutputMIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		// Google Sheets
		{
			name:           "sheets default export (xlsx)",
			mimeType:       "application/vnd.google-apps.spreadsheet",
			exportFormat:   "",
			wantExportMIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			wantOutputMIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		},
		{
			name:           "sheets export as csv",
			mimeType:       "application/vnd.google-apps.spreadsheet",
			exportFormat:   "csv",
			wantExportMIME: "text/csv",
			wantOutputMIME: "text/csv",
		},
		{
			name:           "sheets export as pdf",
			mimeType:       "application/vnd.google-apps.spreadsheet",
			exportFormat:   "pdf",
			wantExportMIME: "application/pdf",
			wantOutputMIME: "application/pdf",
		},
		// Google Slides
		{
			name:           "slides default export (pdf)",
			mimeType:       "application/vnd.google-apps.presentation",
			exportFormat:   "",
			wantExportMIME: "application/pdf",
			wantOutputMIME: "application/pdf",
		},
		{
			name:           "slides export as pptx",
			mimeType:       "application/vnd.google-apps.presentation",
			exportFormat:   "pptx",
			wantExportMIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			wantOutputMIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		},
		// Non-native files
		{
			name:           "regular PDF passthrough",
			mimeType:       "application/pdf",
			exportFormat:   "",
			wantExportMIME: "",
			wantOutputMIME: "application/pdf",
		},
		{
			name:           "plain text passthrough",
			mimeType:       "text/plain",
			exportFormat:   "",
			wantExportMIME: "",
			wantOutputMIME: "text/plain",
		},
		{
			name:           "unknown MIME passthrough",
			mimeType:       "application/octet-stream",
			exportFormat:   "pdf",
			wantExportMIME: "",
			wantOutputMIME: "application/octet-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotExport, gotOutput := resolveExportFormat(tt.mimeType, tt.exportFormat)
			if gotExport != tt.wantExportMIME {
				t.Errorf("exportMIME = %q, want %q", gotExport, tt.wantExportMIME)
			}
			if gotOutput != tt.wantOutputMIME {
				t.Errorf("outputMIME = %q, want %q", gotOutput, tt.wantOutputMIME)
			}
		})
	}
}

// --- tryDecodeUTF8 ---

func TestDriveTryDecodeUTF8(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		mimeType string
		want     string
	}{
		{
			name:     "valid UTF-8 text",
			data:     []byte("Hello, world!"),
			mimeType: "text/plain",
			want:     "Hello, world!",
		},
		{
			name:     "empty data",
			data:     []byte{},
			mimeType: "text/plain",
			want:     "",
		},
		{
			name:     "unicode text",
			data:     []byte("Héllo wörld"),
			mimeType: "text/plain",
			want:     "Héllo wörld",
		},
		{
			name:     "binary data with replacement character",
			data:     []byte{0xff, 0xfe, 0x00, 0x01},
			mimeType: "application/octet-stream",
			want:     "[Binary or unsupported text encoding for mimeType 'application/octet-stream' - 4 bytes]",
		},
		{
			name:     "CSV content",
			data:     []byte("a,b,c\n1,2,3"),
			mimeType: "text/csv",
			want:     "a,b,c\n1,2,3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tryDecodeUTF8(tt.data, tt.mimeType)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- checkPublicLinkPermission ---

func TestDriveCheckPublicLinkPermission(t *testing.T) {
	tests := []struct {
		name  string
		perms []*drive.Permission
		want  bool
	}{
		{
			name:  "nil permissions",
			perms: nil,
			want:  false,
		},
		{
			name:  "empty permissions",
			perms: []*drive.Permission{},
			want:  false,
		},
		{
			name: "no anyone permission",
			perms: []*drive.Permission{
				{Type: "user", Role: "reader", EmailAddress: "user@example.com"},
			},
			want: false,
		},
		{
			name: "anyone reader",
			perms: []*drive.Permission{
				{Type: "anyone", Role: "reader"},
			},
			want: true,
		},
		{
			name: "anyone writer",
			perms: []*drive.Permission{
				{Type: "anyone", Role: "writer"},
			},
			want: true,
		},
		{
			name: "anyone commenter",
			perms: []*drive.Permission{
				{Type: "anyone", Role: "commenter"},
			},
			want: true,
		},
		{
			name: "anyone with owner role (not matching)",
			perms: []*drive.Permission{
				{Type: "anyone", Role: "owner"},
			},
			want: false,
		},
		{
			name: "mixed permissions with anyone",
			perms: []*drive.Permission{
				{Type: "user", Role: "writer", EmailAddress: "user@example.com"},
				{Type: "anyone", Role: "reader"},
			},
			want: true,
		},
		{
			name: "domain permission not matching",
			perms: []*drive.Permission{
				{Type: "domain", Role: "reader", Domain: "example.com"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkPublicLinkPermission(tt.perms)
			if got != tt.want {
				t.Errorf("checkPublicLinkPermission() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- formatPermissionInfo ---

func TestDriveFormatPermissionInfo(t *testing.T) {
	tests := []struct {
		name         string
		perm         *drive.Permission
		wantContains []string
	}{
		{
			name: "anyone type",
			perm: &drive.Permission{
				Type: "anyone",
				Role: "reader",
				Id:   "anyoneWithLink",
			},
			wantContains: []string{
				"Anyone with the link",
				"reader",
				"id: anyoneWithLink",
			},
		},
		{
			name: "user type with email",
			perm: &drive.Permission{
				Type:         "user",
				Role:         "writer",
				EmailAddress: "user@example.com",
				Id:           "perm1",
			},
			wantContains: []string{
				"User: user@example.com",
				"writer",
				"id: perm1",
			},
		},
		{
			name: "user type without email",
			perm: &drive.Permission{
				Type: "user",
				Role: "reader",
				Id:   "perm2",
			},
			wantContains: []string{
				"User: unknown",
			},
		},
		{
			name: "group type",
			perm: &drive.Permission{
				Type:         "group",
				Role:         "commenter",
				EmailAddress: "group@example.com",
				Id:           "perm3",
			},
			wantContains: []string{
				"Group: group@example.com",
				"commenter",
			},
		},
		{
			name: "group type without email",
			perm: &drive.Permission{
				Type: "group",
				Role: "reader",
				Id:   "perm4",
			},
			wantContains: []string{
				"Group: unknown",
			},
		},
		{
			name: "domain type",
			perm: &drive.Permission{
				Type:   "domain",
				Role:   "reader",
				Domain: "example.com",
				Id:     "perm5",
			},
			wantContains: []string{
				"Domain: example.com",
				"reader",
			},
		},
		{
			name: "domain type without domain",
			perm: &drive.Permission{
				Type: "domain",
				Role: "reader",
				Id:   "perm6",
			},
			wantContains: []string{
				"Domain: unknown",
			},
		},
		{
			name: "unknown type fallback",
			perm: &drive.Permission{
				Type: "custom",
				Role: "reader",
				Id:   "perm7",
			},
			wantContains: []string{
				"custom",
				"reader",
				"id: perm7",
			},
		},
		{
			name: "with expiration time",
			perm: &drive.Permission{
				Type:           "user",
				Role:           "reader",
				EmailAddress:   "user@example.com",
				Id:             "perm8",
				ExpirationTime: "2025-12-31T23:59:59Z",
			},
			wantContains: []string{
				"expires: 2025-12-31T23:59:59Z",
			},
		},
		{
			name: "with inherited permission",
			perm: &drive.Permission{
				Type:         "user",
				Role:         "reader",
				EmailAddress: "user@example.com",
				Id:           "perm9",
				PermissionDetails: []*drive.PermissionPermissionDetails{
					{Inherited: true, InheritedFrom: "parentFolder123"},
				},
			},
			wantContains: []string{
				"inherited from: parentFolder123",
			},
		},
		{
			name: "with non-inherited permission details",
			perm: &drive.Permission{
				Type:         "user",
				Role:         "writer",
				EmailAddress: "user@example.com",
				Id:           "perm10",
				PermissionDetails: []*drive.PermissionPermissionDetails{
					{Inherited: false},
				},
			},
			wantContains: []string{
				"User: user@example.com",
				"writer",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPermissionInfo(tt.perm)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\ngot: %s", want, got)
				}
			}
		})
	}
}

// --- detectSourceFormat ---

func TestDriveDetectSourceFormat(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		content  string
		want     string
	}{
		{
			name:     "markdown extension .md",
			fileName: "readme.md",
			content:  "",
			want:     "text/markdown",
		},
		{
			name:     "markdown extension .markdown",
			fileName: "readme.markdown",
			content:  "",
			want:     "text/markdown",
		},
		{
			name:     "text extension .txt",
			fileName: "notes.txt",
			content:  "",
			want:     "text/plain",
		},
		{
			name:     "html extension .html",
			fileName: "page.html",
			content:  "",
			want:     "text/html",
		},
		{
			name:     "htm extension .htm",
			fileName: "page.htm",
			content:  "",
			want:     "text/html",
		},
		{
			name:     "docx extension",
			fileName: "document.docx",
			content:  "",
			want:     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:     "doc extension",
			fileName: "document.doc",
			content:  "",
			want:     "application/msword",
		},
		{
			name:     "rtf extension",
			fileName: "document.rtf",
			content:  "",
			want:     "application/rtf",
		},
		{
			name:     "odt extension",
			fileName: "document.odt",
			content:  "",
			want:     "application/vnd.oasis.opendocument.text",
		},
		{
			name:     "unknown extension falls back to content heuristic - heading",
			fileName: "document.xyz",
			content:  "# My Document\nSome text",
			want:     "text/markdown",
		},
		{
			name:     "unknown extension falls back to content heuristic - code block",
			fileName: "document.xyz",
			content:  "Some code:\n```go\nfmt.Println()\n```",
			want:     "text/markdown",
		},
		{
			name:     "unknown extension falls back to content heuristic - bold",
			fileName: "document.xyz",
			content:  "This is **bold** text",
			want:     "text/markdown",
		},
		{
			name:     "unknown extension no markdown content falls back to plain",
			fileName: "document.xyz",
			content:  "just some plain text",
			want:     "text/plain",
		},
		{
			name:     "no extension with plain content",
			fileName: "document",
			content:  "hello",
			want:     "text/plain",
		},
		{
			name:     "no extension empty content",
			fileName: "document",
			content:  "",
			want:     "text/plain",
		},
		{
			name:     "case insensitive extension",
			fileName: "README.MD",
			content:  "",
			want:     "text/markdown",
		},
		{
			name:     "text extension .text",
			fileName: "notes.text",
			content:  "",
			want:     "text/plain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectSourceFormat(tt.fileName, tt.content)
			if got != tt.want {
				t.Errorf("detectSourceFormat(%q, %q) = %q, want %q", tt.fileName, tt.content, got, tt.want)
			}
		})
	}
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	slides "google.golang.org/api/slides/v1"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

// mapToStruct converts a map[string]any to a typed struct via JSON round-trip.
func mapToStruct(m map[string]any, out any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// RegisterSlidesTools registers all Slides tools with the MCP server.
func RegisterSlidesTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	registerCreatePresentation(s, getClient)
	registerGetPresentation(s, getClient)
	registerBatchUpdatePresentation(s, getClient)
	registerGetPage(s, getClient)
	registerGetPageThumbnail(s, getClient)

	// Register comment tools for Slides (US-006 / US-019).
	RegisterCommentTools(s, getClient, "presentation", "presentation_id")
}

// newSlidesService creates a slides.Service for the given user email.
func newSlidesService(ctx context.Context, getClient httpClientFunc, email string) (*slides.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := slides.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Slides service: %w", err)
	}
	return svc, nil
}

// --- create_presentation ---

func registerCreatePresentation(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_presentation",
		Description: "Create a new Google Slides presentation.",
	}
	mcp.AddTool(s, tool, handleCreatePresentation(getClient))
}

// createPresentationIn is the input schema for create_presentation.
type createPresentationIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Title           string `json:"title,omitempty" jsonschema:"The title for the new presentation. Defaults to \"Untitled Presentation\"."`
}

func handleCreatePresentation(getClient httpClientFunc) mcp.ToolHandlerFor[createPresentationIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createPresentationIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		title := in.Title
		if title == "" {
			title = "Untitled Presentation"
		}

		svc, err := newSlidesService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		pres := &slides.Presentation{Title: title}
		created, err := svc.Presentations.Create(pres).Do()
		if err != nil {
			return nil, nil, toolErr("creating presentation: %v", err)
		}

		presID := created.PresentationId
		presURL := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presID)
		slideCount := len(created.Slides)

		result := fmt.Sprintf(`Presentation Created Successfully for %s:
- Title: %s
- Presentation ID: %s
- URL: %s
- Slides: %d slide(s) created`, email, title, presID, presURL, slideCount)

		return textResult(result)
	}
}

// --- get_presentation ---

func registerGetPresentation(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_presentation",
		Description: "Get details about a Google Slides presentation.",
	}
	mcp.AddTool(s, tool, handleGetPresentation(getClient))
}

// getPresentationIn is the input schema for get_presentation.
type getPresentationIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PresentationID  string `json:"presentation_id" jsonschema:"The ID of the presentation to retrieve."`
}

func handleGetPresentation(getClient httpClientFunc) mcp.ToolHandlerFor[getPresentationIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getPresentationIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		presID := in.PresentationID

		svc, err := newSlidesService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		pres, err := svc.Presentations.Get(presID).Do()
		if err != nil {
			return nil, nil, toolErr("getting presentation: %v", err)
		}

		title := pres.Title
		if title == "" {
			title = "Untitled"
		}

		// Page size info
		pageSizeStr := "Unknown"
		if pres.PageSize != nil && pres.PageSize.Width != nil && pres.PageSize.Height != nil {
			pageSizeStr = fmt.Sprintf("%v x %v %s",
				pres.PageSize.Width.Magnitude,
				pres.PageSize.Height.Magnitude,
				pres.PageSize.Width.Unit)
		}

		// Build slides breakdown
		var slidesInfo []string
		for i, slide := range pres.Slides {
			slideID := slide.ObjectId
			if slideID == "" {
				slideID = "Unknown"
			}
			elemCount := len(slide.PageElements)

			slideText := extractSlideText(slide)

			if slideText != "" {
				slidesInfo = append(slidesInfo, fmt.Sprintf("  Slide %d: ID %s, %d element(s), text: %s",
					i+1, slideID, elemCount, slideText))
			} else {
				slidesInfo = append(slidesInfo, fmt.Sprintf("  Slide %d: ID %s, %d element(s), text: empty",
					i+1, slideID, elemCount))
			}
		}

		slidesBreakdown := "  No slides found"
		if len(slidesInfo) > 0 {
			slidesBreakdown = strings.Join(slidesInfo, "\n")
		}

		presURL := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presID)

		result := fmt.Sprintf(`Presentation Details for %s:
- Title: %s
- Presentation ID: %s
- URL: %s
- Total Slides: %d
- Page Size: %s

Slides Breakdown:
%s`, email, title, presID, presURL, len(pres.Slides), pageSizeStr, slidesBreakdown)

		return textResult(result)
	}
}

// extractSlideText extracts text content from a slide's page elements.
// It walks shape text elements, sorts by startIndex, and joins text runs.
func extractSlideText(slide *slides.Page) string {
	var textsFromElements []string

	for _, elem := range slide.PageElements {
		if elem.Shape == nil || elem.Shape.Text == nil {
			continue
		}

		type indexedText struct {
			index   int64
			content string
		}
		var textRuns []indexedText

		for _, te := range elem.Shape.Text.TextElements {
			if te.TextRun == nil || te.TextRun.Content == "" {
				continue
			}
			textRuns = append(textRuns, indexedText{
				index:   te.StartIndex,
				content: te.TextRun.Content,
			})
		}

		if len(textRuns) > 0 {
			sort.Slice(textRuns, func(i, j int) bool {
				return textRuns[i].index < textRuns[j].index
			})
			var sb strings.Builder
			for _, tr := range textRuns {
				sb.WriteString(tr.content)
			}
			textsFromElements = append(textsFromElements, sb.String())
		}
	}

	if len(textsFromElements) == 0 {
		return ""
	}

	// Clean up: split on newlines, filter empty, prefix with >
	combined := strings.Join(textsFromElements, "\n")
	lines := strings.Split(combined, "\n")
	var nonEmpty []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, "    > "+line)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	return "\n" + strings.Join(nonEmpty, "\n")
}

// --- batch_update_presentation ---

func registerBatchUpdatePresentation(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "batch_update_presentation",
		Description: "Apply batch updates to a Google Slides presentation.",
	}
	mcp.AddTool(s, tool, handleBatchUpdatePresentation(getClient))
}

// batchUpdatePresentationIn is the input schema for batch_update_presentation.
type batchUpdatePresentationIn struct {
	UserGoogleEmail string           `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PresentationID  string           `json:"presentation_id" jsonschema:"The ID of the presentation to update."`
	Requests        []map[string]any `json:"requests" jsonschema:"List of update requests to apply."`
}

func handleBatchUpdatePresentation(getClient httpClientFunc) mcp.ToolHandlerFor[batchUpdatePresentationIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in batchUpdatePresentationIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		presID := in.PresentationID

		requestsList := in.Requests
		if len(requestsList) == 0 {
			return nil, nil, toolErr("requests is required")
		}

		svc, err := newSlidesService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Convert the raw request maps into slides.Request objects.
		// Since the Slides API request types are complex and varied, we use
		// a JSON round-trip approach: marshal the raw maps to JSON, then
		// unmarshal into the typed slides.Request structs.
		var reqs []*slides.Request
		for _, reqMap := range requestsList {
			req := &slides.Request{}
			if err := mapToStruct(reqMap, req); err != nil {
				return nil, nil, toolErr("parsing request: %v", err)
			}
			reqs = append(reqs, req)
		}

		batchReq := &slides.BatchUpdatePresentationRequest{
			Requests: reqs,
		}

		resp, err := svc.Presentations.BatchUpdate(presID, batchReq).Do()
		if err != nil {
			return nil, nil, toolErr("batch updating presentation: %v", err)
		}

		presURL := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presID)

		var out strings.Builder
		fmt.Fprintf(&out, "Batch Update Completed for %s:\n", email)
		fmt.Fprintf(&out, "- Presentation ID: %s\n", presID)
		fmt.Fprintf(&out, "- URL: %s\n", presURL)
		fmt.Fprintf(&out, "- Requests Applied: %d\n", len(requestsList))
		fmt.Fprintf(&out, "- Replies Received: %d", len(resp.Replies))

		if len(resp.Replies) > 0 {
			fmt.Fprintf(&out, "\n\nUpdate Results:")
			for i, reply := range resp.Replies {
				if reply.CreateSlide != nil {
					objectID := reply.CreateSlide.ObjectId
					if objectID == "" {
						objectID = "Unknown"
					}
					fmt.Fprintf(&out, "\n  Request %d: Created slide with ID %s", i+1, objectID)
				} else if reply.CreateShape != nil {
					objectID := reply.CreateShape.ObjectId
					if objectID == "" {
						objectID = "Unknown"
					}
					fmt.Fprintf(&out, "\n  Request %d: Created shape with ID %s", i+1, objectID)
				} else {
					fmt.Fprintf(&out, "\n  Request %d: Operation completed", i+1)
				}
			}
		}

		return textResult(out.String())
	}
}

// --- get_page ---

func registerGetPage(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_page",
		Description: "Get details about a specific page (slide) in a presentation.",
	}
	mcp.AddTool(s, tool, handleGetPage(getClient))
}

// getPageIn is the input schema for get_page.
type getPageIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PresentationID  string `json:"presentation_id" jsonschema:"The ID of the presentation."`
	PageObjectID    string `json:"page_object_id" jsonschema:"The object ID of the page/slide to retrieve."`
}

func handleGetPage(getClient httpClientFunc) mcp.ToolHandlerFor[getPageIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getPageIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		presID := in.PresentationID
		pageID := in.PageObjectID

		svc, err := newSlidesService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		page, err := svc.Presentations.Pages.Get(presID, pageID).Do()
		if err != nil {
			return nil, nil, toolErr("getting page: %v", err)
		}

		pageType := page.PageType
		if pageType == "" {
			pageType = "Unknown"
		}

		var elementsInfo []string
		for _, elem := range page.PageElements {
			elemID := elem.ObjectId
			if elemID == "" {
				elemID = "Unknown"
			}
			if elem.Shape != nil {
				shapeType := elem.Shape.ShapeType
				if shapeType == "" {
					shapeType = "Unknown"
				}
				elementsInfo = append(elementsInfo, fmt.Sprintf("  Shape: ID %s, Type: %s", elemID, shapeType))
			} else if elem.Table != nil {
				rows := elem.Table.Rows
				cols := elem.Table.Columns
				elementsInfo = append(elementsInfo, fmt.Sprintf("  Table: ID %s, Size: %dx%d", elemID, rows, cols))
			} else if elem.Line != nil {
				lineType := elem.Line.LineType
				if lineType == "" {
					lineType = "Unknown"
				}
				elementsInfo = append(elementsInfo, fmt.Sprintf("  Line: ID %s, Type: %s", elemID, lineType))
			} else {
				elementsInfo = append(elementsInfo, fmt.Sprintf("  Element: ID %s, Type: Unknown", elemID))
			}
		}

		elementsBreakdown := "  No elements found"
		if len(elementsInfo) > 0 {
			elementsBreakdown = strings.Join(elementsInfo, "\n")
		}

		result := fmt.Sprintf(`Page Details for %s:
- Presentation ID: %s
- Page ID: %s
- Page Type: %s
- Total Elements: %d

Page Elements:
%s`, email, presID, pageID, pageType, len(page.PageElements), elementsBreakdown)

		return textResult(result)
	}
}

// --- get_page_thumbnail ---

func registerGetPageThumbnail(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[getPageThumbnailIn](&mcp.Tool{
		Name:        "get_page_thumbnail",
		Description: "Generate a thumbnail URL for a specific page (slide) in a presentation.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "thumbnail_size", "LARGE", "MEDIUM", "SMALL")
	})
	mcp.AddTool(s, tool, handleGetPageThumbnail(getClient))
}

// getPageThumbnailIn is the input schema for get_page_thumbnail.
type getPageThumbnailIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PresentationID  string `json:"presentation_id" jsonschema:"The ID of the presentation."`
	PageObjectID    string `json:"page_object_id" jsonschema:"The object ID of the page/slide."`
	ThumbnailSize   string `json:"thumbnail_size,omitempty" jsonschema:"Size of thumbnail (\"LARGE\", \"MEDIUM\", \"SMALL\"). Defaults to \"MEDIUM\"."`
}

func handleGetPageThumbnail(getClient httpClientFunc) mcp.ToolHandlerFor[getPageThumbnailIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getPageThumbnailIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		presID := in.PresentationID
		pageID := in.PageObjectID
		thumbnailSize := in.ThumbnailSize
		if thumbnailSize == "" {
			thumbnailSize = "MEDIUM"
		}

		svc, err := newSlidesService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		thumbnail, err := svc.Presentations.Pages.GetThumbnail(presID, pageID).
			ThumbnailPropertiesThumbnailSize(thumbnailSize).
			ThumbnailPropertiesMimeType("PNG").
			Do()
		if err != nil {
			return nil, nil, toolErr("getting page thumbnail: %v", err)
		}

		thumbnailURL := thumbnail.ContentUrl

		result := fmt.Sprintf(`Thumbnail Generated for %s:
- Presentation ID: %s
- Page ID: %s
- Thumbnail Size: %s
- Thumbnail URL: %s

You can view or download the thumbnail using the provided URL.`, email, presID, pageID, thumbnailSize, thumbnailURL)

		return textResult(result)
	}
}

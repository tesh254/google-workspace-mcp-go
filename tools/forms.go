package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	forms "google.golang.org/api/forms/v1"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

// RegisterFormsTools registers all Forms tools with the MCP server.
func RegisterFormsTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	registerCreateForm(s, getClient)
	registerGetForm(s, getClient)
	registerSetPublishSettings(s, getClient)
	registerGetFormResponse(s, getClient)
	registerListFormResponses(s, getClient)
	registerBatchUpdateForm(s, getClient)
}

// newFormsService creates a forms.Service for the given user email.
func newFormsService(ctx context.Context, getClient httpClientFunc, email string) (*forms.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := forms.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Forms service: %w", err)
	}
	return svc, nil
}

// --- create_form ---

// createFormIn is the input schema for create_form.
type createFormIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Title           string `json:"title" jsonschema:"The title of the form."`
	Description     string `json:"description,omitempty" jsonschema:"The description of the form."`
	DocumentTitle   string `json:"document_title,omitempty" jsonschema:"The document title (shown in browser tab)."`
}

func registerCreateForm(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_form",
		Description: "Create a new Google Form with the specified title and optional description.",
	}
	mcp.AddTool(s, tool, handleCreateForm(getClient))
}

func handleCreateForm(getClient httpClientFunc) mcp.ToolHandlerFor[createFormIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createFormIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		title := in.Title

		description := in.Description
		documentTitle := in.DocumentTitle

		svc, err := newFormsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		formBody := &forms.Form{
			Info: &forms.Info{
				Title: title,
			},
		}

		if description != "" {
			formBody.Info.Description = description
		}

		if documentTitle != "" {
			formBody.Info.DocumentTitle = documentTitle
		}

		created, err := svc.Forms.Create(formBody).Do()
		if err != nil {
			return nil, nil, toolErr("creating form: %v", err)
		}

		formID := created.FormId
		editURL := fmt.Sprintf("https://docs.google.com/forms/d/%s/edit", formID)
		responderURL := created.ResponderUri
		if responderURL == "" {
			responderURL = fmt.Sprintf("https://docs.google.com/forms/d/%s/viewform", formID)
		}

		createdTitle := title
		if created.Info != nil && created.Info.Title != "" {
			createdTitle = created.Info.Title
		}

		result := fmt.Sprintf("Successfully created form '%s' for %s. Form ID: %s. Edit URL: %s. Responder URL: %s",
			createdTitle, email, formID, editURL, responderURL)

		return textResult(result)
	}
}

// --- get_form ---

// getFormIn is the input schema for get_form.
type getFormIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	FormID          string `json:"form_id" jsonschema:"The ID of the form to retrieve."`
}

func registerGetForm(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_form",
		Description: "Get a Google Form's details including title, description, questions, and URLs.",
	}
	mcp.AddTool(s, tool, handleGetForm(getClient))
}

func handleGetForm(getClient httpClientFunc) mcp.ToolHandlerFor[getFormIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getFormIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		formID := in.FormID

		svc, err := newFormsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		form, err := svc.Forms.Get(formID).Do()
		if err != nil {
			return nil, nil, toolErr("getting form: %v", err)
		}

		title := "No Title"
		description := "No Description"
		documentTitle := title
		if form.Info != nil {
			if form.Info.Title != "" {
				title = form.Info.Title
			}
			if form.Info.Description != "" {
				description = form.Info.Description
			}
			if form.Info.DocumentTitle != "" {
				documentTitle = form.Info.DocumentTitle
			} else {
				documentTitle = title
			}
		}

		editURL := fmt.Sprintf("https://docs.google.com/forms/d/%s/edit", formID)
		responderURL := form.ResponderUri
		if responderURL == "" {
			responderURL = fmt.Sprintf("https://docs.google.com/forms/d/%s/viewform", formID)
		}

		items := form.Items
		var questionsSummary []string
		for i, item := range items {
			itemTitle := item.Title
			if itemTitle == "" {
				itemTitle = fmt.Sprintf("Question %d", i+1)
			}
			requiredText := ""
			if item.QuestionItem != nil && item.QuestionItem.Question != nil && item.QuestionItem.Question.Required {
				requiredText = " (Required)"
			}
			questionsSummary = append(questionsSummary, fmt.Sprintf("  %d. %s%s", i+1, itemTitle, requiredText))
		}

		questionsText := "  No questions found"
		if len(questionsSummary) > 0 {
			questionsText = strings.Join(questionsSummary, "\n")
		}

		result := fmt.Sprintf(`Form Details for %s:
- Title: "%s"
- Description: "%s"
- Document Title: "%s"
- Form ID: %s
- Edit URL: %s
- Responder URL: %s
- Questions (%d total):
%s`, email, title, description, documentTitle, formID, editURL, responderURL, len(items), questionsText)

		return textResult(result)
	}
}

// --- set_publish_settings ---

// setPublishSettingsIn is the input schema for set_publish_settings.
type setPublishSettingsIn struct {
	UserGoogleEmail       string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	FormID                string `json:"form_id" jsonschema:"The ID of the form to update publish settings for."`
	PublishAsTemplate     bool   `json:"publish_as_template,omitempty" jsonschema:"Whether to publish as a template. Defaults to false."`
	RequireAuthentication bool   `json:"require_authentication,omitempty" jsonschema:"Whether to require authentication to view/submit. Defaults to false."`
}

func registerSetPublishSettings(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "set_publish_settings",
		Description: "Updates the publish settings of a Google Form.",
	}
	mcp.AddTool(s, tool, handleSetPublishSettings(getClient))
}

func handleSetPublishSettings(getClient httpClientFunc) mcp.ToolHandlerFor[setPublishSettingsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in setPublishSettingsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		formID := in.FormID

		publishAsTemplate := in.PublishAsTemplate
		requireAuthentication := in.RequireAuthentication

		// The setPublishSettings endpoint is not in the generated Go Forms client library.
		// We make a raw HTTP call to the REST API.
		httpClient, err := getClient(ctx, email)
		if err != nil {
			return nil, nil, toolErr("authenticating for %s: %v", email, err)
		}

		settingsBody := map[string]bool{
			"publishAsTemplate":     publishAsTemplate,
			"requireAuthentication": requireAuthentication,
		}
		bodyJSON, err := json.Marshal(settingsBody)
		if err != nil {
			return nil, nil, toolErr("marshaling request body: %v", err)
		}

		url := fmt.Sprintf("https://forms.googleapis.com/v1/forms/%s:setPublishSettings", formID)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, nil, toolErr("creating request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, nil, toolErr("setting publish settings: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return nil, nil, toolErr("setting publish settings (HTTP %d): %s", resp.StatusCode, string(respBody))
		}

		result := fmt.Sprintf("Successfully updated publish settings for form %s for %s. Publish as template: %t, Require authentication: %t",
			formID, email, publishAsTemplate, requireAuthentication)

		return textResult(result)
	}
}

// --- get_form_response ---

// getFormResponseIn is the input schema for get_form_response.
type getFormResponseIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	FormID          string `json:"form_id" jsonschema:"The ID of the form."`
	ResponseID      string `json:"response_id" jsonschema:"The ID of the response to retrieve."`
}

func registerGetFormResponse(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_form_response",
		Description: "Get a single response from a Google Form by response ID.",
	}
	mcp.AddTool(s, tool, handleGetFormResponse(getClient))
}

func handleGetFormResponse(getClient httpClientFunc) mcp.ToolHandlerFor[getFormResponseIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getFormResponseIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		formID := in.FormID
		responseID := in.ResponseID

		svc, err := newFormsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		formResp, err := svc.Forms.Responses.Get(formID, responseID).Do()
		if err != nil {
			return nil, nil, toolErr("getting form response: %v", err)
		}

		respID := formResp.ResponseId
		if respID == "" {
			respID = "Unknown"
		}
		createTime := formResp.CreateTime
		if createTime == "" {
			createTime = "Unknown"
		}
		lastSubmittedTime := formResp.LastSubmittedTime
		if lastSubmittedTime == "" {
			lastSubmittedTime = "Unknown"
		}

		var answerDetails []string
		for questionID, answer := range formResp.Answers {
			if answer.TextAnswers != nil && len(answer.TextAnswers.Answers) > 0 {
				var vals []string
				for _, ans := range answer.TextAnswers.Answers {
					vals = append(vals, ans.Value)
				}
				answerDetails = append(answerDetails, fmt.Sprintf("  Question ID %s: %s", questionID, strings.Join(vals, ", ")))
			} else {
				answerDetails = append(answerDetails, fmt.Sprintf("  Question ID %s: No answer provided", questionID))
			}
		}

		answersText := "  No answers found"
		if len(answerDetails) > 0 {
			answersText = strings.Join(answerDetails, "\n")
		}

		result := fmt.Sprintf(`Form Response Details for %s:
- Form ID: %s
- Response ID: %s
- Created: %s
- Last Submitted: %s
- Answers:
%s`, email, formID, respID, createTime, lastSubmittedTime, answersText)

		return textResult(result)
	}
}

// --- list_form_responses ---

// listFormResponsesIn is the input schema for list_form_responses.
type listFormResponsesIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	FormID          string `json:"form_id" jsonschema:"The ID of the form."`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Maximum number of responses to return. Defaults to 10."`
	PageToken       string `json:"page_token,omitempty" jsonschema:"Token for retrieving next page of results."`
}

func registerListFormResponses(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_form_responses",
		Description: "List responses for a Google Form with pagination support.",
	}
	mcp.AddTool(s, tool, handleListFormResponses(getClient))
}

func handleListFormResponses(getClient httpClientFunc) mcp.ToolHandlerFor[listFormResponsesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listFormResponsesIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		formID := in.FormID

		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 10
		}
		pageToken := in.PageToken

		svc, err := newFormsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		call := svc.Forms.Responses.List(formID).PageSize(int64(pageSize))
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("listing form responses: %v", err)
		}

		responses := resp.Responses
		if len(responses) == 0 {
			return textResult(fmt.Sprintf("No responses found for form %s for %s.", formID, email))
		}

		var responseDetails []string
		for i, r := range responses {
			respID := r.ResponseId
			if respID == "" {
				respID = "Unknown"
			}
			createTime := r.CreateTime
			if createTime == "" {
				createTime = "Unknown"
			}
			lastSubmittedTime := r.LastSubmittedTime
			if lastSubmittedTime == "" {
				lastSubmittedTime = "Unknown"
			}
			answersCount := len(r.Answers)
			responseDetails = append(responseDetails, fmt.Sprintf("  %d. Response ID: %s | Created: %s | Last Submitted: %s | Answers: %d",
				i+1, respID, createTime, lastSubmittedTime, answersCount))
		}

		paginationInfo := "\nNo more pages."
		if resp.NextPageToken != "" {
			paginationInfo = fmt.Sprintf("\nNext page token: %s", resp.NextPageToken)
		}

		result := fmt.Sprintf(`Form Responses for %s:
- Form ID: %s
- Total responses returned: %d
- Responses:
%s%s`, email, formID, len(responses), strings.Join(responseDetails, "\n"), paginationInfo)

		return textResult(result)
	}
}

// --- batch_update_form ---

// batchUpdateFormIn is the input schema for batch_update_form.
type batchUpdateFormIn struct {
	UserGoogleEmail string           `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	FormID          string           `json:"form_id" jsonschema:"The ID of the form to update."`
	Requests        []map[string]any `json:"requests" jsonschema:"List of update requests to apply. Supported types: createItem, updateItem, deleteItem, moveItem, updateFormInfo, updateSettings."`
}

func registerBatchUpdateForm(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "batch_update_form",
		Description: "Apply batch updates to a Google Form. Supports adding, updating, and deleting form items, as well as updating form metadata and settings.",
	}
	mcp.AddTool(s, tool, handleBatchUpdateForm(getClient))
}

func handleBatchUpdateForm(getClient httpClientFunc) mcp.ToolHandlerFor[batchUpdateFormIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in batchUpdateFormIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		formID := in.FormID

		// Extract raw requests array — we pass these as raw JSON to the API
		// since the request format is complex and varies by operation type.
		requestsList := in.Requests
		if requestsList == nil {
			return nil, nil, toolErr("requests is required")
		}

		// Use raw HTTP to send the batch update, since the typed Go structs
		// would require converting each request map into the proper struct.
		httpClient, err := getClient(ctx, email)
		if err != nil {
			return nil, nil, toolErr("authenticating for %s: %v", email, err)
		}

		body := map[string]any{
			"requests": requestsList,
		}
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			return nil, nil, toolErr("marshaling request body: %v", err)
		}

		url := fmt.Sprintf("https://forms.googleapis.com/v1/forms/%s:batchUpdate", formID)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, nil, toolErr("creating request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, nil, toolErr("batch updating form: %v", err)
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, toolErr("reading response: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, nil, toolErr("batch updating form (HTTP %d): %s", resp.StatusCode, string(respBody))
		}

		// Parse the response to extract reply details
		var batchResp struct {
			Replies []map[string]any `json:"replies"`
		}
		if err := json.Unmarshal(respBody, &batchResp); err != nil {
			return nil, nil, toolErr("parsing response: %v", err)
		}

		editURL := fmt.Sprintf("https://docs.google.com/forms/d/%s/edit", formID)

		var out strings.Builder
		fmt.Fprintf(&out, "Batch Update Completed:\n")
		fmt.Fprintf(&out, "- Form ID: %s\n", formID)
		fmt.Fprintf(&out, "- URL: %s\n", editURL)
		fmt.Fprintf(&out, "- Requests Applied: %d\n", len(requestsList))
		fmt.Fprintf(&out, "- Replies Received: %d", len(batchResp.Replies))

		if len(batchResp.Replies) > 0 {
			fmt.Fprintf(&out, "\n\nUpdate Results:")
			for i, reply := range batchResp.Replies {
				if createItem, ok := reply["createItem"]; ok {
					createMap, _ := createItem.(map[string]any)
					itemID := "Unknown"
					if id, ok := createMap["itemId"]; ok {
						itemID = fmt.Sprintf("%v", id)
					}
					questionInfo := ""
					if qIDs, ok := createMap["questionId"]; ok {
						if qList, ok := qIDs.([]any); ok && len(qList) > 0 {
							var ids []string
							for _, q := range qList {
								ids = append(ids, fmt.Sprintf("%v", q))
							}
							questionInfo = fmt.Sprintf(" (Question IDs: %s)", strings.Join(ids, ", "))
						}
					}
					fmt.Fprintf(&out, "\n  Request %d: Created item %s%s", i+1, itemID, questionInfo)
				} else {
					fmt.Fprintf(&out, "\n  Request %d: Operation completed", i+1)
				}
			}
		}

		return textResult(out.String())
	}
}

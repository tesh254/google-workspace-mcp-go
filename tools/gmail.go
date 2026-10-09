package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	gmail "google.golang.org/api/gmail/v1"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

// gmailMetadataHeaders are the headers to request in metadata-only fetches.
var gmailMetadataHeaders = []string{"Subject", "From", "To", "Cc", "Date", "Message-ID"}

// gmailBatchSize is the max messages/threads per batch request.
const gmailBatchSize = 25

// RegisterGmailTools registers all Gmail tools with the MCP server.
func RegisterGmailTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	// Read tools
	registerSearchGmailMessages(s, getClient)
	registerGetGmailMessageContent(s, getClient)
	registerGetGmailMessagesContentBatch(s, getClient)
	registerGetGmailAttachmentContent(s, getClient)
	registerGetGmailThreadContent(s, getClient)
	registerGetGmailThreadsContentBatch(s, getClient)
	registerListGmailLabels(s, getClient)

	// Write tools
	registerSendGmailMessage(s, getClient)
	registerDraftGmailMessage(s, getClient)
	registerManageGmailLabel(s, getClient)
	registerListGmailFilters(s, getClient)
	registerCreateGmailFilter(s, getClient)
	registerDeleteGmailFilter(s, getClient)
	registerModifyGmailMessageLabels(s, getClient)
	registerBatchModifyGmailMessageLabels(s, getClient)
}

// newGmailService creates a gmail.Service for the given user email.
func newGmailService(ctx context.Context, getClient httpClientFunc, email string) (*gmail.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := gmail.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Gmail service: %w", err)
	}
	return svc, nil
}

// --- search_gmail_messages ---

// searchGmailMessagesIn is the input schema for search_gmail_messages.
type searchGmailMessagesIn struct {
	Query           string `json:"query" jsonschema:"The search query. Supports standard Gmail search operators."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"The maximum number of messages to return. Defaults to 10."`
	PageToken       string `json:"page_token,omitempty" jsonschema:"Token for retrieving the next page of results. Use the next_page_token from a previous response."`
}

func registerSearchGmailMessages(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "search_gmail_messages",
		Description: "Searches messages in a user's Gmail account based on a query. Returns both Message IDs and Thread IDs for each found message, along with Gmail web interface links for manual verification. Supports pagination via page_token parameter.",
	}
	mcp.AddTool(s, tool, handleSearchGmailMessages(getClient))
}

func handleSearchGmailMessages(getClient httpClientFunc) mcp.ToolHandlerFor[searchGmailMessagesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in searchGmailMessagesIn) (*mcp.CallToolResult, any, error) {
		query := in.Query
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 10
		}
		pageToken := in.PageToken

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		call := svc.Users.Messages.List("me").Q(query).MaxResults(int64(pageSize))
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		messages := resp.Messages
		if messages == nil {
			messages = []*gmail.Message{}
		}

		return textResult(formatSearchResults(messages, query, resp.NextPageToken))
	}
}

func formatSearchResults(messages []*gmail.Message, query, nextPageToken string) string {
	if len(messages) == 0 {
		return fmt.Sprintf("No messages found for query: '%s'", query)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d messages for query: '%s'\n\n", len(messages), query)

	for i, msg := range messages {
		fmt.Fprintf(&b, "%d. Message ID: %s\n", i+1, msg.Id)
		fmt.Fprintf(&b, "   Thread ID: %s\n", msg.ThreadId)
		fmt.Fprintf(&b, "   Web Link: https://mail.google.com/mail/u/0/#inbox/%s\n", msg.Id)
		if i < len(messages)-1 {
			b.WriteString("\n")
		}
	}

	if nextPageToken != "" {
		fmt.Fprintf(&b, "\n\nMore results available. Use page_token: '%s' to get the next page.", nextPageToken)
	}

	return b.String()
}

// --- get_gmail_message_content ---

// getGmailMessageContentIn is the input schema for get_gmail_message_content.
type getGmailMessageContentIn struct {
	MessageID       string `json:"message_id" jsonschema:"The unique ID of the Gmail message to retrieve."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
}

func registerGetGmailMessageContent(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_gmail_message_content",
		Description: "Retrieves the full content (subject, sender, recipients, plain text body) of a specific Gmail message.",
	}
	mcp.AddTool(s, tool, handleGetGmailMessageContent(getClient))
}

func handleGetGmailMessageContent(getClient httpClientFunc) mcp.ToolHandlerFor[getGmailMessageContentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getGmailMessageContentIn) (*mcp.CallToolResult, any, error) {
		messageID := in.MessageID
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Fetch metadata headers.
		metaMsg, err := svc.Users.Messages.Get("me", messageID).Format("metadata").MetadataHeaders(gmailMetadataHeaders...).Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}
		headers := extractHeaders(metaMsg.Payload, gmailMetadataHeaders)

		// Fetch full message for body.
		fullMsg, err := svc.Users.Messages.Get("me", messageID).Format("full").Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		textBody, htmlBody := extractMessageBodies(fullMsg.Payload)
		bodyData := formatBodyContent(textBody, htmlBody)
		attachments := extractAttachments(fullMsg.Payload)

		return textResult(formatMessageContent(messageID, headers, bodyData, attachments))
	}
}

func formatMessageContent(messageID string, headers map[string]string, bodyData string, attachments []attachmentMeta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Subject: %s\n", headerOrDefault(headers, "Subject", "(no subject)"))
	fmt.Fprintf(&b, "From:    %s\n", headerOrDefault(headers, "From", "(unknown sender)"))
	fmt.Fprintf(&b, "Date:    %s\n", headerOrDefault(headers, "Date", "(unknown date)"))

	if msgID := headers["Message-ID"]; msgID != "" {
		fmt.Fprintf(&b, "Message-ID: %s\n", msgID)
	}
	if to := headers["To"]; to != "" {
		fmt.Fprintf(&b, "To:      %s\n", to)
	}
	if cc := headers["Cc"]; cc != "" {
		fmt.Fprintf(&b, "Cc:      %s\n", cc)
	}

	if bodyData == "" {
		bodyData = "[No text/plain body found]"
	}
	fmt.Fprintf(&b, "\n--- BODY ---\n%s", bodyData)

	if len(attachments) > 0 {
		b.WriteString("\n\n--- ATTACHMENTS ---\n")
		for i, att := range attachments {
			sizeKB := float64(att.size) / 1024
			fmt.Fprintf(&b, "%d. %s (%s, %.1f KB)\n", i+1, att.filename, att.mimeType, sizeKB)
			fmt.Fprintf(&b, "   Attachment ID: %s\n", att.attachmentID)
			fmt.Fprintf(&b, "   Use get_gmail_attachment_content(message_id='%s', attachment_id='%s') to download\n", messageID, att.attachmentID)
		}
	}

	return b.String()
}

// --- get_gmail_messages_content_batch ---

// getGmailMessagesContentBatchIn is the input schema for get_gmail_messages_content_batch.
type getGmailMessagesContentBatchIn struct {
	MessageIDs      []string `json:"message_ids" jsonschema:"List of Gmail message IDs to retrieve (max 25 per batch)."`
	UserGoogleEmail string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	Format          string   `json:"format,omitempty" jsonschema:"Message format. \"full\" includes body, \"metadata\" only headers."`
}

func registerGetGmailMessagesContentBatch(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[getGmailMessagesContentBatchIn](&mcp.Tool{
		Name:        "get_gmail_messages_content_batch",
		Description: "Retrieves the content of multiple Gmail messages in a single batch request. Supports up to 25 messages per batch to prevent SSL connection exhaustion.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "format", "full", "metadata")
	})
	mcp.AddTool(s, tool, handleGetGmailMessagesContentBatch(getClient))
}

func handleGetGmailMessagesContentBatch(getClient httpClientFunc) mcp.ToolHandlerFor[getGmailMessagesContentBatchIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getGmailMessagesContentBatchIn) (*mcp.CallToolResult, any, error) {
		messageIDs := in.MessageIDs
		if len(messageIDs) == 0 {
			return nil, nil, toolErr("No message IDs provided")
		}
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		format := in.Format
		if format == "" {
			format = "full"
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		var outputMessages []string

		// Process in chunks of gmailBatchSize.
		for chunkStart := 0; chunkStart < len(messageIDs); chunkStart += gmailBatchSize {
			chunkEnd := chunkStart + gmailBatchSize
			if chunkEnd > len(messageIDs) {
				chunkEnd = len(messageIDs)
			}
			chunk := messageIDs[chunkStart:chunkEnd]

			for _, mid := range chunk {
				var msg *gmail.Message
				var fetchErr error

				if format == "metadata" {
					msg, fetchErr = svc.Users.Messages.Get("me", mid).Format("metadata").MetadataHeaders(gmailMetadataHeaders...).Do()
				} else {
					msg, fetchErr = svc.Users.Messages.Get("me", mid).Format("full").Do()
				}

				if fetchErr != nil {
					outputMessages = append(outputMessages, fmt.Sprintf("Message %s: %v", mid, fetchErr))
					continue
				}

				outputMessages = append(outputMessages, formatBatchMessage(mid, msg, format))
			}
		}

		result := fmt.Sprintf("Retrieved %d messages:\n\n%s", len(messageIDs), strings.Join(outputMessages, "\n---\n\n"))
		return textResult(result)
	}
}

func formatBatchMessage(mid string, msg *gmail.Message, format string) string {
	headers := extractHeaders(msg.Payload, gmailMetadataHeaders)

	var b strings.Builder
	fmt.Fprintf(&b, "Message ID: %s\n", mid)
	fmt.Fprintf(&b, "Subject: %s\n", headerOrDefault(headers, "Subject", "(no subject)"))
	fmt.Fprintf(&b, "From: %s\n", headerOrDefault(headers, "From", "(unknown sender)"))
	fmt.Fprintf(&b, "Date: %s\n", headerOrDefault(headers, "Date", "(unknown date)"))

	if msgID := headers["Message-ID"]; msgID != "" {
		fmt.Fprintf(&b, "Message-ID: %s\n", msgID)
	}
	if to := headers["To"]; to != "" {
		fmt.Fprintf(&b, "To: %s\n", to)
	}
	if cc := headers["Cc"]; cc != "" {
		fmt.Fprintf(&b, "Cc: %s\n", cc)
	}

	fmt.Fprintf(&b, "Web Link: https://mail.google.com/mail/u/0/#inbox/%s\n", mid)

	if format == "full" {
		textBody, htmlBody := extractMessageBodies(msg.Payload)
		bodyData := formatBodyContent(textBody, htmlBody)
		if bodyData != "" {
			fmt.Fprintf(&b, "\n%s\n", bodyData)
		}
	}

	return b.String()
}

// --- get_gmail_attachment_content ---

// getGmailAttachmentContentIn is the input schema for get_gmail_attachment_content.
type getGmailAttachmentContentIn struct {
	MessageID       string `json:"message_id" jsonschema:"The ID of the Gmail message containing the attachment."`
	AttachmentID    string `json:"attachment_id" jsonschema:"The ID of the attachment to download."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
}

func registerGetGmailAttachmentContent(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_gmail_attachment_content",
		Description: "Downloads the content of a specific email attachment.",
	}
	mcp.AddTool(s, tool, handleGetGmailAttachmentContent(getClient))
}

func handleGetGmailAttachmentContent(getClient httpClientFunc) mcp.ToolHandlerFor[getGmailAttachmentContentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getGmailAttachmentContentIn) (*mcp.CallToolResult, any, error) {
		messageID := in.MessageID
		attachmentID := in.AttachmentID
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		att, err := svc.Users.Messages.Attachments.Get("me", messageID, attachmentID).Do()
		if err != nil {
			return nil, nil, toolErr(
				"Error: Failed to download attachment. The attachment ID may have changed.\n"+
					"Please fetch the message content again to get an updated attachment ID.\n\n"+
					"Error details: %v", err)
		}

		sizeBytes := att.Size
		sizeKB := float64(sizeBytes) / 1024
		data := att.Data

		var b strings.Builder
		b.WriteString("Attachment downloaded successfully!\n")
		fmt.Fprintf(&b, "Message ID: %s\n", messageID)
		fmt.Fprintf(&b, "Size: %.1f KB (%d bytes)\n", sizeKB, sizeBytes)
		b.WriteString("\nBase64-encoded content (first 100 characters shown):\n")
		preview := data
		if len(preview) > 100 {
			preview = preview[:100]
		}
		fmt.Fprintf(&b, "%s...\n", preview)
		b.WriteString("\nNote: Attachment IDs are ephemeral. Always use IDs from the most recent message fetch.")

		return textResult(b.String())
	}
}

// --- get_gmail_thread_content ---

// getGmailThreadContentIn is the input schema for get_gmail_thread_content.
type getGmailThreadContentIn struct {
	ThreadID        string `json:"thread_id" jsonschema:"The unique ID of the Gmail thread to retrieve."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
}

func registerGetGmailThreadContent(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_gmail_thread_content",
		Description: "Retrieves the complete content of a Gmail conversation thread, including all messages.",
	}
	mcp.AddTool(s, tool, handleGetGmailThreadContent(getClient))
}

func handleGetGmailThreadContent(getClient httpClientFunc) mcp.ToolHandlerFor[getGmailThreadContentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getGmailThreadContentIn) (*mcp.CallToolResult, any, error) {
		threadID := in.ThreadID
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		thread, err := svc.Users.Threads.Get("me", threadID).Format("full").Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		return textResult(formatThreadContent(thread, threadID))
	}
}

func formatThreadContent(thread *gmail.Thread, threadID string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Thread ID: %s\n", threadID)
	fmt.Fprintf(&b, "Messages in thread: %d\n\n", len(thread.Messages))

	for i, msg := range thread.Messages {
		headers := extractHeaders(msg.Payload, gmailMetadataHeaders)
		fmt.Fprintf(&b, "--- Message %d of %d ---\n", i+1, len(thread.Messages))
		fmt.Fprintf(&b, "Message ID: %s\n", msg.Id)
		fmt.Fprintf(&b, "Subject: %s\n", headerOrDefault(headers, "Subject", "(no subject)"))
		fmt.Fprintf(&b, "From: %s\n", headerOrDefault(headers, "From", "(unknown sender)"))
		fmt.Fprintf(&b, "Date: %s\n", headerOrDefault(headers, "Date", "(unknown date)"))
		if to := headers["To"]; to != "" {
			fmt.Fprintf(&b, "To: %s\n", to)
		}
		if cc := headers["Cc"]; cc != "" {
			fmt.Fprintf(&b, "Cc: %s\n", cc)
		}

		textBody, htmlBody := extractMessageBodies(msg.Payload)
		bodyData := formatBodyContent(textBody, htmlBody)
		if bodyData != "" {
			fmt.Fprintf(&b, "\n%s\n", bodyData)
		} else {
			b.WriteString("\n[No body content]\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// --- get_gmail_threads_content_batch ---

// getGmailThreadsContentBatchIn is the input schema for get_gmail_threads_content_batch.
type getGmailThreadsContentBatchIn struct {
	ThreadIDs       []string `json:"thread_ids" jsonschema:"A list of Gmail thread IDs to retrieve. The function will automatically batch requests in chunks of 25."`
	UserGoogleEmail string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
}

func registerGetGmailThreadsContentBatch(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_gmail_threads_content_batch",
		Description: "Retrieves the content of multiple Gmail threads in a single batch request. Supports up to 25 threads per batch to prevent SSL connection exhaustion.",
	}
	mcp.AddTool(s, tool, handleGetGmailThreadsContentBatch(getClient))
}

func handleGetGmailThreadsContentBatch(getClient httpClientFunc) mcp.ToolHandlerFor[getGmailThreadsContentBatchIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getGmailThreadsContentBatchIn) (*mcp.CallToolResult, any, error) {
		threadIDs := in.ThreadIDs
		if len(threadIDs) == 0 {
			return nil, nil, toolErr("No thread IDs provided")
		}
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		var outputThreads []string

		for chunkStart := 0; chunkStart < len(threadIDs); chunkStart += gmailBatchSize {
			chunkEnd := chunkStart + gmailBatchSize
			if chunkEnd > len(threadIDs) {
				chunkEnd = len(threadIDs)
			}
			chunk := threadIDs[chunkStart:chunkEnd]

			for _, tid := range chunk {
				thread, fetchErr := svc.Users.Threads.Get("me", tid).Format("full").Do()
				if fetchErr != nil {
					outputThreads = append(outputThreads, fmt.Sprintf("Thread %s: %v", tid, fetchErr))
					continue
				}
				outputThreads = append(outputThreads, formatThreadContent(thread, tid))
			}
		}

		result := fmt.Sprintf("Retrieved %d threads:\n\n%s", len(threadIDs), strings.Join(outputThreads, "\n---\n\n"))
		return textResult(result)
	}
}

// --- list_gmail_labels ---

// listGmailLabelsIn is the input schema for list_gmail_labels.
type listGmailLabelsIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
}

func registerListGmailLabels(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_gmail_labels",
		Description: "Lists all labels in the user's Gmail account.",
	}
	mcp.AddTool(s, tool, handleListGmailLabels(getClient))
}

func handleListGmailLabels(getClient httpClientFunc) mcp.ToolHandlerFor[listGmailLabelsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listGmailLabelsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resp, err := svc.Users.Labels.List("me").Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		labels := resp.Labels
		if len(labels) == 0 {
			return textResult("No labels found.")
		}

		var systemLabels, userLabels []*gmail.Label
		for _, l := range labels {
			if l.Type == "system" {
				systemLabels = append(systemLabels, l)
			} else {
				userLabels = append(userLabels, l)
			}
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Found %d labels:\n", len(labels))

		if len(systemLabels) > 0 {
			b.WriteString("\nSYSTEM LABELS:\n")
			for _, l := range systemLabels {
				fmt.Fprintf(&b, "  - %s (ID: %s)\n", l.Name, l.Id)
			}
		}

		if len(userLabels) > 0 {
			b.WriteString("\nUSER LABELS:\n")
			for _, l := range userLabels {
				fmt.Fprintf(&b, "  - %s (ID: %s)\n", l.Name, l.Id)
			}
		}

		return textResult(b.String())
	}
}

// --- send_gmail_message ---

// sendGmailMessageIn is the input schema for send_gmail_message.
type sendGmailMessageIn struct {
	UserGoogleEmail string           `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	To              string           `json:"to" jsonschema:"Recipient email address."`
	Subject         string           `json:"subject" jsonschema:"Email subject."`
	Body            string           `json:"body" jsonschema:"Email body content (plain text or HTML)."`
	BodyFormat      string           `json:"body_format,omitempty" jsonschema:"Email body format."`
	Cc              string           `json:"cc,omitempty" jsonschema:"CC email address."`
	Bcc             string           `json:"bcc,omitempty" jsonschema:"BCC email address."`
	FromName        string           `json:"from_name,omitempty" jsonschema:"Sender display name for \"Name <email>\" formatting."`
	FromEmail       string           `json:"from_email,omitempty" jsonschema:"\"Send As\" alias email (must be configured in Gmail settings)."`
	ThreadID        string           `json:"thread_id,omitempty" jsonschema:"Gmail thread ID to reply within."`
	InReplyTo       string           `json:"in_reply_to,omitempty" jsonschema:"Message-ID of message being replied to."`
	References      string           `json:"references,omitempty" jsonschema:"Chain of Message-IDs for proper threading."`
	Attachments     []map[string]any `json:"attachments,omitempty" jsonschema:"Optional list of attachments. Each can have: \"path\" (file path, auto-encodes), OR \"content\" (standard base64, not urlsafe) + \"filename\". Optional \"mime_type\". Example: [{\"path\": \"/path/to/file.pdf\"}] or [{\"filename\": \"doc.pdf\", \"content\": \"base64data\", \"mime_type\": \"application/pdf\"}]"`
}

func registerSendGmailMessage(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[sendGmailMessageIn](&mcp.Tool{
		Name:        "send_gmail_message",
		Description: "Sends an email using the user's Gmail account. Supports both new emails and replies with optional attachments. Supports Gmail's \"Send As\" feature to send from configured alias addresses.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "body_format", "plain", "html")
		if att := sch.Properties["attachments"]; att != nil {
			att.Items = &jsonschema.Schema{
				Type:                 "object",
				AdditionalProperties: &jsonschema.Schema{Type: "string"},
			}
		}
	})
	mcp.AddTool(s, tool, handleSendGmailMessage(getClient))
}

func handleSendGmailMessage(getClient httpClientFunc) mcp.ToolHandlerFor[sendGmailMessageIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in sendGmailMessageIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		to := in.To
		subject := in.Subject
		body := in.Body

		bodyFormat := in.BodyFormat
		if bodyFormat == "" {
			bodyFormat = "plain"
		}
		ccAddr := in.Cc
		bccAddr := in.Bcc
		fromName := in.FromName
		fromEmail := in.FromEmail
		threadID := in.ThreadID
		inReplyTo := in.InReplyTo
		references := in.References

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		sender := email
		if fromEmail != "" {
			sender = fromEmail
		}

		// Auto-prefix "Re: " for replies.
		if inReplyTo != "" && !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}

		attachments := getAttachments(in.Attachments)

		raw, err := buildRawMessage(sender, fromName, to, ccAddr, bccAddr, subject, body, bodyFormat, inReplyTo, references, attachments)
		if err != nil {
			return nil, nil, toolErr("Error building message: %v", err)
		}

		msg := &gmail.Message{Raw: raw}
		if threadID != "" {
			msg.ThreadId = threadID
		}

		sent, err := svc.Users.Messages.Send("me", msg).Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		return textResult(fmt.Sprintf("Email sent successfully!\nMessage ID: %s", sent.Id))
	}
}

// --- draft_gmail_message ---

// draftGmailMessageIn is the input schema for draft_gmail_message.
type draftGmailMessageIn struct {
	UserGoogleEmail string           `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Subject         string           `json:"subject" jsonschema:"Email subject."`
	Body            string           `json:"body" jsonschema:"Email body content (plain text or HTML)."`
	To              string           `json:"to,omitempty" jsonschema:"Recipient email address."`
	BodyFormat      string           `json:"body_format,omitempty" jsonschema:"Email body format."`
	Cc              string           `json:"cc,omitempty" jsonschema:"CC email address."`
	Bcc             string           `json:"bcc,omitempty" jsonschema:"BCC email address."`
	FromName        string           `json:"from_name,omitempty" jsonschema:"Sender display name for \"Name <email>\" formatting."`
	FromEmail       string           `json:"from_email,omitempty" jsonschema:"\"Send As\" alias email (must be configured in Gmail settings)."`
	ThreadID        string           `json:"thread_id,omitempty" jsonschema:"Gmail thread ID to reply within."`
	InReplyTo       string           `json:"in_reply_to,omitempty" jsonschema:"Message-ID of message being replied to."`
	References      string           `json:"references,omitempty" jsonschema:"Chain of Message-IDs for proper threading."`
	Attachments     []map[string]any `json:"attachments,omitempty" jsonschema:"Optional list of attachments. Each can have: 'path' (file path, auto-encodes), OR 'content' (standard base64, not urlsafe) + 'filename'. Optional 'mime_type' (auto-detected from path if not provided)."`
}

func registerDraftGmailMessage(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[draftGmailMessageIn](&mcp.Tool{
		Name:        "draft_gmail_message",
		Description: "Creates a draft email in the user's Gmail account. Supports both new drafts and reply drafts with optional attachments. Supports Gmail's \"Send As\" feature to draft from configured alias addresses.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "body_format", "plain", "html")
		if att := sch.Properties["attachments"]; att != nil {
			att.Items = &jsonschema.Schema{
				Type:                 "object",
				AdditionalProperties: &jsonschema.Schema{Type: "string"},
			}
		}
	})
	mcp.AddTool(s, tool, handleDraftGmailMessage(getClient))
}

func handleDraftGmailMessage(getClient httpClientFunc) mcp.ToolHandlerFor[draftGmailMessageIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in draftGmailMessageIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		subject := in.Subject
		body := in.Body

		toAddr := in.To
		bodyFormat := in.BodyFormat
		if bodyFormat == "" {
			bodyFormat = "plain"
		}
		ccAddr := in.Cc
		bccAddr := in.Bcc
		fromName := in.FromName
		fromEmail := in.FromEmail
		threadID := in.ThreadID
		inReplyTo := in.InReplyTo
		references := in.References

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		sender := email
		if fromEmail != "" {
			sender = fromEmail
		}

		if inReplyTo != "" && !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}

		attachments := getAttachments(in.Attachments)

		raw, err := buildRawMessage(sender, fromName, toAddr, ccAddr, bccAddr, subject, body, bodyFormat, inReplyTo, references, attachments)
		if err != nil {
			return nil, nil, toolErr("Error building message: %v", err)
		}

		innerMsg := &gmail.Message{Raw: raw}
		if threadID != "" {
			innerMsg.ThreadId = threadID
		}

		draft := &gmail.Draft{Message: innerMsg}

		created, err := svc.Users.Drafts.Create("me", draft).Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		return textResult(fmt.Sprintf("Draft created successfully!\nDraft ID: %s", created.Id))
	}
}

// --- manage_gmail_label ---

// manageGmailLabelIn is the input schema for manage_gmail_label.
type manageGmailLabelIn struct {
	UserGoogleEmail       string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Action                string `json:"action" jsonschema:"Action to perform."`
	Name                  string `json:"name,omitempty" jsonschema:"Label name. Required for create, optional for update."`
	LabelID               string `json:"label_id,omitempty" jsonschema:"Label ID. Required for update and delete."`
	LabelListVisibility   string `json:"label_list_visibility,omitempty" jsonschema:"Show in label list."`
	MessageListVisibility string `json:"message_list_visibility,omitempty" jsonschema:"Show in message list."`
}

func registerManageGmailLabel(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[manageGmailLabelIn](&mcp.Tool{
		Name:        "manage_gmail_label",
		Description: "Manages Gmail labels: create, update, or delete labels.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "action", "create", "update", "delete")
		setEnum(sch, "label_list_visibility", "labelShow", "labelHide")
		setEnum(sch, "message_list_visibility", "show", "hide")
	})
	mcp.AddTool(s, tool, handleManageGmailLabel(getClient))
}

func handleManageGmailLabel(getClient httpClientFunc) mcp.ToolHandlerFor[manageGmailLabelIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in manageGmailLabelIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		action := in.Action

		name := in.Name
		labelID := in.LabelID
		labelListVis := in.LabelListVisibility
		if labelListVis == "" {
			labelListVis = "labelShow"
		}
		msgListVis := in.MessageListVisibility
		if msgListVis == "" {
			msgListVis = "show"
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		switch action {
		case "create":
			if name == "" {
				return nil, nil, toolErr("name is required for create action")
			}
			label := &gmail.Label{
				Name:                  name,
				LabelListVisibility:   labelListVis,
				MessageListVisibility: msgListVis,
			}
			created, err := svc.Users.Labels.Create("me", label).Do()
			if err != nil {
				return nil, nil, toolErr("Gmail API error: %v", err)
			}
			return textResult(fmt.Sprintf("Label created successfully!\nName: %s\nID: %s", created.Name, created.Id))

		case "update":
			if labelID == "" {
				return nil, nil, toolErr("label_id is required for update action")
			}
			current, err := svc.Users.Labels.Get("me", labelID).Do()
			if err != nil {
				return nil, nil, toolErr("Gmail API error fetching label: %v", err)
			}
			updateName := current.Name
			if name != "" {
				updateName = name
			}
			label := &gmail.Label{
				Name:                  updateName,
				LabelListVisibility:   labelListVis,
				MessageListVisibility: msgListVis,
			}
			updated, err := svc.Users.Labels.Update("me", labelID, label).Do()
			if err != nil {
				return nil, nil, toolErr("Gmail API error: %v", err)
			}
			return textResult(fmt.Sprintf("Label updated successfully!\nName: %s\nID: %s", updated.Name, updated.Id))

		case "delete":
			if labelID == "" {
				return nil, nil, toolErr("label_id is required for delete action")
			}
			current, err := svc.Users.Labels.Get("me", labelID).Do()
			if err != nil {
				return nil, nil, toolErr("Gmail API error fetching label: %v", err)
			}
			err = svc.Users.Labels.Delete("me", labelID).Do()
			if err != nil {
				return nil, nil, toolErr("Gmail API error: %v", err)
			}
			return textResult(fmt.Sprintf("Label deleted successfully!\nName: %s\nID: %s", current.Name, current.Id))

		default:
			return nil, nil, toolErr("unknown action: %s", action)
		}
	}
}

// --- list_gmail_filters ---

// listGmailFiltersIn is the input schema for list_gmail_filters.
type listGmailFiltersIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerListGmailFilters(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_gmail_filters",
		Description: "Lists all Gmail filters configured in the user's mailbox.",
	}
	mcp.AddTool(s, tool, handleListGmailFilters(getClient))
}

func handleListGmailFilters(getClient httpClientFunc) mcp.ToolHandlerFor[listGmailFiltersIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listGmailFiltersIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resp, err := svc.Users.Settings.Filters.List("me").Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		filters := resp.Filter
		if len(filters) == 0 {
			return textResult("No filters found.")
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Found %d filters:\n\n", len(filters))
		for i, f := range filters {
			fmt.Fprintf(&b, "%d. Filter ID: %s\n", i+1, f.Id)
			if f.Criteria != nil {
				b.WriteString("   Criteria:\n")
				if f.Criteria.From != "" {
					fmt.Fprintf(&b, "     From: %s\n", f.Criteria.From)
				}
				if f.Criteria.To != "" {
					fmt.Fprintf(&b, "     To: %s\n", f.Criteria.To)
				}
				if f.Criteria.Subject != "" {
					fmt.Fprintf(&b, "     Subject: %s\n", f.Criteria.Subject)
				}
				if f.Criteria.Query != "" {
					fmt.Fprintf(&b, "     Query: %s\n", f.Criteria.Query)
				}
				if f.Criteria.NegatedQuery != "" {
					fmt.Fprintf(&b, "     Negated Query: %s\n", f.Criteria.NegatedQuery)
				}
				if f.Criteria.HasAttachment {
					b.WriteString("     Has Attachment: true\n")
				}
				if f.Criteria.ExcludeChats {
					b.WriteString("     Exclude Chats: true\n")
				}
				if f.Criteria.Size > 0 {
					fmt.Fprintf(&b, "     Size: %d (%s)\n", f.Criteria.Size, f.Criteria.SizeComparison)
				}
			}
			if f.Action != nil {
				b.WriteString("   Actions:\n")
				if f.Action.Forward != "" {
					fmt.Fprintf(&b, "     Forward to: %s\n", f.Action.Forward)
				}
				if len(f.Action.AddLabelIds) > 0 {
					fmt.Fprintf(&b, "     Add labels: %s\n", strings.Join(f.Action.AddLabelIds, ", "))
				}
				if len(f.Action.RemoveLabelIds) > 0 {
					fmt.Fprintf(&b, "     Remove labels: %s\n", strings.Join(f.Action.RemoveLabelIds, ", "))
				}
			}
			if i < len(filters)-1 {
				b.WriteString("\n")
			}
		}

		return textResult(b.String())
	}
}

// --- create_gmail_filter ---

// createGmailFilterIn is the input schema for create_gmail_filter.
type createGmailFilterIn struct {
	UserGoogleEmail string         `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Criteria        map[string]any `json:"criteria" jsonschema:"Filter criteria object. Supports: from, to, subject, query, negatedQuery, hasAttachment, excludeChats, size, sizeComparison."`
	Action          map[string]any `json:"action" jsonschema:"Filter action object. Supports: forward, addLabelIds, removeLabelIds."`
}

func registerCreateGmailFilter(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_gmail_filter",
		Description: "Creates a Gmail filter using the users.settings.filters API.",
	}
	mcp.AddTool(s, tool, handleCreateGmailFilter(getClient))
}

func handleCreateGmailFilter(getClient httpClientFunc) mcp.ToolHandlerFor[createGmailFilterIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createGmailFilterIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		criteriaRaw := in.Criteria
		actionRaw := in.Action

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		criteria := &gmail.FilterCriteria{}
		if v, ok := criteriaRaw["from"].(string); ok {
			criteria.From = v
		}
		if v, ok := criteriaRaw["to"].(string); ok {
			criteria.To = v
		}
		if v, ok := criteriaRaw["subject"].(string); ok {
			criteria.Subject = v
		}
		if v, ok := criteriaRaw["query"].(string); ok {
			criteria.Query = v
		}
		if v, ok := criteriaRaw["negatedQuery"].(string); ok {
			criteria.NegatedQuery = v
		}
		if v, ok := criteriaRaw["hasAttachment"].(bool); ok {
			criteria.HasAttachment = v
		}
		if v, ok := criteriaRaw["excludeChats"].(bool); ok {
			criteria.ExcludeChats = v
		}
		if v, ok := criteriaRaw["size"].(float64); ok {
			criteria.Size = int64(v)
		}
		if v, ok := criteriaRaw["sizeComparison"].(string); ok {
			criteria.SizeComparison = v
		}

		action := &gmail.FilterAction{}
		if v, ok := actionRaw["forward"].(string); ok {
			action.Forward = v
		}
		if v, ok := actionRaw["addLabelIds"].([]any); ok {
			for _, id := range v {
				if s, ok := id.(string); ok {
					action.AddLabelIds = append(action.AddLabelIds, s)
				}
			}
		}
		if v, ok := actionRaw["removeLabelIds"].([]any); ok {
			for _, id := range v {
				if s, ok := id.(string); ok {
					action.RemoveLabelIds = append(action.RemoveLabelIds, s)
				}
			}
		}

		filter := &gmail.Filter{
			Criteria: criteria,
			Action:   action,
		}

		created, err := svc.Users.Settings.Filters.Create("me", filter).Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		return textResult(fmt.Sprintf("Filter created successfully!\nFilter ID: %s", created.Id))
	}
}

// --- delete_gmail_filter ---

// deleteGmailFilterIn is the input schema for delete_gmail_filter.
type deleteGmailFilterIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	FilterID        string `json:"filter_id" jsonschema:"ID of the filter to delete."`
}

func registerDeleteGmailFilter(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "delete_gmail_filter",
		Description: "Deletes a Gmail filter by ID.",
	}
	mcp.AddTool(s, tool, handleDeleteGmailFilter(getClient))
}

func handleDeleteGmailFilter(getClient httpClientFunc) mcp.ToolHandlerFor[deleteGmailFilterIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteGmailFilterIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		filterID := in.FilterID

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Fetch filter details before deleting.
		filter, err := svc.Users.Settings.Filters.Get("me", filterID).Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error fetching filter: %v", err)
		}

		err = svc.Users.Settings.Filters.Delete("me", filterID).Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		criteriaStr := "(none)"
		if filter.Criteria != nil {
			var parts []string
			if filter.Criteria.From != "" {
				parts = append(parts, "from:"+filter.Criteria.From)
			}
			if filter.Criteria.To != "" {
				parts = append(parts, "to:"+filter.Criteria.To)
			}
			if filter.Criteria.Subject != "" {
				parts = append(parts, "subject:"+filter.Criteria.Subject)
			}
			if filter.Criteria.Query != "" {
				parts = append(parts, "query:"+filter.Criteria.Query)
			}
			if len(parts) > 0 {
				criteriaStr = strings.Join(parts, ", ")
			}
		}

		actionStr := "(none)"
		if filter.Action != nil {
			var parts []string
			if filter.Action.Forward != "" {
				parts = append(parts, "forward:"+filter.Action.Forward)
			}
			if len(filter.Action.AddLabelIds) > 0 {
				parts = append(parts, "addLabels:"+strings.Join(filter.Action.AddLabelIds, ","))
			}
			if len(filter.Action.RemoveLabelIds) > 0 {
				parts = append(parts, "removeLabels:"+strings.Join(filter.Action.RemoveLabelIds, ","))
			}
			if len(parts) > 0 {
				actionStr = strings.Join(parts, ", ")
			}
		}

		return textResult(fmt.Sprintf("Filter deleted successfully!\nFilter ID: %s\nCriteria: %s\nAction: %s", filterID, criteriaStr, actionStr))
	}
}

// --- modify_gmail_message_labels ---

// modifyGmailMessageLabelsIn is the input schema for modify_gmail_message_labels.
type modifyGmailMessageLabelsIn struct {
	UserGoogleEmail string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	MessageID       string   `json:"message_id" jsonschema:"ID of the message to modify."`
	AddLabelIds     []string `json:"add_label_ids,omitempty" jsonschema:"Label IDs to add."`
	RemoveLabelIds  []string `json:"remove_label_ids,omitempty" jsonschema:"Label IDs to remove."`
}

func registerModifyGmailMessageLabels(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "modify_gmail_message_labels",
		Description: "Adds or removes labels from a Gmail message. To archive an email, remove the INBOX label. To delete an email, add the TRASH label.",
	}
	mcp.AddTool(s, tool, handleModifyGmailMessageLabels(getClient))
}

func handleModifyGmailMessageLabels(getClient httpClientFunc) mcp.ToolHandlerFor[modifyGmailMessageLabelsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in modifyGmailMessageLabelsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		messageID := in.MessageID

		addLabels := in.AddLabelIds
		removeLabels := in.RemoveLabelIds

		if len(addLabels) == 0 && len(removeLabels) == 0 {
			return nil, nil, toolErr("at least one of add_label_ids or remove_label_ids must be provided")
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		modReq := &gmail.ModifyMessageRequest{}
		if len(addLabels) > 0 {
			modReq.AddLabelIds = addLabels
		}
		if len(removeLabels) > 0 {
			modReq.RemoveLabelIds = removeLabels
		}

		_, err = svc.Users.Messages.Modify("me", messageID, modReq).Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Labels modified for message %s\n", messageID)
		if len(addLabels) > 0 {
			fmt.Fprintf(&b, "Added: %s\n", strings.Join(addLabels, ", "))
		}
		if len(removeLabels) > 0 {
			fmt.Fprintf(&b, "Removed: %s\n", strings.Join(removeLabels, ", "))
		}

		return textResult(b.String())
	}
}

// --- batch_modify_gmail_message_labels ---

// batchModifyGmailMessageLabelsIn is the input schema for batch_modify_gmail_message_labels.
type batchModifyGmailMessageLabelsIn struct {
	UserGoogleEmail string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	MessageIDs      []string `json:"message_ids" jsonschema:"List of message IDs to modify."`
	AddLabelIds     []string `json:"add_label_ids,omitempty" jsonschema:"Label IDs to add to messages."`
	RemoveLabelIds  []string `json:"remove_label_ids,omitempty" jsonschema:"Label IDs to remove from messages."`
}

func registerBatchModifyGmailMessageLabels(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "batch_modify_gmail_message_labels",
		Description: "Adds or removes labels from multiple Gmail messages in a single batch request.",
	}
	mcp.AddTool(s, tool, handleBatchModifyGmailMessageLabels(getClient))
}

func handleBatchModifyGmailMessageLabels(getClient httpClientFunc) mcp.ToolHandlerFor[batchModifyGmailMessageLabelsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in batchModifyGmailMessageLabelsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		messageIDs := in.MessageIDs
		if len(messageIDs) == 0 {
			return nil, nil, toolErr("message_ids must not be empty")
		}

		addLabels := in.AddLabelIds
		removeLabels := in.RemoveLabelIds

		if len(addLabels) == 0 && len(removeLabels) == 0 {
			return nil, nil, toolErr("at least one of add_label_ids or remove_label_ids must be provided")
		}

		svc, err := newGmailService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		batchReq := &gmail.BatchModifyMessagesRequest{
			Ids: messageIDs,
		}
		if len(addLabels) > 0 {
			batchReq.AddLabelIds = addLabels
		}
		if len(removeLabels) > 0 {
			batchReq.RemoveLabelIds = removeLabels
		}

		err = svc.Users.Messages.BatchModify("me", batchReq).Do()
		if err != nil {
			return nil, nil, toolErr("Gmail API error: %v", err)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Labels modified for %d messages\n", len(messageIDs))
		if len(addLabels) > 0 {
			fmt.Fprintf(&b, "Added: %s\n", strings.Join(addLabels, ", "))
		}
		if len(removeLabels) > 0 {
			fmt.Fprintf(&b, "Removed: %s\n", strings.Join(removeLabels, ", "))
		}

		return textResult(b.String())
	}
}

// --- Gmail write helper functions ---

// emailAttachment represents a parsed attachment for message building.
type emailAttachment struct {
	filename string
	mimeType string
	data     []byte
}

// getAttachments extracts attachment data from the typed attachments input.
func getAttachments(arr []map[string]any) []emailAttachment {
	if len(arr) == 0 {
		return nil
	}
	var result []emailAttachment
	for _, m := range arr {
		att := emailAttachment{}
		mimeType, _ := m["mime_type"].(string)

		if path, ok := m["path"].(string); ok && path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			att.data = data
			att.filename = filepath.Base(path)
			if mimeType == "" {
				mimeType = mime.TypeByExtension(filepath.Ext(path))
			}
		} else if content, ok := m["content"].(string); ok && content != "" {
			data, err := base64.StdEncoding.DecodeString(content)
			if err != nil {
				continue
			}
			att.data = data
			att.filename, _ = m["filename"].(string)
		}

		if att.filename == "" {
			att.filename = "attachment"
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		att.mimeType = mimeType

		if len(att.data) > 0 {
			result = append(result, att)
		}
	}
	return result
}

// buildRawMessage constructs a base64url-encoded RFC 2822 message with optional attachments.
func buildRawMessage(from, fromName, to, cc, bcc, subject, body, bodyFormat, inReplyTo, references string, attachments []emailAttachment) (string, error) {
	var msg strings.Builder

	// Common headers.
	if fromName != "" {
		msg.WriteString(fmt.Sprintf("From: %s\r\n", formatAddress(fromName, from)))
	} else {
		msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	}

	if to != "" {
		msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	}
	if cc != "" {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", cc))
	}
	if bcc != "" {
		msg.WriteString(fmt.Sprintf("Bcc: %s\r\n", bcc))
	}

	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", encodeSubject(subject)))

	if inReplyTo != "" {
		msg.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", inReplyTo))
	}
	if references != "" {
		msg.WriteString(fmt.Sprintf("References: %s\r\n", references))
	}

	msg.WriteString("MIME-Version: 1.0\r\n")

	contentType := "text/plain"
	if bodyFormat == "html" {
		contentType = "text/html"
	}

	if len(attachments) == 0 {
		// Simple message without attachments.
		msg.WriteString(fmt.Sprintf("Content-Type: %s; charset=\"UTF-8\"\r\n", contentType))
		msg.WriteString("\r\n")
		msg.WriteString(body)
	} else {
		// Multipart message with attachments.
		boundary := "boundary_mcp_go_attachment"
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
		msg.WriteString("\r\n")

		// Body part.
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString(fmt.Sprintf("Content-Type: %s; charset=\"UTF-8\"\r\n", contentType))
		msg.WriteString("\r\n")
		msg.WriteString(body)
		msg.WriteString("\r\n")

		// Attachment parts.
		for _, att := range attachments {
			msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
			msg.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", att.mimeType, att.filename))
			msg.WriteString("Content-Transfer-Encoding: base64\r\n")
			msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", att.filename))
			msg.WriteString("\r\n")
			msg.WriteString(base64.StdEncoding.EncodeToString(att.data))
			msg.WriteString("\r\n")
		}

		msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	}

	return base64.URLEncoding.EncodeToString([]byte(msg.String())), nil
}

// formatAddress formats "Name <email>" using RFC 2047 encoding for the name.
func formatAddress(name, email string) string {
	addr := mail.Address{Name: name, Address: email}
	return addr.String()
}

// encodeSubject encodes a subject line using RFC 2047 if needed.
func encodeSubject(subject string) string {
	for _, c := range subject {
		if c > 127 {
			return mime.QEncoding.Encode("utf-8", subject)
		}
	}
	return subject
}

// --- helper functions ---

// extractHeaders extracts the specified headers from a MessagePart payload.
func extractHeaders(payload *gmail.MessagePart, headerNames []string) map[string]string {
	result := make(map[string]string, len(headerNames))
	if payload == nil {
		return result
	}
	want := make(map[string]bool, len(headerNames))
	for _, h := range headerNames {
		want[h] = true
	}
	for _, h := range payload.Headers {
		if want[h.Name] {
			result[h.Name] = h.Value
		}
	}
	return result
}

// headerOrDefault returns the header value or a default.
func headerOrDefault(headers map[string]string, key, def string) string {
	if v, ok := headers[key]; ok && v != "" {
		return v
	}
	return def
}

// attachmentMeta holds metadata about an attachment.
type attachmentMeta struct {
	filename     string
	mimeType     string
	size         int64
	attachmentID string
}

// extractAttachments extracts attachment metadata from a message payload.
func extractAttachments(payload *gmail.MessagePart) []attachmentMeta {
	if payload == nil {
		return nil
	}
	var result []attachmentMeta
	var walk func(part *gmail.MessagePart)
	walk = func(part *gmail.MessagePart) {
		if part.Filename != "" && part.Body != nil && part.Body.AttachmentId != "" {
			result = append(result, attachmentMeta{
				filename:     part.Filename,
				mimeType:     part.MimeType,
				size:         part.Body.Size,
				attachmentID: part.Body.AttachmentId,
			})
		}
		for _, child := range part.Parts {
			walk(child)
		}
	}
	walk(payload)
	return result
}

// extractMessageBodies extracts text/plain and text/html bodies from a message payload.
func extractMessageBodies(payload *gmail.MessagePart) (textBody, htmlBody string) {
	if payload == nil {
		return "", ""
	}

	var walk func(part *gmail.MessagePart)
	walk = func(part *gmail.MessagePart) {
		if part.MimeType == "text/plain" && textBody == "" {
			if part.Body != nil && part.Body.Data != "" {
				textBody = decodeBase64URL(part.Body.Data)
			}
		} else if part.MimeType == "text/html" && htmlBody == "" {
			if part.Body != nil && part.Body.Data != "" {
				htmlBody = decodeBase64URL(part.Body.Data)
			}
		}
		for _, child := range part.Parts {
			walk(child)
		}
	}
	walk(payload)
	return textBody, htmlBody
}

// formatBodyContent returns the text body, falling back to HTML body if empty.
func formatBodyContent(textBody, htmlBody string) string {
	if textBody != "" {
		return textBody
	}
	if htmlBody != "" {
		return "[HTML body - plain text not available]\n" + htmlBody
	}
	return ""
}

// decodeBase64URL decodes a base64url-encoded string (Gmail API format).
func decodeBase64URL(s string) string {
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		// Try with padding stripped (Gmail sometimes omits padding).
		data, err = base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			return s // Return raw if decode fails.
		}
	}
	return string(data)
}

package tools

import (
	"context"
	"fmt"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	chat "google.golang.org/api/chat/v1"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

// RegisterChatTools registers all Chat tools with the MCP server.
func RegisterChatTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	registerListSpaces(s, getClient)
	registerGetMessages(s, getClient)
	registerSendMessage(s, getClient)
	registerSearchMessages(s, getClient)
}

// newChatService creates a chat.Service for the given user email.
func newChatService(ctx context.Context, getClient httpClientFunc, email string) (*chat.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := chat.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Chat service: %w", err)
	}
	return svc, nil
}

// --- list_spaces ---

func registerListSpaces(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_spaces",
		Description: "Lists Google Chat spaces (rooms and direct messages) accessible to the user.",
	}
	mcp.AddTool(s, tool, handleListSpaces(getClient))
}

// listSpacesIn is the input schema for list_spaces.
type listSpacesIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"User's Google email address"`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Number of spaces to return per page. Defaults to 100."`
	SpaceType       string `json:"space_type,omitempty" jsonschema:"Type of spaces to filter: 'all', 'room', or 'dm'. Defaults to 'all'."`
}

func handleListSpaces(getClient httpClientFunc) mcp.ToolHandlerFor[listSpacesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listSpacesIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 100
		}
		spaceType := in.SpaceType
		if spaceType == "" {
			spaceType = "all"
		}

		svc, err := newChatService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		call := svc.Spaces.List().PageSize(int64(pageSize))

		// Build filter based on space_type
		switch spaceType {
		case "room":
			call = call.Filter("spaceType = SPACE")
		case "dm":
			call = call.Filter("spaceType = DIRECT_MESSAGE")
		}

		resp, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("listing spaces: %v", err)
		}

		spaces := resp.Spaces
		if len(spaces) == 0 {
			return textResult(fmt.Sprintf("No Chat spaces found for type '%s'.", spaceType))
		}

		var out strings.Builder
		fmt.Fprintf(&out, "Found %d Chat spaces (type: %s):", len(spaces), spaceType)
		for _, space := range spaces {
			name := space.DisplayName
			if name == "" {
				name = "Unnamed Space"
			}
			spaceTypeActual := space.SpaceType
			if spaceTypeActual == "" {
				spaceTypeActual = "UNKNOWN"
			}
			fmt.Fprintf(&out, "\n- %s (ID: %s, Type: %s)", name, space.Name, spaceTypeActual)
		}

		return textResult(out.String())
	}
}

// --- get_messages ---

func registerGetMessages(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_messages",
		Description: "Retrieves messages from a Google Chat space.",
	}
	mcp.AddTool(s, tool, handleGetMessages(getClient))
}

// getMessagesIn is the input schema for get_messages.
type getMessagesIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"User's Google email address"`
	SpaceID         string `json:"space_id" jsonschema:"The ID of the Chat space to retrieve messages from"`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Number of messages to return per page. Defaults to 50."`
	OrderBy         string `json:"order_by,omitempty" jsonschema:"Sort order for messages. Defaults to 'createTime desc'."`
}

func handleGetMessages(getClient httpClientFunc) mcp.ToolHandlerFor[getMessagesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getMessagesIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spaceID := in.SpaceID

		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 50
		}
		orderBy := in.OrderBy
		if orderBy == "" {
			orderBy = "createTime desc"
		}

		svc, err := newChatService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Get space info first
		spaceInfo, err := svc.Spaces.Get(spaceID).Do()
		if err != nil {
			return nil, nil, toolErr("getting space info: %v", err)
		}
		spaceName := spaceInfo.DisplayName
		if spaceName == "" {
			spaceName = "Unknown Space"
		}

		// Get messages
		resp, err := svc.Spaces.Messages.List(spaceID).
			PageSize(int64(pageSize)).
			OrderBy(orderBy).
			Do()
		if err != nil {
			return nil, nil, toolErr("listing messages: %v", err)
		}

		messages := resp.Messages
		if len(messages) == 0 {
			return textResult(fmt.Sprintf("No messages found in space '%s' (ID: %s).", spaceName, spaceID))
		}

		var out strings.Builder
		fmt.Fprintf(&out, "Messages from '%s' (ID: %s):\n", spaceName, spaceID)
		for _, msg := range messages {
			sender := "Unknown Sender"
			if msg.Sender != nil && msg.Sender.DisplayName != "" {
				sender = msg.Sender.DisplayName
			}
			createTime := msg.CreateTime
			if createTime == "" {
				createTime = "Unknown Time"
			}
			text := msg.Text
			if text == "" {
				text = "No text content"
			}

			fmt.Fprintf(&out, "\n[%s] %s:", createTime, sender)
			fmt.Fprintf(&out, "\n  %s", text)
			fmt.Fprintf(&out, "\n  (Message ID: %s)\n", msg.Name)
		}

		return textResult(out.String())
	}
}

// --- send_message ---

func registerSendMessage(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "send_message",
		Description: "Sends a message to a Google Chat space.",
	}
	mcp.AddTool(s, tool, handleSendMessage(getClient))
}

// sendMessageIn is the input schema for send_message.
type sendMessageIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"User's Google email address"`
	SpaceID         string `json:"space_id" jsonschema:"The ID of the Chat space to send the message to"`
	MessageText     string `json:"message_text" jsonschema:"The text content of the message to send"`
	ThreadKey       string `json:"thread_key,omitempty" jsonschema:"Optional thread key for threaded replies"`
}

func handleSendMessage(getClient httpClientFunc) mcp.ToolHandlerFor[sendMessageIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in sendMessageIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spaceID := in.SpaceID
		messageText := in.MessageText
		threadKey := in.ThreadKey

		svc, err := newChatService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		msgBody := &chat.Message{
			Text: messageText,
		}

		call := svc.Spaces.Messages.Create(spaceID, msgBody)
		if threadKey != "" {
			call = call.ThreadKey(threadKey)
		}

		message, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("sending message: %v", err)
		}

		result := fmt.Sprintf("Message sent to space '%s' by %s. Message ID: %s, Time: %s",
			spaceID, email, message.Name, message.CreateTime)

		return textResult(result)
	}
}

// --- search_messages ---

func registerSearchMessages(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "search_messages",
		Description: "Searches for messages in Google Chat spaces by text content.",
	}
	mcp.AddTool(s, tool, handleSearchMessages(getClient))
}

// searchMessagesIn is the input schema for search_messages.
type searchMessagesIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"User's Google email address"`
	Query           string `json:"query" jsonschema:"Search query text to find in messages"`
	SpaceID         string `json:"space_id,omitempty" jsonschema:"Optional space ID to limit search to a specific space; if not provided, searches all accessible spaces"`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Number of messages to return per page. Defaults to 25."`
}

func handleSearchMessages(getClient httpClientFunc) mcp.ToolHandlerFor[searchMessagesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in searchMessagesIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		query := in.Query

		spaceID := in.SpaceID
		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 25
		}

		svc, err := newChatService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		type messageWithSpace struct {
			msg       *chat.Message
			spaceName string
		}

		var results []messageWithSpace
		var searchContext string

		filter := fmt.Sprintf(`text:"%s"`, query)

		if spaceID != "" {
			// Search within a specific space
			resp, err := svc.Spaces.Messages.List(spaceID).
				PageSize(int64(pageSize)).
				Filter(filter).
				Do()
			if err != nil {
				return nil, nil, toolErr("searching messages: %v", err)
			}
			for _, msg := range resp.Messages {
				results = append(results, messageWithSpace{msg: msg, spaceName: ""})
			}
			searchContext = fmt.Sprintf("space '%s'", spaceID)
		} else {
			// Search across all accessible spaces
			spacesResp, err := svc.Spaces.List().PageSize(100).Do()
			if err != nil {
				return nil, nil, toolErr("listing spaces for search: %v", err)
			}

			// Limit to first 10 spaces to avoid timeout
			spaces := spacesResp.Spaces
			if len(spaces) > 10 {
				spaces = spaces[:10]
			}

			for _, space := range spaces {
				resp, err := svc.Spaces.Messages.List(space.Name).
					PageSize(5).
					Filter(filter).
					Do()
				if err != nil {
					continue // Skip spaces we can't access
				}
				displayName := space.DisplayName
				if displayName == "" {
					displayName = "Unknown"
				}
				for _, msg := range resp.Messages {
					results = append(results, messageWithSpace{msg: msg, spaceName: displayName})
				}
			}
			searchContext = "all accessible spaces"
		}

		if len(results) == 0 {
			return textResult(fmt.Sprintf("No messages found matching '%s' in %s.", query, searchContext))
		}

		var out strings.Builder
		fmt.Fprintf(&out, "Found %d messages matching '%s' in %s:", len(results), query, searchContext)
		for _, r := range results {
			sender := "Unknown Sender"
			if r.msg.Sender != nil && r.msg.Sender.DisplayName != "" {
				sender = r.msg.Sender.DisplayName
			}
			createTime := r.msg.CreateTime
			if createTime == "" {
				createTime = "Unknown Time"
			}
			text := r.msg.Text
			if text == "" {
				text = "No text content"
			}
			spaceName := r.spaceName
			if spaceName == "" {
				spaceName = "Unknown Space"
			}

			// Truncate long messages
			if len(text) > 100 {
				text = text[:100] + "..."
			}

			fmt.Fprintf(&out, "\n- [%s] %s in '%s': %s", createTime, sender, spaceName, text)
		}

		return textResult(out.String())
	}
}

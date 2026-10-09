package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	drive "google.golang.org/api/drive/v3"
)

// commentToolArgs decodes the raw arguments of a low-level tool handler into
// a keyed map. The comment tools have dynamic parameter names (the file ID
// parameter differs per app), so they parse arguments manually instead of
// using typed input structs.
func commentToolArgs(req *mcp.CallToolRequest) map[string]any {
	var args map[string]any
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &args)
	}
	return args
}

// commentInputSchema builds the input schema for a comment tool.
// user_google_email and the app-specific file ID property are always present;
// extra adds operation-specific properties (comment IDs, reply contents).
// required names the properties the call must supply.
func commentInputSchema(appTitle, fileIDParam string, extra map[string]string, required []string) *jsonschema.Schema {
	props := map[string]*jsonschema.Schema{
		"user_google_email": {Type: "string", Description: "The user's Google email address."},
		fileIDParam:         {Type: "string", Description: fmt.Sprintf("The ID of the Google %s.", appTitle)},
	}
	order := []string{"user_google_email", fileIDParam}
	for name, desc := range extra {
		props[name] = &jsonschema.Schema{Type: "string", Description: desc}
		order = append(order, name)
	}
	return &jsonschema.Schema{
		Type:          "object",
		Properties:    props,
		Required:      required,
		PropertyOrder: order,
	}
}

// RegisterCommentTools registers 4 comment management tools for a Google Workspace app.
// appName is the app type (e.g. "document", "spreadsheet", "presentation").
// fileIDParam is the parameter name for the file ID (e.g. "document_id", "spreadsheet_id", "presentation_id").
func RegisterCommentTools(s *mcp.Server, getClient httpClientFunc, appName, fileIDParam string) {
	appTitle := titleCase(appName)

	// read_{appName}_comments
	s.AddTool(
		&mcp.Tool{
			Name:        fmt.Sprintf("read_%s_comments", appName),
			Description: fmt.Sprintf("Read all comments from a Google %s.", appTitle),
			InputSchema: commentInputSchema(appTitle, fileIDParam, nil, []string{fileIDParam}),
		},
		makeReadCommentsHandler(getClient, appName, fileIDParam),
	)

	// create_{appName}_comment
	s.AddTool(
		&mcp.Tool{
			Name:        fmt.Sprintf("create_%s_comment", appName),
			Description: fmt.Sprintf("Create a new comment on a Google %s.", appTitle),
			InputSchema: commentInputSchema(appTitle, fileIDParam, map[string]string{
				"comment_content": "The content of the comment.",
			}, []string{fileIDParam, "comment_content"}),
		},
		makeCreateCommentHandler(getClient, appName, fileIDParam),
	)

	// reply_to_{appName}_comment
	s.AddTool(
		&mcp.Tool{
			Name:        fmt.Sprintf("reply_to_%s_comment", appName),
			Description: fmt.Sprintf("Reply to a specific comment on a Google %s.", appTitle),
			InputSchema: commentInputSchema(appTitle, fileIDParam, map[string]string{
				"comment_id":    "The ID of the comment to reply to.",
				"reply_content": "The content of the reply.",
			}, []string{fileIDParam, "comment_id", "reply_content"}),
		},
		makeReplyToCommentHandler(getClient, appName, fileIDParam),
	)

	// resolve_{appName}_comment
	s.AddTool(
		&mcp.Tool{
			Name:        fmt.Sprintf("resolve_%s_comment", appName),
			Description: fmt.Sprintf("Resolve a comment on a Google %s.", appTitle),
			InputSchema: commentInputSchema(appTitle, fileIDParam, map[string]string{
				"comment_id": "The ID of the comment to resolve.",
			}, []string{fileIDParam, "comment_id"}),
		},
		makeResolveCommentHandler(getClient, appName, fileIDParam),
	)
}

// titleCase returns the first letter capitalized, rest lower (simple single-word).
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}

// commentStringArg reads a required string argument for a comment tool.
// It mirrors the runtime checks the old untyped request parsing performed.
func commentStringArg(args map[string]any, key string) (string, bool) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// makeReadCommentsHandler creates a handler that reads all comments from a file.
func makeReadCommentsHandler(getClient httpClientFunc, appName, fileIDParam string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := commentToolArgs(req)
		emailArg, _ := commentStringArg(args, "user_google_email")
		email, err := resolveEmail(emailArg)
		if err != nil {
			return isErrorResult(err.Error()), nil
		}
		fileID, ok := commentStringArg(args, fileIDParam)
		if !ok {
			return isErrorResult(fmt.Sprintf("%s is required", fileIDParam)), nil
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return isErrorResult(err.Error()), nil
		}

		resp, err := svc.Comments.List(fileID).
			Fields("comments(id,content,author,createdTime,modifiedTime,resolved,replies(content,author,id,createdTime,modifiedTime))").
			Do()
		if err != nil {
			return isErrorResult(fmt.Sprintf("listing comments: %v", err)), nil
		}

		if len(resp.Comments) == 0 {
			return textToolResult(fmt.Sprintf("No comments found in %s %s", appName, fileID)), nil
		}

		var out strings.Builder
		fmt.Fprintf(&out, "Found %d comments in %s %s:\n", len(resp.Comments), appName, fileID)

		for _, c := range resp.Comments {
			author := "Unknown"
			if c.Author != nil && c.Author.DisplayName != "" {
				author = c.Author.DisplayName
			}
			status := ""
			if c.Resolved {
				status = " [RESOLVED]"
			}

			fmt.Fprintf(&out, "\nComment ID: %s\n", c.Id)
			fmt.Fprintf(&out, "Author: %s\n", author)
			fmt.Fprintf(&out, "Created: %s%s\n", c.CreatedTime, status)
			fmt.Fprintf(&out, "Content: %s\n", c.Content)

			if len(c.Replies) > 0 {
				fmt.Fprintf(&out, "  Replies (%d):\n", len(c.Replies))
				for _, r := range c.Replies {
					replyAuthor := "Unknown"
					if r.Author != nil && r.Author.DisplayName != "" {
						replyAuthor = r.Author.DisplayName
					}
					fmt.Fprintf(&out, "    Reply ID: %s\n", r.Id)
					fmt.Fprintf(&out, "    Author: %s\n", replyAuthor)
					fmt.Fprintf(&out, "    Created: %s\n", r.CreatedTime)
					fmt.Fprintf(&out, "    Content: %s\n", r.Content)
				}
			}
		}

		return textToolResult(out.String()), nil
	}
}

// makeCreateCommentHandler creates a handler that creates a new comment on a file.
func makeCreateCommentHandler(getClient httpClientFunc, appName, fileIDParam string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := commentToolArgs(req)
		emailArg, _ := commentStringArg(args, "user_google_email")
		email, emailErr := resolveEmail(emailArg)
		if emailErr != nil {
			return isErrorResult("user_google_email is required"), nil
		}
		fileID, ok := commentStringArg(args, fileIDParam)
		if !ok {
			return isErrorResult(fmt.Sprintf("%s is required", fileIDParam)), nil
		}
		content, ok := commentStringArg(args, "comment_content")
		if !ok {
			return isErrorResult("comment_content is required"), nil
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return isErrorResult(err.Error()), nil
		}

		comment, err := svc.Comments.Create(fileID, &drive.Comment{
			Content: content,
		}).Fields("id,content,author,createdTime,modifiedTime").Do()
		if err != nil {
			return isErrorResult(fmt.Sprintf("creating comment: %v", err)), nil
		}

		author := "Unknown"
		if comment.Author != nil && comment.Author.DisplayName != "" {
			author = comment.Author.DisplayName
		}

		return textToolResult(fmt.Sprintf(
			"Comment created successfully!\nComment ID: %s\nAuthor: %s\nCreated: %s\nContent: %s",
			comment.Id, author, comment.CreatedTime, content,
		)), nil
	}
}

// makeReplyToCommentHandler creates a handler that replies to a comment on a file.
func makeReplyToCommentHandler(getClient httpClientFunc, appName, fileIDParam string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := commentToolArgs(req)
		emailArg, _ := commentStringArg(args, "user_google_email")
		email, emailErr := resolveEmail(emailArg)
		if emailErr != nil {
			return isErrorResult("user_google_email is required"), nil
		}
		fileID, ok := commentStringArg(args, fileIDParam)
		if !ok {
			return isErrorResult(fmt.Sprintf("%s is required", fileIDParam)), nil
		}
		commentID, ok := commentStringArg(args, "comment_id")
		if !ok {
			return isErrorResult("comment_id is required"), nil
		}
		replyContent, ok := commentStringArg(args, "reply_content")
		if !ok {
			return isErrorResult("reply_content is required"), nil
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return isErrorResult(err.Error()), nil
		}

		reply, err := svc.Replies.Create(fileID, commentID, &drive.Reply{
			Content: replyContent,
		}).Fields("id,content,author,createdTime,modifiedTime").Do()
		if err != nil {
			return isErrorResult(fmt.Sprintf("replying to comment: %v", err)), nil
		}

		author := "Unknown"
		if reply.Author != nil && reply.Author.DisplayName != "" {
			author = reply.Author.DisplayName
		}

		return textToolResult(fmt.Sprintf(
			"Reply posted successfully!\nReply ID: %s\nAuthor: %s\nCreated: %s\nContent: %s",
			reply.Id, author, reply.CreatedTime, replyContent,
		)), nil
	}
}

// makeResolveCommentHandler creates a handler that resolves a comment on a file.
func makeResolveCommentHandler(getClient httpClientFunc, appName, fileIDParam string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := commentToolArgs(req)
		emailArg, _ := commentStringArg(args, "user_google_email")
		email, emailErr := resolveEmail(emailArg)
		if emailErr != nil {
			return isErrorResult("user_google_email is required"), nil
		}
		fileID, ok := commentStringArg(args, fileIDParam)
		if !ok {
			return isErrorResult(fmt.Sprintf("%s is required", fileIDParam)), nil
		}
		commentID, ok := commentStringArg(args, "comment_id")
		if !ok {
			return isErrorResult("comment_id is required"), nil
		}

		svc, err := newDriveService(ctx, getClient, email)
		if err != nil {
			return isErrorResult(err.Error()), nil
		}

		reply, err := svc.Replies.Create(fileID, commentID, &drive.Reply{
			Content: "This comment has been resolved.",
			Action:  "resolve",
		}).Fields("id,content,author,createdTime,modifiedTime").Do()
		if err != nil {
			return isErrorResult(fmt.Sprintf("resolving comment: %v", err)), nil
		}

		author := "Unknown"
		if reply.Author != nil && reply.Author.DisplayName != "" {
			author = reply.Author.DisplayName
		}

		return textToolResult(fmt.Sprintf(
			"Comment %s has been resolved successfully.\nResolve reply ID: %s\nAuthor: %s\nCreated: %s",
			commentID, reply.Id, author, reply.CreatedTime,
		)), nil
	}
}

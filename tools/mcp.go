// mcp.go holds shared helpers for the official go-sdk tool handlers.
package tools

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/magks/google-workspace-mcp-go/auth"
)

// errTool marks a tool-level error — one that should be rendered to the
// caller as an IsError result instead of a protocol error.
type errTool struct{ msg string }

func (e *errTool) Error() string { return e.msg }

// toolErr returns a tool-level error. The SDK turns it into a result with
// IsError set and msg as its text content. Use it for expected failures
// (bad arguments, API errors so on). format must only contain verbs that
// were intended; strings built elsewhere pass through %s or errors.New.
func toolErr(format string, args ...any) error {
	return &errTool{msg: fmt.Sprintf(format, args...)}
}

// textResult returns a text-only tool result.
func textResult(text string) (*mcp.CallToolResult, any, error) {
	return textToolResult(text), nil, nil
}

// textToolResult builds a CallToolResult with a single text content block.
func textToolResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// isErrorResult builds a CallToolResult flagged as a tool error with the
// given text. Handlers normally return errTool instead; this helper exists
// for tests.
func isErrorResult(text string) *mcp.CallToolResult {
	res := textToolResult(text)
	res.IsError = true
	return res
}

// inputSchema derives the tool's input schema from the In struct (including
// json/jsonschema tags) and applies tweak before binding it to t.
//
// Struct tags can only express property names, optionality, and
// descriptions. Use this helper when a tool needs richer JSON Schema
// constraints — enums on string fields, for example — by editing the derived
// schema in tweak. The returned Tool can then be passed to mcp.AddTool.
func inputSchema[In any](t *mcp.Tool, tweak func(*jsonschema.Schema)) *mcp.Tool {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("inputSchema: tool %q: %v", t.Name, err))
	}
	if tweak != nil {
		tweak(schema)
	}
	t.InputSchema = schema
	return t
}

// setEnum applies enum values to a string property of a derived input
// schema. It is meant to be called from an inputSchema tweak function.
func setEnum(s *jsonschema.Schema, prop string, values ...string) {
	p, ok := s.Properties[prop]
	if !ok {
		panic(fmt.Sprintf("setEnum: no property %s in schema", prop))
	}
	enum := make([]any, len(values))
	for i, v := range values {
		enum[i] = v
	}
	p.Enum = enum
}

// resolveEmail extracts the user's Google email for a tool call.
//
// Order of precedence:
//  1. the user_google_email argument carried by the request (via In struct)
//  2. the USER_GOOGLE_EMAIL environment variable
//  3. the single credential in the credential store (single-user mode)
//
// This keeps the tool identity stateless: the caller supplies the email on
// every request; the server never infers it from the connection.
func resolveEmail(requestedEmail string) (string, error) {
	if strings.TrimSpace(requestedEmail) != "" {
		return requestedEmail, nil
	}

	if envEmail := os.Getenv("USER_GOOGLE_EMAIL"); strings.TrimSpace(envEmail) != "" {
		return envEmail, nil
	}

	store := auth.NewCredentialStore()
	users, err := store.ListUsers()
	if err == nil && len(users) == 1 {
		return users[0], nil
	}

	return "", errors.New("user_google_email is required: provide it as a parameter, set USER_GOOGLE_EMAIL env var, or use --single-user with one credential file")
}

package mcp

import (
	"context"
	"net/http"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server with all alist tools registered.
func NewServer() *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		"alist",
		conf.Version,
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithRecovery(),
	)
	registerReadTools(s)
	registerManageTools(s)
	registerUploadTools(s)
	return s
}

// NewHTTPHandler creates a Streamable HTTP handler for the MCP server.
func NewHTTPHandler() http.Handler {
	s := NewServer()
	return mcpserver.NewStreamableHTTPServer(s,
		mcpserver.WithHTTPContextFunc(HTTPContextFunc),
	)
}

// NewStdioServer creates an MCP server configured for STDIO mode with a fixed user.
func NewStdioServer(username string) (*mcpserver.MCPServer, *model.User, error) {
	user, err := resolveUserForStdio(username)
	if err != nil {
		return nil, nil, err
	}
	s := NewServer()
	return s, user, nil
}

// ServeStdio starts the MCP server in STDIO mode.
func ServeStdio(username string) error {
	s, user, err := NewStdioServer(username)
	if err != nil {
		return err
	}
	ctxFunc := userContextMiddleware(user)
	return mcpserver.ServeStdio(s, mcpserver.WithStdioContextFunc(ctxFunc))
}

// toolHandlerWithAuth wraps a tool handler to require authentication.
func toolHandlerWithAuth(fn func(ctx context.Context, user *model.User, request mcp.CallToolRequest) (*mcp.CallToolResult, error)) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		user, err := resolveUser(ctx)
		if err != nil {
			return toolError("authentication required")
		}
		return fn(ctx, user, request)
	}
}

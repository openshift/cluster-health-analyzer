package mcp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type authHeader string

const authHeaderStr authHeader = "kubernetes-authorization"

// MCPHealthServer is a helper and wrapper type
// providing basic methods to run the underlying SSE server
// and to register tools
type MCPHealthServer struct {
	mcpServer  *server.MCPServer
	httpServer *server.StreamableHTTPServer
	addr       string
}

// NewMCPSSEServer
func NewMCPSSEServer(name, version, url string) *MCPHealthServer {
	mcpServer := server.NewMCPServer(name, version, server.WithToolCapabilities(true))
	streamableServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithHTTPContextFunc(authFromRequest))

	return &MCPHealthServer{
		mcpServer:  mcpServer,
		httpServer: streamableServer,
		addr:       url,
	}
}

// Start
func (m *MCPHealthServer) Start() error {
	slog.Info("Starting MCP server on ", "address", m.addr)
	return m.httpServer.Start(m.addr)
}

// RegisterTool
func (m *MCPHealthServer) RegisterTool(t mcp.Tool, handler server.ToolHandlerFunc) {
	m.mcpServer.AddTool(t, handler)
	slog.Info("Registered tool ", "name", t.Name)
}

func authFromRequest(ctx context.Context, r *http.Request) context.Context {
	token := r.Header.Get(string(authHeaderStr))
	return context.WithValue(ctx, authHeaderStr, token)
}

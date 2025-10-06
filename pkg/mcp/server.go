package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authHeader string

const authHeaderStr authHeader = "kubernetes-authorization"

// MCPHealthServer is a helper and wrapper type
// providing basic methods to run the underlying SSE server
// and to register tools
type MCPHealthServer struct {
	server *mcp.Server
	addr   string
}

type MCPHealthServerCfg struct {
	Name    string
	Version string
	Url     string

	PrometheusURL   string
	AlertManagerURL string
}

// NewMCPHealthServer returns an instance of the MCPHealthServer
func NewMCPHealthServer(cfg MCPHealthServerCfg) *MCPHealthServer {
	impl := mcp.Implementation{
		Name:    cfg.Name,
		Version: cfg.Version,
	}

	server := mcp.NewServer(&impl, &mcp.ServerOptions{HasTools: true})

	incTool := NewIncidentsTool(cfg.PrometheusURL, cfg.AlertManagerURL)
	// get_incidents
	mcp.AddTool(server, &incTool.Tool, mcp.ToolHandlerFor[GetIncidentsParams, any](incTool.IncidentsHandler))

	return &MCPHealthServer{
		server: server,
		addr:   cfg.Url,
	}
}

// Start runs the MCPHealthServer
func (m *MCPHealthServer) Start() error {
	if m.addr == "" {
		return errors.New("empty http address")
	}
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return m.server
	}, nil)

	slog.Info("Starting MCP server on ", "address", m.addr)

	// the following middleware is needed to enrich the context that will be
	// forwarded until the mcp server with the kubernetes-authorization token
	mdw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCtx := authFromRequest(r.Context(), r)
			r = r.WithContext(authCtx)
			next.ServeHTTP(w, r)
		})
	}

	handlerWithAuthCtx := mdw(handler)
	return http.ListenAndServe(m.addr, handlerWithAuthCtx)
}

// RegisterTool registers a new tool on the MCPHealthServer
func (m *MCPHealthServer) RegisterTool(t *mcp.Tool, handler mcp.ToolHandlerFor[any, any]) {
	mcp.AddTool(m.server, t, handler)
}

func authFromRequest(ctx context.Context, r *http.Request) context.Context {
	authHeaderValue := r.Header.Get(string(authHeaderStr))
	token, found := strings.CutPrefix(authHeaderValue, "Bearer ")
	if !found {
		slog.Error("Failed to parse kubernetes-authorization header. Prefix Bearer not found.")
		return ctx
	}
	return context.WithValue(ctx, authHeaderStr, token)
}

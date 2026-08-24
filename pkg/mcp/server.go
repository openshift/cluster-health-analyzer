package mcp

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type authHeader string

const authHeaderStr authHeader = "kubernetes-authorization"

// MCPHealthServer is a helper and wrapper type
// providing basic methods to run the underlying SSE server
// and to register tools
type MCPHealthServer struct {
	server        *mcp.Server
	addr          string
	tlsCertFile   string
	tlsKeyFile    string
	tokenReviewer *TokenReviewer
}

type MCPHealthServerCfg struct {
	Name    string
	Version string
	Url     string

	PrometheusURL   string
	AlertManagerURL string

	TLSCertFile string
	TLSKeyFile  string

	// DisableAuthForTesting skips TokenReview validation.
	// Only intended for local development.
	DisableAuthForTesting bool
}

// NewMCPHealthServer returns an instance of the MCPHealthServer
func NewMCPHealthServer(cfg MCPHealthServerCfg) (*MCPHealthServer, error) {
	impl := mcp.Implementation{
		Name:    cfg.Name,
		Version: cfg.Version,
	}

	server := mcp.NewServer(&impl, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{
				ListChanged: false,
			},
		},
	})

	incTool := NewIncidentsTool(cfg.PrometheusURL, cfg.AlertManagerURL)
	// get_incidents
	mcp.AddTool(server, &incTool.Tool, mcp.ToolHandlerFor[GetIncidentsParams, any](incTool.IncidentsHandler))

	var reviewer *TokenReviewer
	if !cfg.DisableAuthForTesting {
		restCfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, errors.Join(errors.New("failed to build in-cluster config for token review"), err)
		}
		clientset, err := kubernetes.NewForConfig(restCfg)
		if err != nil {
			return nil, errors.Join(errors.New("failed to create kubernetes client for token review"), err)
		}
		reviewer = NewTokenReviewer(clientset)
		slog.Info("TokenReview authentication enabled")
	} else {
		slog.Warn("Authentication is disabled — do not use in production")
	}

	return &MCPHealthServer{
		server:        server,
		addr:          cfg.Url,
		tlsCertFile:   cfg.TLSCertFile,
		tlsKeyFile:    cfg.TLSKeyFile,
		tokenReviewer: reviewer,
	}, nil
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
	// forwarded until the mcp server with the kubernetes-authorization token.
	// When a TokenReviewer is configured, the token is validated via the
	// Kubernetes TokenReview API before the request is forwarded.
	mdw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCtx := authFromRequest(r.Context(), r)

			if m.tokenReviewer != nil {
				token, err := getTokenFromCtx(authCtx)
				if err != nil {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				if err := m.tokenReviewer.ValidateToken(r.Context(), token); err != nil {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}

			r = r.WithContext(authCtx)
			next.ServeHTTP(w, r)
		})
	}

	handlerWithAuthCtx := mdw(handler)

	if (m.tlsCertFile == "") != (m.tlsKeyFile == "") {
		return errors.New("both TLS certificate and private key files must be configured")
	}

	if m.tlsCertFile != "" && m.tlsKeyFile != "" {
		slog.Info("TLS enabled for MCP server")
		tlsServer := &http.Server{
			Addr:    m.addr,
			Handler: handlerWithAuthCtx,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		}
		return tlsServer.ListenAndServeTLS(m.tlsCertFile, m.tlsKeyFile)
	}

	slog.Warn("TLS is not configured, serving over plaintext HTTP")
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

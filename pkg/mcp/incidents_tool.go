package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// IncidentsTool create a new MCP tool for the incidents
func IncidentsTool() mcp.Tool {
	readonly := true
	return mcp.Tool{
		Name: "get_incidents",
		Description: `List the current firing incidents in the cluster. 
		One incident is a group of related alerts that are likely triggered by the same root cause.
		Use this tool to analyze the cluster health status and determine why a component is failing or degraded.`,
		Annotations: mcp.ToolAnnotation{
			Title:        "Provides information about Incidents in the cluster",
			ReadOnlyHint: &readonly,
		},
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: make(map[string]interface{}),
		},
	}
}

// IncidentsHandler is the main handler for the Incidents. It connects to the
// in-cluster Prometheus and queries the Incidents metrics.
func IncidentsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slog.Info("Incidents tool received request with ", "params", request.Params, "and arguments ", request.Params.Arguments)
	token, err := tokenFromCtx(ctx)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}

	promURL := os.Getenv("PROM_URL")
	promCli, err := prom.NewPrometheusClientWithToken(promURL, token)
	if err != nil {
		slog.Error("Failed to initialize Prometheus client", "error", err)
		return nil, err
	}

	promAPI := v1.NewAPI(promCli)
	val, warning, err := promAPI.Query(ctx, "cluster:health:components:map{}", time.Now())
	if err != nil {
		slog.Error("Recieved error response from Prometheus", "error", err)
		return nil, err
	}
	if warning != nil {
		slog.Warn("Prometheus query response", "warning", warning)
	}

	return mcp.NewToolResultText(val.String()), nil
}

// tokenFromCtx gets the authorization header from the
// provide context
func tokenFromCtx(ctx context.Context) (string, error) {
	k8sToken := ctx.Value(authHeaderStr)
	k8TokenStr, ok := k8sToken.(string)
	if !ok {
		return "", fmt.Errorf("failed to convert the authorization token to string")
	}
	return k8TokenStr, nil
}

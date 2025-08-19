package mcp

import (
	"log/slog"

	"github.com/openshift/cluster-health-analyzer/pkg/mcp"
	"github.com/spf13/cobra"
)

var MCPCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run the MCP server providing tools reporting the cluster health data",
	Run: func(cmd *cobra.Command, args []string) {
		mcpServer := mcp.NewMCPSSEServer("cluster-health-mcp-server", "0.0.1", ":8085")
		incidentTool := mcp.NewIncidentsTool()
		mcpServer.RegisterTool(incidentTool.Tool, incidentTool.IncidentsHandler)

		err := mcpServer.Start()
		if err != nil {
			slog.Error("Failed to start the MCP server", "error", err)
			return
		}
	},
}

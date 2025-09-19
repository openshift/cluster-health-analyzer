package mcp

import (
	"log/slog"
	"os"

	"github.com/openshift/cluster-health-analyzer/pkg/mcp"
	"github.com/spf13/cobra"
)

var (
	promURL         string
	alertManagerURL string
)

var MCPCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run the MCP server providing tools reporting the cluster health data",
	Run: func(cmd *cobra.Command, args []string) {

		if promURL == "" {
			if value, ok := os.LookupEnv("PROM_URL"); ok {
				promURL = value
			} else {
				promURL = "http://localhost:9090"
			}
		}

		if alertManagerURL == "" {
			if value, ok := os.LookupEnv("ALERTMANAGER_URL"); ok {
				alertManagerURL = value
			} else {
				alertManagerURL = "http://localhost:9093"
			}
		}

		mcpServer := mcp.NewMCPSSEServer("cluster-health-mcp-server", "0.0.1", ":8085")
		incidentTool := mcp.NewIncidentsTool(promURL, alertManagerURL)
		mcpServer.RegisterTool(incidentTool.Tool, incidentTool.IncidentsHandler)

		err := mcpServer.Start()
		if err != nil {
			slog.Error("Failed to start the MCP server", "error", err)
			return
		}
	},
}

func init() {
	MCPCmd.Flags().StringVarP(&promURL, "prom-url", "u", "", "URL of the Prometheus server")
	MCPCmd.Flags().StringVar(&alertManagerURL, "alertmanager-url", "", "URL of the AlertManager server")
}

package mcp

import (
	"log/slog"
	"os"

	"github.com/openshift/cluster-health-analyzer/pkg/mcp"
	"github.com/spf13/cobra"
)

const (
	serverName    = "cluster-health-mcp-server"
	serverVersion = "0.0.1"
)

var (
	promURL         string
	alertManagerURL string

	MCPCmd = &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server providing tools reporting the cluster health data",
		Run: func(cmd *cobra.Command, args []string) {
			serverCfg := mcp.MCPHealthServerCfg{
				Name:            serverName,
				Version:         serverVersion,
				Url:             ":8085",
				PrometheusURL:   resolveURL(promURL, "PROM_URL", "http://localhost:9090"),
				AlertManagerURL: resolveURL(alertManagerURL, "ALERTMANAGER_URL", "http://localhost:9093"),
			}

			server := mcp.NewMCPHealthServer(serverCfg)

			if err := server.Start(); err != nil {
				slog.Error("Failed to start the MCP server", "error", err)
				os.Exit(1)
			}
		},
	}
)

func init() {
	MCPCmd.Flags().StringVarP(&promURL, "prom-url", "u", "", "URL of the Prometheus server")
	MCPCmd.Flags().StringVar(&alertManagerURL, "alertmanager-url", "", "URL of the AlertManager server")
}

func resolveURL(flagValue, envKey, defaultValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if value, ok := os.LookupEnv(envKey); ok {
		return value
	}
	return defaultValue
}

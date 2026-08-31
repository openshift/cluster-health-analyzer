package mcp

import (
	"log/slog"
	"os"

	"github.com/openshift/cluster-health-analyzer/pkg/mcp"
	"github.com/spf13/cobra"
)

var (
	MCPServerName    = "cluster-health-mcp-server"
	MCPServerTitle   = "Cluster Health Analyzer MCP Server"
	MCPServerVersion = "0.0.1"
)

var (
	promURL                string
	alertManagerURL        string
	tlsCertFile            string
	tlsKeyFile             string
	disableAuthForTesting  bool
)

var (
	MCPCmd = &cobra.Command{
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

			serverCfg := mcp.MCPHealthServerCfg{
				Name:                 MCPServerName,
				Version:              MCPServerVersion,
				Url:                  ":8085",
				PrometheusURL:        promURL,
				AlertManagerURL:      alertManagerURL,
				TLSCertFile:          tlsCertFile,
				TLSKeyFile:           tlsKeyFile,
				DisableAuthForTesting: disableAuthForTesting,
			}

			server, err := mcp.NewMCPHealthServer(serverCfg)
			if err != nil {
				slog.Error("Failed to initialize the MCP server", "error", err)
				return
			}

			err = server.Start()
			if err != nil {
				slog.Error("Failed to start the MCP server", "error", err)
				return
			}
		},
	}
)

func init() {
	MCPCmd.Flags().StringVarP(&promURL, "prom-url", "u", "", "URL of the Prometheus server")
	MCPCmd.Flags().StringVar(&alertManagerURL, "alertmanager-url", "", "URL of the AlertManager server")
	MCPCmd.Flags().StringVar(&tlsCertFile, "tls-cert-file", "", "Path to the TLS certificate file")
	MCPCmd.Flags().StringVar(&tlsKeyFile, "tls-private-key-file", "", "Path to the TLS private key file")
	MCPCmd.Flags().BoolVar(&disableAuthForTesting, "disable-auth-for-testing", false, "Disable token authentication (for local development only)")
}

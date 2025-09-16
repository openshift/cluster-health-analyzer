package mcp

import (
	"context"
	"log"

	"github.com/openshift/cluster-health-analyzer/pkg/mcp"
	"github.com/spf13/cobra"
)

var (
	MCPServerName    = "cluster-health-mcp-server"
	MCPServerTitle   = "Cluster Health Analyzer MCP Server"
	MCPServerVersion = "0.0.1"
)

var (
	MCPCmd = &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server providing tools reporting the cluster health data",
		Run: func(cmd *cobra.Command, args []string) {
			server := mcp.NewMCPHealthServer(MCPServerName, MCPServerVersion, ":8085")

			if err := server.Start(context.Background()); err != nil {
				log.Fatal(err)
			}
		},
	}
)

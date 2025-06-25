package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift/cluster-health-analyzer/cmd/mcp"
	"github.com/openshift/cluster-health-analyzer/cmd/serve"
	"github.com/openshift/cluster-health-analyzer/cmd/simulate"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cluster-health-analyzer",
	Short: "Health analyzer for OpenShift clusters",
	Long:  ``,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(simulate.SimulateCmd)
	rootCmd.AddCommand(serve.ServeCmd)
	rootCmd.AddCommand(mcp.MCPCmd)
}

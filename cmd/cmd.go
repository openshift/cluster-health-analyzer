package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/openshift/cluster-health-analyzer/pkg/server"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cluster-health-analyzer",
	Short: "Health analyzer for OpenShift clusters",
	Long:  ``,
}

// Refresh interval in seconds.
var interval int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the server",
	Long:  "Start the server to expose the metrics for the health analyzer",
	Run: func(cmd *cobra.Command, args []string) {
		server.StartServer(time.Duration(float64(interval) * float64(time.Second)))
	},
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
	serveCmd.Flags().IntVarP(&interval, "interval", "i", 30, "Refresh interval in seconds")

	rootCmd.AddCommand(serveCmd)
}

package cmd

import (
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/openshift/cluster-health-analyzer/cmd/simulate"
	"github.com/openshift/cluster-health-analyzer/pkg/server"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cluster-health-analyzer",
	Short: "Health analyzer for OpenShift clusters",
	Long:  ``,
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the server",
	Long:  "Start the server to expose the metrics for the health analyzer",
	Run: func(cmd *cobra.Command, args []string) {
		url := cmd.Flags().Lookup("url").DefValue
		if value, ok := os.LookupEnv("PROM_URL"); ok {
			url = value
		}
		if cmd.Flags().Changed("url") {
			url, _ = cmd.Flags().GetString("url")
		}

		seconds, _ := strconv.Atoi(cmd.Flags().Lookup("interval").DefValue)
		if env, ok := os.LookupEnv("REFRESH_INTERVAL"); ok {
			seconds, _ = strconv.Atoi(env)
		}
		if cmd.Flags().Changed("interval") {
			seconds, _ = cmd.Flags().GetInt("interval")
		}
		interval := time.Duration(float64(seconds) * float64(time.Second))

		slog.Info("Parameters", "interval", interval.String(), "url", url)

		server.StartServer(interval, url)
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
	serveCmd.Flags().IntP("interval", "i", 30, "refresh interval in seconds")
	serveCmd.Flags().StringP("url", "u", "http://localhost:9090", "URL of the Prometheus server")

	rootCmd.AddCommand(simulate.SimulateCmd)
	rootCmd.AddCommand(serveCmd)
}

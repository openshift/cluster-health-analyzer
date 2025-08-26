package serve

import (
	"log"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	genericoptions "k8s.io/apiserver/pkg/server/options"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
	"github.com/openshift/cluster-health-analyzer/pkg/server"
)

var ServeCmd = newServeCmd()

func newServeCmd() *cobra.Command {
	opts := newOptions()
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the server",
		Long:  "Start the server to expose the metrics for the health analyzer",
		Run: func(cmd *cobra.Command, args []string) {
			interval := time.Duration(float64(opts.RefreshInterval) * float64(time.Second))
			apiServer, err := buildServer(opts)
			if err != nil {
				log.Fatal("Error building a server", err)
			}

			slog.Info("Parameters", "refresh-interval", interval, "prom-url", opts.PromURL, "alertmanager-url", opts.AlertManagerURL)

			server.StartServer(interval, apiServer, opts)
		},
	}
	cmd.Flags().AddFlagSet(opts.Flags())
	return cmd
}

// newOptions initializes default values for the command options.
func newOptions() common.Options {
	refreshInterval := 30
	if env, ok := os.LookupEnv("REFRESH_INTERVAL"); ok {
		refreshInterval, _ = strconv.Atoi(env)
	}

	promURL := "http://localhost:9090"
	if value, ok := os.LookupEnv("PROM_URL"); ok {
		promURL = value
	}

	alertManagerURL := "http://localhost:9093"
	if value, ok := os.LookupEnv("ALERTMANAGER_URL"); ok {
		alertManagerURL = value
	}

	secureServingOptions := genericoptions.NewSecureServingOptions().WithLoopback()
	secureServingOptions.BindPort = 8443

	return common.Options{
		RefreshInterval: refreshInterval,
		PromURL:         promURL,
		AlertManagerURL: alertManagerURL,
	}
}

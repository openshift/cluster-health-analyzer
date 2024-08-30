package serve

import (
	"log"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	genericoptions "k8s.io/apiserver/pkg/server/options"

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

			slog.Info("Parameters", "refresh-interval", interval, "prom-url", opts.PromURL)

			server.StartServer(interval, opts.PromURL, apiServer)
		},
	}
	cmd.Flags().AddFlagSet(opts.flags())
	return cmd
}

type options struct {
	// Refresh interval in seconds.
	RefreshInterval int

	PromURL string

	// Path to the kube-config file.
	Kubeconfig string

	CertFile string
	CertKey  string

	// Only to be used to for testing.
	DisableAuthForTesting bool
}

// newOptions initializes default values for the command options.
func newOptions() options {
	refreshInterval := 30
	if env, ok := os.LookupEnv("REFRESH_INTERVAL"); ok {
		refreshInterval, _ = strconv.Atoi(env)
	}

	promURL := "http://localhost:9090"
	if value, ok := os.LookupEnv("PROM_URL"); ok {
		promURL = value
	}

	secureServingOptions := genericoptions.NewSecureServingOptions().WithLoopback()
	secureServingOptions.BindPort = 8443

	return options{
		RefreshInterval: refreshInterval,
		PromURL:         promURL,
	}
}

// flags returns supported cli flags for the options.
func (o *options) flags() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	fs.IntVarP(&o.RefreshInterval, "refresh-interval", "i", o.RefreshInterval,
		"Refresh interval in seconds")
	fs.StringVarP(&o.PromURL, "prom-url", "u", o.PromURL,
		"URL of the Prometheus server")
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig,
		"The path to the kubeconfig (defaults to in-cluster config)")

	fs.StringVar(&o.CertFile, "tls-cert-file", "", "The path to the server certificate")
	fs.StringVar(&o.CertKey, "tls-private-key", "", "The path to the server key")

	fs.BoolVar(&o.DisableAuthForTesting, "disable-auth-for-testing", o.DisableAuthForTesting,
		"Flag for testing purposes to disable auth")
	return fs
}

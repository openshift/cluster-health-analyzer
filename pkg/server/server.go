// Package server contains the core logic for starting processor
// and serving resulting metrics.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert/yaml"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
	"github.com/openshift/cluster-health-analyzer/pkg/health"
	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
)

const (
	// HistoryLookback is the number of days to look back for alerts.
	// This is used to build the groups collection to match against.
	historyLookback = 4 * 24 * time.Hour

	// default file path for the configuration of components for
	// health evaluation
	defaultComponentsConfigPath = "/etc/config/components.yaml"
)

var (
	healthMapMetrics = prom.NewMetricSet(
		processor.ClusterHealthComponentsMap,
		"Cluster health components mapping.",
	)
	componentsMetrics = prom.NewMetricSet(
		"cluster:health:components",
		"Cluster components and their ranking.",
	)
	groupSeverityCountMetrics = prom.NewMetricSet(
		"cluster:health:group_severity:count",
		"Current counts of group_ids by severity.",
	)

	componentHealthAlerts = prom.NewMetricSet(
		"component_health_alert",
		"Health status of a component based on alerts",
	)

	componentHealthObjects = prom.NewMetricSet(
		"component_health_object",
		"Health status of a component based on Kubernetes objects",
	)
	componentsHealth = prom.NewMetricSet(
		"component_health",
		"Health status of a component based on the child objects",
	)
)

// Server is the interface for serving the metrics.
type Server interface {
	// Handle registers a handler for the given pattern, similar to http.Handle.
	Handle(pattern string, handler http.Handler)

	// Start starts the server and blocks until the server is stopped.
	Start(ctx context.Context) error
}

// StartServer starts processing the metrics and serving them
// on the /metrics endpoint.
func StartServer(interval time.Duration, server Server, options common.Options) {
	slog.Info("Starting server")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !options.DisableComponentsHealth {
		componentsPath := defaultComponentsConfigPath
		if options.ComponentsPath != "" {
			componentsPath = options.ComponentsPath
		}
		conf, err := loadConfig(componentsPath)
		if err != nil {
			slog.Error("Failed to load config ", "error", err)
			return
		}
		componentsProc, err := health.NewHealthProcessor(interval,
			componentHealthAlerts, componentHealthObjects, componentsHealth, options.Kubeconfig, conf)
		if err != nil {
			slog.Info("Failed to create component procesor, terminating", "err", err)
			return
		}
		componentsProc.Start(ctx)
	} else {
		slog.Info("Components health evaluation is disabled")
	}

	if !options.DisableIncidents {
		processor, err := processor.NewProcessor(healthMapMetrics, componentsMetrics, groupSeverityCountMetrics, interval, options.PromURL)
		if err != nil {
			slog.Error("Failed to create processor, terminating", "err", err)
			return
		}

		end := time.Now()
		start := end.Add(-1 * historyLookback)
		step := time.Minute
		err = processor.InitGroupsCollection(ctx, start, end, step)
		if err != nil {
			slog.Error("Failed to initialize groups collection, terminating", "err", err)
			return
		}

		processor.Start(ctx)
	} else {
		slog.Info("Incident detection is disabled")
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(healthMapMetrics)
	reg.MustRegister(componentsMetrics)
	reg.MustRegister(groupSeverityCountMetrics)
	reg.MustRegister(componentHealthAlerts)
	reg.MustRegister(componentHealthObjects)
	reg.MustRegister(componentsHealth)

	slog.Info("Serving metrics")

	server.Handle("/metrics",
		promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	err := server.Start(ctx)
	if err != nil {
		slog.Error("Failed to run server", "err", err)
	}
}

// loadConfig reads the file
// and unmarshals the component config.
func loadConfig(filePath string) (*health.ComponentsConfig, error) {
	conf := &health.ComponentsConfig{}
	cData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(cData, conf)
	if err != nil {
		return nil, err
	}
	slog.Info("Successfully loaded components definition from ", "path", filePath)
	return conf, nil
}

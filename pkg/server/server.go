// Package server contains the core logic for starting processor
// and serving resulting metrics.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
)

const (
	// HistoryLookback is the number of days to look back for alerts.
	// This is used to build the groups collection to match against.
	historyLookback = 4 * 24 * time.Hour
)

var (
	healthMapMetrics = prom.NewMetricSet(
		"cluster:health:components:map",
		"Cluster health components mapping.",
	)
	componentsMetrics = prom.NewMetricSet(
		"cluster:health:components",
		"Cluster components and their ranking.",
	)
	severityCountsMetrics = prom.NewMetricSet(
		"cluster:health:group_severity_count",
		"Current counts of group_ids by severity.",
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
func StartServer(interval time.Duration, prometheusURL string, server Server) {
	slog.Info("Starting server")

	processor, err := processor.NewProcessor(healthMapMetrics, componentsMetrics, severityCountsMetrics, interval, prometheusURL)
	if err != nil {
		slog.Error("Failed to create processor, terminating", "err", err)
		return
	}

	end := time.Now()
	start := end.Add(-1 * historyLookback)
	step := time.Minute
	err = processor.InitGroupsCollection(context.Background(), start, end, step)
	if err != nil {
		slog.Error("Failed to initialize groups collection, terminating", "err", err)
		return
	}

	processor.Start(context.Background())

	reg := prometheus.NewRegistry()
	reg.MustRegister(healthMapMetrics)
	reg.MustRegister(componentsMetrics)
	reg.MustRegister(severityCountsMetrics)

	slog.Info("Serving metrics")

	server.Handle("/metrics",
		promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	err = server.Start(context.Background())
	if err != nil {
		slog.Error("Failed to run server", "err", err)
	}
}

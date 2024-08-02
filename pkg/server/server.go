// Package server contains the core logic for starting processor
// and serving resulting metrics.
package server

import (
	"context"
	"fmt"
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
)

// StartServer starts processing the metrics and serving them
// on the /metrics endpoint.
func StartServer(interval time.Duration) {
	fmt.Println("Starting server")
	processor, err := processor.NewProcessor(healthMapMetrics, componentsMetrics, interval)
	if err != nil {
		fmt.Println(err)
		return
	}

	end := time.Now()
	start := end.Add(-1 * historyLookback)
	step := time.Minute
	err = processor.InitGroupsCollection(context.Background(), start, end, step)
	if err != nil {
		fmt.Println(err)
		return
	}

	processor.Start(context.Background())

	reg := prometheus.NewRegistry()
	reg.MustRegister(healthMapMetrics)
	reg.MustRegister(componentsMetrics)

	http.Handle(
		"/metrics",
		promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
	)
	http.ListenAndServe(":8080", nil)
}

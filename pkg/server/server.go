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

func StartServer(interval time.Duration) {
	fmt.Println("Starting server")
	processor, err := processor.NewProcessor(healthMapMetrics, componentsMetrics, interval)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Load alerts for the last 4 days to build the groups collection to match against.
	start := time.Now().Add(-4 * 24 * time.Hour)
	end := time.Now()
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

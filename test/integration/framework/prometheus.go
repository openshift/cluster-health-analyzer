package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/prometheus/common/model"
)

// PrometheusClient wraps the existing prom.Loader for integration testing.
type PrometheusClient struct {
	loader prom.Loader
}

// NewPrometheusClient creates a client for querying Thanos/Prometheus.
// Reuses the existing pkg/prom infrastructure.
func NewPrometheusClient(prometheusURL, token string) (*PrometheusClient, error) {
	var loader prom.Loader
	var err error

	if token != "" {
		loader, err = prom.NewLoaderWithToken(prometheusURL, token)
	} else {
		loader, err = prom.NewLoader(prometheusURL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus loader: %w", err)
	}

	return &PrometheusClient{loader: loader}, nil
}

// Alert represents a Prometheus alert as a simple label map.
type Alert map[string]string

// GetAlerts returns all firing alerts matching the alertname pattern.
// Pattern can be exact ("MyAlert") or regex ("MyAlert.*").
// If queryTime is zero, uses time.Now().
func (p *PrometheusClient) GetAlerts(ctx context.Context, alertnamePattern string, queryTime time.Time) ([]Alert, error) {
	if queryTime.IsZero() {
		queryTime = time.Now()
	}
	query := fmt.Sprintf(`ALERTS{alertname=~"%s", alertstate="firing"}`, alertnamePattern)
	results, err := p.loader.LoadQuery(ctx, query, queryTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts: %w", err)
	}

	alerts := make([]Alert, 0, len(results))
	for _, ls := range results {
		alerts = append(alerts, Alert(labelSetToMap(ls)))
	}
	return alerts, nil
}

// Incident represents a processed incident from cluster_health_components_map as a simple label map.
type Incident map[string]string

// GetIncidents returns all incidents matching the alertname pattern.
// Pattern can be exact ("MyAlert") or regex ("MyAlert.*").
// If queryTime is zero, uses time.Now().
func (p *PrometheusClient) GetIncidents(ctx context.Context, alertnamePattern string, queryTime time.Time) ([]Incident, error) {
	if queryTime.IsZero() {
		queryTime = time.Now()
	}
	query := fmt.Sprintf(`%s{src_alertname=~"%s"}`, processor.ClusterHealthComponentsMap, alertnamePattern)
	results, err := p.loader.LoadQuery(ctx, query, queryTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query incidents: %w", err)
	}

	incidents := make([]Incident, 0, len(results))
	for _, ls := range results {
		incidents = append(incidents, Incident(labelSetToMap(ls)))
	}
	return incidents, nil
}

// labelSetToMap converts a model.LabelSet to a plain string map.
func labelSetToMap(ls model.LabelSet) map[string]string {
	result := make(map[string]string, len(ls))
	for k, v := range ls {
		result[string(k)] = string(v)
	}
	return result
}

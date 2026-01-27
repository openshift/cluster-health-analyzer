package simulate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultPrometheusPods are the default Prometheus pods to try (in order).
var DefaultPrometheusPods = []string{"prometheus-k8s-0", "prometheus-k8s-1"}

// Injector injects simulated alerts into Prometheus.
type Injector struct {
	projectRoot    string
	prometheusNS   string
	prometheusPods []string
}

// NewInjector creates an injector for the given Prometheus instance.
// Uses DefaultPrometheusPods for fallback if one pod is unavailable.
func NewInjector(prometheusNS string) (*Injector, error) {
	return NewInjectorWithPods(prometheusNS, DefaultPrometheusPods...)
}

// NewInjectorWithPods creates an injector with specific Prometheus pods.
// Pods are tried in order - if copying to the first pod fails, the next is tried.
func NewInjectorWithPods(prometheusNS string, prometheusPods ...string) (*Injector, error) {
	if len(prometheusPods) == 0 {
		prometheusPods = DefaultPrometheusPods
	}

	root, err := findProjectRoot()
	if err != nil {
		return nil, err
	}
	return &Injector{
		projectRoot:    root,
		prometheusNS:   prometheusNS,
		prometheusPods: prometheusPods,
	}, nil
}

// InjectionResult contains information about the injection for querying.
type InjectionResult struct {
	// QueryTime is the time to use when querying for the injected alerts.
	// Alerts with maxEnd exist at approximately this time.
	QueryTime time.Time

	// UsedPod is the Prometheus pod that was successfully used for injection.
	UsedPod string
}

// WipePrometheusData deletes all Prometheus data and restarts the pods.
// This guarantees a clean slate before running tests.
// WARNING: This destroys ALL metrics data. Only use on dedicated test clusters.
func (i *Injector) WipePrometheusData() error {
	return WipePrometheusData(i.prometheusNS, i.prometheusPods)
}

// Inject takes a scenario and injects only ALERTS into Prometheus.
// It handles the full workflow: CSV -> simulate (alerts-only) -> TSDB blocks -> copy to Prometheus.
// The cluster_health_components_map metrics are NOT injected - they should be computed
// by the cluster-health-analyzer being tested.
// Returns InjectionResult with the query time to use for finding the alerts.
func (i *Injector) Inject(scenario *ScenarioBuilder) (*InjectionResult, error) {
	return i.injectWithOptions(scenario, true)
}

// InjectFull takes a scenario and injects ALL metrics into Prometheus.
// This includes ALERTS, cluster_health_components, and cluster_health_components_map.
// Use this when you want pre-computed metrics (e.g., for testing queries).
func (i *Injector) InjectFull(scenario *ScenarioBuilder) (*InjectionResult, error) {
	return i.injectWithOptions(scenario, false)
}

func (i *Injector) injectWithOptions(scenario *ScenarioBuilder, alertsOnly bool) (*InjectionResult, error) {
	tmpDir, err := os.MkdirTemp("", "simulate-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	scenarioFile := filepath.Join(tmpDir, "scenario.csv")
	openmetricsFile := filepath.Join(tmpDir, "openmetrics.txt")
	dataDir := filepath.Join(tmpDir, "data")

	// Step 1: Write scenario CSV
	if err := scenario.WriteCSV(scenarioFile); err != nil {
		return nil, fmt.Errorf("failed to write scenario: %w", err)
	}

	// Step 2: Run simulate command (with --alerts-only when testing the analyzer)
	if err := runSimulate(i.projectRoot, scenarioFile, openmetricsFile, alertsOnly); err != nil {
		return nil, fmt.Errorf("failed to run simulate: %w", err)
	}

	// Capture time after simulate - alerts with maxEnd exist at approximately this time
	queryTime := time.Now()

	// Step 3: Create TSDB blocks
	if err := CreateTSDBBlocks(openmetricsFile, dataDir); err != nil {
		return nil, fmt.Errorf("failed to create TSDB blocks: %w", err)
	}

	// Step 4: Copy to Prometheus (with fallback to other pods)
	usedPod, err := CopyBlocksToPrometheusWithFallback(dataDir, i.prometheusNS, i.prometheusPods)
	if err != nil {
		return nil, fmt.Errorf("failed to copy blocks: %w", err)
	}

	return &InjectionResult{
		QueryTime: queryTime,
		UsedPod:   usedPod,
	}, nil
}

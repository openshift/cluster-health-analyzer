package processor

// This file contains logic for mapping prometheus alerts to component health maps.

import (
	"slices"
	"strings"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
)

// MapAlerts maps prometheus alerts to component health maps.
func MapAlerts(alerts []prom.Alert) []ComponentHealthMap {
	healthMaps := make([]ComponentHealthMap, 0, len(alerts))
	for _, alert := range alerts {
		healthMap := getAlertHealthMap(alert)
		healthMaps = append(healthMaps, healthMap)
	}
	return healthMaps
}

// getAlertHealthMap maps a prometheus alert to a component health map.
func getAlertHealthMap(a prom.Alert) ComponentHealthMap {
	// Check if alert is a node alert
	layer, component, labels := determineComponent(a)

	healthMap := ComponentHealthMap{
		Layer:     layer,
		Component: component,
		SrcType:   Alert,
		SrcLabels: labels,
	}

	healthMap.GroupId = a.Labels["group_id"]

	updateHealthValue(a, &healthMap)

	return healthMap
}

// determineComponent determines the component of a prometheus alert.
//
// It uses various strategies to determine the component.
func determineComponent(a prom.Alert) (layer, component string, labels map[string]string) {
	// Check if alert is a node alert.
	return evalMatcherFns([]componentMatcherFn{
		cvoAlertsMatcher,
		computeMatcher,
		coreMatcher,
		workloadMatcher,
	}, a.Labels)
}

var cvoAlerts = []string{"ClusterOperatorDown", "ClusterOperatorDegraded"}

func cvoAlertsMatcher(labels map[string]string) (layer, comp string, keys []string) {
	if slices.Contains(cvoAlerts, labels["alertname"]) {
		component := labels["name"]
		if component == "" {
			component = "version"
		}
		return "core", component, nil
	}
	return "", "", nil
}

func computeMatcher(labels map[string]string) (layer, comp string, keys []string) {
	for _, nodeAlert := range nodeAlerts {
		if labels["alertname"] == nodeAlert {
			// TODO: determine node identifier as a component
			// TODO: split nodes to controlplane and worker nodes
			component := "compute"
			return "compute", component, nil
		}
	}
	return "", "", nil
}

func coreMatcher(labels map[string]string) (layer, comp string, keys []string) {
	// Try matching against core components.
	if component, keys := findComponent(coreMatchers, labels); component != "" {
		return "core", component, keys
	}
	return "", "", nil
}

func workloadMatcher(labels map[string]string) (layer, comp string, keys []string) {
	// Try matching against workload components.
	if component, keys := findComponent(workloadMatchers, labels); component != "" {
		return "workload", component, keys
	}
	return "", "", nil
}

func updateHealthValue(a prom.Alert, healthMap *ComponentHealthMap) {
	switch strings.ToLower(a.Labels["severity"]) {
	case "critical":
		healthMap.Health = Critical
	case "warning":
		healthMap.Health = Warning
	case "info":
		healthMap.Health = Healthy
	default:
		// We don't recognize the severity, so we'll default to warning
		healthMap.Health = Warning
	}
}

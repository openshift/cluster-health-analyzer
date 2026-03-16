package framework

import (
	"fmt"
	"strings"

	"github.com/onsi/gomega/types"
)

// ValidLayers contains all valid OpenShift layer values.
// IMPORTANT: Keep in sync with pkg/processor/alerts.go layer definitions.
var ValidLayers = map[string]struct{}{
	"compute":  {},
	"core":     {},
	"workload": {},
}

// ValidComponents contains all valid OpenShift component values.
// IMPORTANT: Keep in sync with pkg/processor/mappings.go (coreMatchers and workloadMatchers).
var ValidComponents = map[string]struct{}{
	// compute layer
	"compute": {},
	// core layer components
	"etcd":                          {},
	"kube-apiserver":                {},
	"kube-controller-manager":       {},
	"kube-scheduler":                {},
	"machine-approver":              {},
	"machine-config":                {},
	"version":                       {},
	"dns":                           {},
	"authentication":                {},
	"cert-manager":                  {},
	"cloud-controller-manager":      {},
	"cloud-credential":              {},
	"cluster-api":                   {},
	"config-operator":               {},
	"kube-storage-version-migrator": {},
	"image-registry":                {},
	"ingress":                       {},
	"console":                       {},
	"insights":                      {},
	"machine-api":                   {},
	"monitoring":                    {},
	"network":                       {},
	"node-tuning":                   {},
	"openshift-apiserver":           {},
	"openshift-controller-manager":  {},
	"openshift-samples":             {},
	"operator-lifecycle-manager":    {},
	"service-ca":                    {},
	"storage":                       {},
	"vertical-pod-autoscaler":       {},
	"marketplace":                   {},
	// workload layer components
	"openshift-compliance":               {},
	"openshift-file-integrity":           {},
	"openshift-logging":                  {},
	"openshift-user-workload-monitoring": {},
	"openshift-gitops":                   {},
	"openshift-operators":                {},
	"kubevirt":                           {},
	"openshift-local-storage":            {},
	"quay":                               {},
	"Argo":                               {},
}

// BeValidIncident returns a Gomega matcher that verifies an Incident has all required labels.
// Required labels: src_alertname, src_severity, src_namespace, component, layer, group_id
// All required labels must have non-empty values.
// Additionally, component and layer must be valid OpenShift values.
func BeValidIncident() types.GomegaMatcher {
	return &validIncidentMatcher{}
}

type validIncidentMatcher struct {
	missing  []string
	empty    []string
	invalid  []string
	incident *Incident
}

var requiredIncidentLabels = []string{
	"src_alertname",
	"src_severity",
	"src_namespace",
	"component",
	"layer",
	"group_id",
}

func (m *validIncidentMatcher) Match(actual interface{}) (bool, error) {
	incident, ok := actual.(*Incident)
	if !ok {
		return false, fmt.Errorf("BeValidIncident expects *Incident, got %T", actual)
	}

	if incident == nil {
		return false, nil
	}

	m.incident = incident
	m.missing = nil
	m.empty = nil
	m.invalid = nil

	for _, label := range requiredIncidentLabels {
		value, exists := incident.Labels[label]
		if !exists {
			m.missing = append(m.missing, label)
		} else if value == "" {
			m.empty = append(m.empty, label)
		}
	}

	// Validate layer is a known OpenShift layer
	if layer, exists := incident.Labels["layer"]; exists && layer != "" {
		if _, ok := ValidLayers[layer]; !ok {
			m.invalid = append(m.invalid, fmt.Sprintf("layer=%q", layer))
		}
	}

	// Validate component is a known OpenShift component
	if component, exists := incident.Labels["component"]; exists && component != "" {
		if _, ok := ValidComponents[component]; !ok {
			m.invalid = append(m.invalid, fmt.Sprintf("component=%q", component))
		}
	}

	return len(m.missing) == 0 && len(m.empty) == 0 && len(m.invalid) == 0, nil
}

func (m *validIncidentMatcher) FailureMessage(actual interface{}) string {
	var parts []string

	if m.incident == nil {
		return "Expected incident to be non-nil"
	}

	if len(m.missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing labels: %v", m.missing))
	}
	if len(m.empty) > 0 {
		parts = append(parts, fmt.Sprintf("empty labels: %v", m.empty))
	}
	if len(m.invalid) > 0 {
		parts = append(parts, fmt.Sprintf("invalid values: %v", m.invalid))
	}

	alertname := m.incident.Labels["src_alertname"]
	if alertname == "" {
		alertname = "(unknown)"
	}

	return fmt.Sprintf("Expected incident %s to be valid, but: %s\nAll labels: %v",
		alertname, strings.Join(parts, "; "), m.incident.Labels)
}

func (m *validIncidentMatcher) NegatedFailureMessage(actual interface{}) string {
	alertname := "(unknown)"
	if m.incident != nil {
		if name := m.incident.Labels["src_alertname"]; name != "" {
			alertname = name
		}
	}
	return fmt.Sprintf("Expected incident %s to not be valid, but it was", alertname)
}

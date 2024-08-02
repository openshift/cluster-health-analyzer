package processor

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
)

// TestAlertsMapAlerts tests the mapping of prometheus alerts to component health maps.
func TestAlertsMapAlerts(t *testing.T) {
	alerts := []prom.Alert{
		{Name: "KubeNodeNotReady", Labels: map[string]string{
			"alertname": "KubeNodeNotReady",
			"namespace": "openshift-monitoring"}},
		{Name: "KubePodCrashLooping", Labels: map[string]string{
			"alertname": "KubePodCrashLooping", "namespace": "openshift-etcd"}},
		{Name: "ClusterOperatorDown", Labels: map[string]string{
			"alertname": "ClusterOperatorDown",
			"namespace": "openshift-cluster-version",
			"name":      "machine-config"}},
	}

	componentsMap := MapAlerts(alerts)

	assert.Equal(t, componentsMap[0].Component, "compute")
	assert.Equal(t, componentsMap[0].Layer, "compute")
	assert.Equal(t, componentsMap[1].Component, "etcd")
	assert.Equal(t, componentsMap[1].Layer, "core")
	assert.Equal(t, componentsMap[2].Component, "machine-config")
	assert.Equal(t, componentsMap[1].Layer, "core")
}

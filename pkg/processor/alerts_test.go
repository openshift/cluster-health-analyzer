package processor

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

// TestAlertsMapAlerts tests the mapping of prometheus alerts to component health maps.
func TestAlertsMapAlerts(t *testing.T) {
	alerts := []model.LabelSet{
		{
			"alertname": "KubeNodeNotReady",
			"namespace": "openshift-monitoring",
			"silenced":  "0",
		},
		{
			"alertname": "KubePodCrashLooping",
			"namespace": "openshift-etcd",
			"silenced":  "0",
		},
		{
			"alertname": "ClusterOperatorDown",
			"namespace": "openshift-cluster-version",
			"name":      "machine-config",
			"silenced":  "1",
		},
	}

	componentsMap := MapAlerts(alerts)

	assert.Equal(t, componentsMap[0].Component, "compute")
	assert.Equal(t, componentsMap[0].Layer, "compute")
	assert.Equal(t, componentsMap[0].Silenced, "0")

	assert.Equal(t, componentsMap[1].Component, "etcd")
	assert.Equal(t, componentsMap[1].Layer, "core")
	assert.Equal(t, componentsMap[1].Silenced, "0")

	assert.Equal(t, componentsMap[2].Component, "machine-config")
	assert.Equal(t, componentsMap[2].Layer, "core")
	assert.Equal(t, componentsMap[2].Silenced, "1")
}

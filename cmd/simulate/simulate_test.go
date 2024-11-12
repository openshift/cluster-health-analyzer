package simulate

import (
	"strings"
	"testing"

	"github.com/openshift/cluster-health-analyzer/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestParseIntervalsFromCSV_ValidInput(t *testing.T) {
	input := `start,end,alertname,namespace,severity,labels
0,60,Watchdog,openshift-monitoring,none,
10,40,ClusterOperatorDegraded,openshift-cluster-version,warning,{"name":"machine-config"}`

	expected := []utils.RelativeInterval{
		{
			Labels: map[string]string{
				"alertname": "Watchdog",
				"namespace": "openshift-monitoring",
				"severity":  "none",
			},
			Start: 0,
			End:   60,
		},
		{
			Labels: map[string]string{
				"alertname": "ClusterOperatorDegraded",
				"namespace": "openshift-cluster-version",
				"severity":  "warning",
				"name":      "machine-config",
			},
			Start: 10,
			End:   40,
		},
	}

	reader := strings.NewReader(input)
	result, err := parseIntervalsFromCSV(reader)

	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestParseIntervalsFromCSV_InvalidStartTime(t *testing.T) {
	input := `start,end,alertname,namespace,severity,labels
invalid,60,Watchdog,openshift-monitoring,warning,`

	reader := strings.NewReader(input)
	_, err := parseIntervalsFromCSV(reader)

	assert.Error(t, err)
}

func TestParseIntervalsFromCSV_InvalidEndTime(t *testing.T) {
	input := `start,end,alertname,namespace,severity,labels
0,invalid,Watchdog,openshift-monitoring,warning,`

	reader := strings.NewReader(input)
	_, err := parseIntervalsFromCSV(reader)

	assert.Error(t, err)
}

func TestParseIntervalsFromCSV_InvalidJSONLabels(t *testing.T) {
	input := `start,end,alertname,namespace,severity,labels
0,60,Watchdog,openshift-monitoring,warning,{invalid:json}`

	reader := strings.NewReader(input)
	_, err := parseIntervalsFromCSV(reader)

	assert.Error(t, err)
}

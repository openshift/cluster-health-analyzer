package processor

import (
	"testing"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/stretchr/testify/assert"
)

func Test_computeSeverityCountMetrics(t *testing.T) {
	tests := []struct {
		name            string
		alertsHealthMap []ComponentHealthMap
		want            []prom.Metric
	}{
		{
			name:            "empty health map returns empty metrics",
			alertsHealthMap: []ComponentHealthMap{},
			want:            []prom.Metric{},
		},
		{
			name: "single alert generates one severity metric",
			alertsHealthMap: []ComponentHealthMap{
				{
					GroupId: "group1",
					Health:  Critical,
				},
			},
			want: []prom.Metric{
				{
					Labels: map[string]string{"severity": "critical"},
					Value:  1,
				},
			},
		},
		{
			name: "multiple alerts with same severity are counted together",
			alertsHealthMap: []ComponentHealthMap{
				{
					GroupId: "group1",
					Health:  Critical,
				},
				{
					GroupId: "group2",
					Health:  Critical,
				},
			},
			want: []prom.Metric{
				{
					Labels: map[string]string{"severity": "critical"},
					Value:  2,
				},
			},
		},
		{
			name: "multiple alerts with different severities generate separate metrics",
			alertsHealthMap: []ComponentHealthMap{
				{
					GroupId: "group1",
					Health:  Critical,
				},
				{
					GroupId: "group2",
					Health:  Warning,
				},
			},
			want: []prom.Metric{
				{
					Labels: map[string]string{"severity": "critical"},
					Value:  1,
				},
				{
					Labels: map[string]string{"severity": "warning"},
					Value:  1,
				},
			},
		},
	}

	p := &processor{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.computeSeverityCountMetrics(tt.alertsHealthMap)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

package processor

import (
	"errors"
	"testing"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/test/mocks"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func Test_computeSeverityCountMetrics_EmptyHealthMap(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{}
	expected := []prom.Metric{}
	p := &processor{}

	actual := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, actual)
}

func Test_computeSeverityCountMetrics_SingleGroup(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{
		{
			GroupId: "group1",
			Health:  Critical,
		},
	}
	expected := []prom.Metric{
		{
			Labels: model.LabelSet{"severity": "critical"},
			Value:  1,
		},
	}
	p := &processor{}

	actual := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, actual)
}

func Test_computeSeverityCountMetrics_ValidHealthValues(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{
		{
			GroupId: "group1",
			Health:  Healthy,
		},
		{
			GroupId: "group2",
			Health:  Warning,
		},
		{
			GroupId: "group3",
			Health:  Critical,
		},
	}
	expected := []prom.Metric{
		{
			Labels: model.LabelSet{"severity": "info"},
			Value:  1,
		},
		{
			Labels: model.LabelSet{"severity": "warning"},
			Value:  1,
		},
		{
			Labels: model.LabelSet{"severity": "critical"},
			Value:  1,
		},
	}
	p := &processor{}

	actual := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, actual)
}

func Test_computeSeverityCountMetrics_MultipleGroupsSameSeverity(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{
		{
			GroupId: "group1",
			Health:  Critical,
		},
		{
			GroupId: "group2",
			Health:  Critical,
		},
	}
	expected := []prom.Metric{
		{
			Labels: model.LabelSet{"severity": "critical"},
			Value:  2,
		},
	}
	p := &processor{}

	actual := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, actual)
}

func Test_computeSeverityCountMetrics_EmptyGroupId(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{
		{
			GroupId: "",
			Health:  Critical,
		},
	}
	expected := []prom.Metric{}
	p := &processor{}

	got := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, got)
}

func Test_computeSeverityCountMetrics_CriticalOverWarning(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{
		{
			GroupId: "group1",
			Health:  Warning,
		},
		{
			GroupId: "group1",
			Health:  Critical,
		},
	}
	expected := []prom.Metric{
		{
			Labels: model.LabelSet{"severity": "critical"},
			Value:  1,
		},
	}
	p := &processor{}

	got := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, got)
}

func Test_computeSeverityCountMetrics_WarningOverInfo(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{
		{
			GroupId: "group1",
			Health:  Warning,
		},
		{
			GroupId: "group1",
			Health:  Healthy,
		},
	}
	expected := []prom.Metric{
		{
			Labels: model.LabelSet{"severity": "warning"},
			Value:  1,
		},
	}
	p := &processor{}

	got := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, got)
}

func Test_computeSeverityCountMetrics_CriticalOverInfo(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{
		{
			GroupId: "group1",
			Health:  Critical,
		},
		{
			GroupId: "group1",
			Health:  Healthy,
		},
	}
	expected := []prom.Metric{
		{
			Labels: model.LabelSet{"severity": "critical"},
			Value:  1,
		},
	}
	p := &processor{}

	got := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, got)
}

func Test_computeSeverityCountMetrics_UnrecognizedHealthValue(t *testing.T) {
	alertsHealthMap := []ComponentHealthMap{
		{
			GroupId: "group1",
			Health:  HealthValue(999),
		},
	}
	expected := []prom.Metric{
		{
			Labels: model.LabelSet{"severity": "none"},
			Value:  1,
		},
	}
	p := &processor{}

	got := p.computeSeverityCountMetrics(alertsHealthMap)

	assert.ElementsMatch(t, expected, got)
}

func Test_evaluateSilences(t *testing.T) {
	type args struct {
		alerts []model.LabelSet
	}

	tests := []struct {
		name            string
		args            args
		silenced        []models.Alert
		alertManagerErr error
		expected        []model.LabelSet
		wantErr         error
	}{
		{
			name: "happy path",
			args: args{
				alerts: []model.LabelSet{
					{
						"alertname": "KubeNodeNotReady",
						"namespace": "openshift-monitoring",
					},
					{
						"alertname": "KubePodCrashLooping",
						"namespace": "openshift-etcd",
					},
				},
			},
			silenced: []models.Alert{
				{
					Labels: map[string]string{
						"alertname": "KubePodCrashLooping",
					},
				},
			},
			expected: []model.LabelSet{
				{
					"alertname": "KubeNodeNotReady",
					"namespace": "openshift-monitoring",
					"silenced":  "false",
				},
				{
					"alertname": "KubePodCrashLooping",
					"namespace": "openshift-etcd",
					"silenced":  "true",
				},
			},
			wantErr: nil,
		},
		{
			name: "unhappy path - alert manager client gets an error",
			args: args{
				alerts: []model.LabelSet{
					{
						"alertname": "KubeNodeNotReady",
						"namespace": "openshift-monitoring",
					},
					{
						"alertname": "KubePodCrashLooping",
						"namespace": "openshift-etcd",
					},
				},
			},
			alertManagerErr: errors.New("alertmanager error"),
			wantErr:         errors.New("alertmanager error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testProcessor := processor{
				amLoader: mocks.NewMockAlertLoader(nil, tt.silenced, tt.alertManagerErr),
			}
			got, err := testProcessor.evaluateSilences(tt.args.alerts)
			assert.Equal(t, tt.expected, got)
			assert.Equal(t, tt.wantErr, err)
		})
	}

}

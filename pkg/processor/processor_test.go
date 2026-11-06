package processor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/alertmanager"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/test/mocks"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	type args struct {
		alerts []model.LabelSet
	}

	tests := []struct {
		name     string
		args     args
		amLoader alertmanager.Loader
		expected []model.LabelSet
		wantErr  error
	}{
		{
			name: "all alerts within same groupID with the same triple (alertname, namespace, severity) are silenced",
			args: args{
				alerts: []model.LabelSet{
					{
						"alertname": "KubePodCrashLooping",
						"namespace": "openshift-monitoring",
						"severity":  "warning",
						"pod":       "foo",
						"group_id":  "group_1",
					},
					{
						"alertname": "KubePodCrashLooping",
						"namespace": "openshift-monitoring",
						"severity":  "warning",
						"pod":       "bar",
						"group_id":  "group_1",
					},
					{
						"alertname": "UpdateAvailable",
						"namespace": "openshift-monitoring",
						"severity":  "info",
						"group_id":  "group_1",
					},
				},
			},
			amLoader: func() alertmanager.Loader {
				silenced := []models.Alert{
					{
						Labels: map[string]string{
							"alertname": "KubePodCrashLooping",
							"namespace": "openshift-monitoring",
							"severity":  "warning",
							"pod":       "foo",
							"group_id":  "group_1",
						},
					},
					{
						Labels: map[string]string{
							"alertname": "KubePodCrashLooping",
							"namespace": "openshift-monitoring",
							"severity":  "warning",
							"pod":       "bar",
							"group_id":  "group_1",
						},
					},
				}
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(silenced, nil)
				return mocked
			}(),
			expected: []model.LabelSet{
				{
					"alertname": "KubePodCrashLooping",
					"namespace": "openshift-monitoring",
					"severity":  "warning",
					"group_id":  "group_1",
					"pod":       "foo",
					"silenced":  "true",
				},
				{
					"alertname": "KubePodCrashLooping",
					"namespace": "openshift-monitoring",
					"group_id":  "group_1",
					"severity":  "warning",
					"pod":       "bar",
					"silenced":  "true",
				},
				{
					"alertname": "UpdateAvailable",
					"namespace": "openshift-monitoring",
					"severity":  "info",
					"group_id":  "group_1",
					"silenced":  "false",
				},
			},
			wantErr: nil,
		},
		{
			name: "not all alerts in the group are silenced",
			args: args{
				alerts: []model.LabelSet{
					{
						"alertname": "KubePodCrashLooping",
						"namespace": "openshift-monitoring",
						"severity":  "warning",
						"pod":       "foo",
						"group_id":  "group_1",
					},
					{
						"alertname": "KubePodCrashLooping",
						"namespace": "openshift-monitoring",
						"severity":  "warning",
						"pod":       "bar",
						"group_id":  "group_1",
					},
				},
			},
			amLoader: func() alertmanager.Loader {
				silenced := []models.Alert{
					{
						Labels: map[string]string{
							"alertname": "KubePodCrashLooping",
							"namespace": "openshift-monitoring",
							"severity":  "warning",
							"group_id":  "group_1",
							"pod":       "foo",
						},
					},
				}
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(silenced, nil)
				return mocked
			}(),
			expected: []model.LabelSet{
				{
					"alertname": "KubePodCrashLooping",
					"namespace": "openshift-monitoring",
					"severity":  "warning",
					"pod":       "foo",
					"group_id":  "group_1",
					"silenced":  "false",
				},
				{
					"alertname": "KubePodCrashLooping",
					"namespace": "openshift-monitoring",
					"severity":  "warning",
					"pod":       "bar",
					"group_id":  "group_1",
					"silenced":  "false",
				},
			},
			wantErr: nil,
		},
		{
			name: "unhappy path, alertmanager error",
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
			amLoader: func() alertmanager.Loader {
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(nil, errors.New("alertmanager error"))
				return mocked
			}(),
			wantErr: errors.New("alertmanager error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testProcessor := processor{
				amLoader: tt.amLoader,
			}
			got, err := testProcessor.evaluateSilences(tt.args.alerts)
			assert.ElementsMatch(t, tt.expected, got)
			assert.Equal(t, tt.wantErr, err)
		})
	}

}

func TestProcessorReloadsMatchersOnRuleChange(t *testing.T) {
	ctrl := gomock.NewController(t)
	loader := mocks.NewMockPrometheusLoader(ctrl)
	am := mocks.NewMockAlertManagerLoader(ctrl)
	original := newTestGroupsCollection(t)
	now := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	original.ProcessAlertsBatch([]model.LabelSet{{"alertname": "A", "namespace": "same", "node": "n0"}}, now.Add(-time.Hour))
	replacement := testRules(t, `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`)
	p := &processor{
		loader: loader, amLoader: am, groupsCollection: original,
		rulesSource:     func() *RulesSnapshot { return replacement },
		historyLookback: 4 * 24 * time.Hour, historyStep: time.Minute,
	}
	loader.EXPECT().LoadAlertsRange(gomock.Any(), now.Add(-4*24*time.Hour), now, time.Minute).Return(prom.RangeVector(nil), nil)
	loader.EXPECT().LoadVectorRange(gomock.Any(), ClusterHealthComponentsMap, now.Add(-4*24*time.Hour), now, time.Minute).Return(prom.RangeVector(nil), nil)
	loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, now).Return([]model.LabelSet{{"alertname": "A", "namespace": "same", "node": "n1"}}, nil)
	loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, now.Add(time.Minute)).Return([]model.LabelSet{{"alertname": "A", "namespace": "same", "node": "n2"}}, nil)
	am.EXPECT().SilencedAlerts().Return(nil, nil).Times(2)
	first, err := p.loadAlerts(context.Background(), now)
	require.NoError(t, err)
	require.NotSame(t, original, p.groupsCollection)
	require.Same(t, replacement, p.groupsCollection.rules)
	second, err := p.loadAlerts(context.Background(), now.Add(time.Minute))
	require.NoError(t, err)
	require.NotEqual(t, first[0]["group_id"], second[0]["group_id"])
	for _, g := range p.groupsCollection.Groups {
		for _, m := range g.Matchers {
			require.Len(t, m.Labels, 1)
			require.Contains(t, m.Labels, model.LabelName("node"))
		}
	}
}

func TestProcessorDoesNotUseMatchersWithoutRules(t *testing.T) {
	ctrl := gomock.NewController(t)
	original := newTestGroupsCollection(t)
	p := &processor{
		loader: mocks.NewMockPrometheusLoader(ctrl), groupsCollection: original,
		rulesSource: func() *RulesSnapshot { return &RulesSnapshot{} },
	}
	_, err := p.loadAlerts(context.Background(), time.Now())
	require.ErrorContains(t, err, "no valid IncidentRules")
	_, err = NewProcessor(ProcessorConfig{}, nil, nil, nil)
	require.ErrorContains(t, err, "source is required")
}

func TestProcessorFailedReloadDoesNotPublishPartialState(t *testing.T) {
	ctrl := gomock.NewController(t)
	loader := mocks.NewMockPrometheusLoader(ctrl)
	original := newTestGroupsCollection(t)
	replacement := testRules(t, `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`)
	p := &processor{
		loader: loader, groupsCollection: original,
		rulesSource:     func() *RulesSnapshot { return replacement },
		historyLookback: 4 * 24 * time.Hour, historyStep: time.Minute,
	}
	loader.EXPECT().LoadAlertsRange(gomock.Any(), gomock.Any(), gomock.Any(), time.Minute).Return(prom.RangeVector(nil), nil)
	loader.EXPECT().LoadVectorRange(gomock.Any(), ClusterHealthComponentsMap, gomock.Any(), gomock.Any(), time.Minute).Return(prom.RangeVector(nil), errors.New("history unavailable"))
	_, err := p.loadAlerts(context.Background(), time.Now())
	require.ErrorContains(t, err, "failed to reload incident matchers")
	require.Same(t, original, p.groupsCollection)
}

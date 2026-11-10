package processor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/alertmanager"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/test/mocks"
	"github.com/openshift/cluster-health-analyzer/pkg/utils"
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
	// A was assigned to an incident by the previous batch under the default rules.
	assigned := original.ProcessAlertsBatch([]model.LabelSet{{"alertname": "A", "namespace": "same", "node": "n0"}}, now.Add(-time.Hour))
	originalID := string(assigned[0]["group_id"])
	replacement := testRules(t, `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`)
	p := &processor{
		loader: loader, amLoader: am, groupsCollection: original,
		rulesSource: func() *RulesSnapshot { return replacement },
		assignedAlerts: []model.LabelSet{
			{"alertname": "A", "namespace": "same", "node": "n0", "group_id": model.LabelValue(originalID)},
		},
	}
	// The rules change is adopted without reclassifying: A keeps its incident,
	// a new alert on the same node joins it, and an alert on another node gets
	// a new incident under the new rules.
	loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, now).Return([]model.LabelSet{
		{"alertname": "A", "namespace": "same", "node": "n0"},
		{"alertname": "C", "namespace": "same", "node": "n0"},
		{"alertname": "D", "namespace": "same", "node": "n1"},
	}, nil)
	am.EXPECT().SilencedAlerts().Return(nil, nil)
	got, err := p.loadAlerts(context.Background(), now)
	require.NoError(t, err)
	require.NotSame(t, original, p.groupsCollection)
	require.Same(t, replacement, p.groupsCollection.rules)
	byName := map[string]string{}
	for _, alert := range got {
		byName[string(alert["alertname"])] = string(alert["group_id"])
	}
	require.Equal(t, originalID, byName["A"], "assigned alert must keep its incident")
	require.Equal(t, originalID, byName["C"], "new alert on the same node may join the existing incident")
	require.NotEqual(t, originalID, byName["D"], "alert on another node must get a new incident")
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
	original := newTestGroupsCollection(t)
	p := &processor{
		loader: mocks.NewMockPrometheusLoader(gomock.NewController(t)), groupsCollection: original,
		rulesSource: func() *RulesSnapshot { return &RulesSnapshot{} },
	}
	_, err := p.loadAlerts(context.Background(), time.Now())
	require.ErrorContains(t, err, "no valid IncidentRules")
	require.Same(t, original, p.groupsCollection)
}

// correlateHistoricalAssignments must produce assignments only from
// unambiguous evidence: when the exported mapping labels cannot distinguish
// two alerts, neither receives a forced assignment.
func TestHistoricalAssignmentCorrelation(t *testing.T) {
	start := model.TimeFromUnixNano(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	alerts := func(a, b model.LabelValue) prom.RangeVector {
		return utils.RelativeIntervalsToRangeVectors([]utils.RelativeInterval{
			{Labels: model.LabelSet{"alertname": a, "namespace": "prod", "severity": "warning", "node": "worker-1"}, Start: 0, End: 120},
			{Labels: model.LabelSet{"alertname": b, "namespace": "prod", "severity": "warning", "node": "worker-2"}, Start: 0, End: 120},
		}, start, time.Minute)
	}
	mappings := func(firstName, first, secondName, second string) prom.RangeVector {
		return utils.RelativeIntervalsToRangeVectors([]utils.RelativeInterval{
			{Labels: model.LabelSet{"group_id": model.LabelValue(first), "src_alertname": model.LabelValue(firstName), "src_namespace": "prod", "src_severity": "warning"}, Start: 0, End: 120},
			{Labels: model.LabelSet{"group_id": model.LabelValue(second), "src_alertname": model.LabelValue(secondName), "src_namespace": "prod", "src_severity": "warning"}, Start: 0, End: 120},
		}, start, time.Minute)
	}

	// Distinct exported alert names: each alert matches exactly one incident.
	assignments := correlateHistoricalAssignments(alerts("A", "B"), mappings("A", "ID-1", "B", "ID-2"))
	require.Len(t, assignments, 2)
	byAlert := map[string]string{}
	for _, a := range assignments {
		byAlert[string(a.labels["alertname"])+string(a.labels["node"])] = a.id
	}
	require.Equal(t, "ID-1", byAlert["Aworker-1"])
	require.Equal(t, "ID-2", byAlert["Bworker-2"])

	// Same exported labels, different incidents: the evidence cannot tell
	// which alert belonged to which incident, so nothing is forced.
	assignments = correlateHistoricalAssignments(alerts("A", "A"), mappings("A", "ID-1", "A", "ID-2"))
	require.Empty(t, assignments)

	// No mapping history: no assignments.
	require.Empty(t, correlateHistoricalAssignments(alerts("A", "B"), nil))
}

// A restart must preserve historical assignments even when the current rules
// would group the alerts differently, and new alerts may join the preserved
// incidents under the current rules.
func TestRestartAfterRuleChangePreservesAssignments(t *testing.T) {
	ctrl := gomock.NewController(t)
	loader := mocks.NewMockPrometheusLoader(ctrl)
	am := mocks.NewMockAlertManagerLoader(ctrl)
	// Node-only rules: replaying from scratch would separate A and B.
	rules := testRules(t, `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`)
	start := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	now := start.Add(3 * time.Hour)
	// A and B shared incident ID-123 under the previous namespace rules.
	alertsRange := utils.RelativeIntervalsToRangeVectors([]utils.RelativeInterval{
		{Labels: model.LabelSet{"alertname": "A", "namespace": "prod", "severity": "warning", "node": "worker-1"}, Start: 0, End: 120},
		{Labels: model.LabelSet{"alertname": "B", "namespace": "prod", "severity": "warning", "node": "worker-2"}, Start: 0, End: 120},
	}, model.TimeFromUnixNano(start.UnixNano()), time.Minute)
	healthMap := utils.RelativeIntervalsToRangeVectors([]utils.RelativeInterval{
		{Labels: model.LabelSet{"group_id": "ID-123", "src_alertname": "A", "src_namespace": "prod", "src_severity": "warning"}, Start: 0, End: 120},
		{Labels: model.LabelSet{"group_id": "ID-123", "src_alertname": "B", "src_namespace": "prod", "src_severity": "warning"}, Start: 0, End: 120},
	}, model.TimeFromUnixNano(start.UnixNano()), time.Minute)
	p := &processor{
		loader: loader, amLoader: am,
		rulesSource: func() *RulesSnapshot { return rules },
	}
	loader.EXPECT().LoadAlertsRange(gomock.Any(), start, now, time.Minute).Return(alertsRange, nil)
	loader.EXPECT().LoadVectorRange(gomock.Any(), ClusterHealthComponentsMap, start, now, time.Minute).Return(healthMap, nil)
	require.NoError(t, p.InitGroupsCollection(context.Background(), start, now, time.Minute))

	// Live batch: A is still firing, C joins A's node, D is on another node.
	loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, now).Return([]model.LabelSet{
		{"alertname": "A", "namespace": "prod", "severity": "warning", "node": "worker-1"},
		{"alertname": "C", "namespace": "prod", "severity": "warning", "node": "worker-1"},
		{"alertname": "D", "namespace": "prod", "severity": "warning", "node": "worker-3"},
	}, nil)
	am.EXPECT().SilencedAlerts().Return(nil, nil)
	got, err := p.loadAlerts(context.Background(), now)
	require.NoError(t, err)
	byName := map[string]string{}
	for _, alert := range got {
		byName[string(alert["alertname"])] = string(alert["group_id"])
	}
	require.Equal(t, "ID-123", byName["A"], "the assigned alert must keep its incident across the restart")
	require.Equal(t, "ID-123", byName["C"], "a new alert on the same node may join the preserved incident")
	require.NotEqual(t, "ID-123", byName["D"], "an alert on another node must get a new incident")
}

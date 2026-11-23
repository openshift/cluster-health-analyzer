package processor

import (
	"context"
	"errors"
	"fmt"
	"maps"
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

// The Watchdog safeguard holds across a rules reload: the recovered Watchdog
// incident must not get temporal matchers via adoptRules.
func TestAdoptRulesKeepsWatchdogSafeguard(t *testing.T) {
	original := newTestGroupsCollection(t)
	now := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	assigned := original.ProcessAlertsBatch([]model.LabelSet{{"alertname": "Watchdog", "namespace": "openshift-monitoring"}}, now.Add(-time.Hour))
	id := string(assigned[0]["group_id"])
	replacement := testRules(t, `{"strategies":[{"match":{"exact":true},"within":"24h","from":"end"},{"match":{},"within":"15m","from":"start"}]}`)
	p := &processor{
		groupsCollection: original,
		rulesSource:      func() *RulesSnapshot { return replacement },
		assignedAlerts: []model.LabelSet{
			{"alertname": "Watchdog", "namespace": "openshift-monitoring", "group_id": model.LabelValue(id)},
		},
	}
	require.NoError(t, p.adoptRules(replacement, now))
	for _, g := range p.groupsCollection.Groups {
		if g.RootGroupID != id {
			continue
		}
		require.NotNil(t, g.strategy)
		require.NotEmpty(t, g.strategy.operator, "Watchdog incident must have no time-only matcher")
	}
}

// newPriorityRules builds a snapshot from multiple JSON rule specs, sorted
// by priority.
func newPriorityRules(t *testing.T, specs ...string) *RulesSnapshot {
	t.Helper()
	rules := make([]*parsedRule, 0, len(specs))
	for i, spec := range specs {
		spec, err := decodeRuleSpec([]byte(spec))
		require.NoError(t, err)
		rule, err := parseRule(fmt.Sprintf("rule-%d", i), spec)
		require.NoError(t, err)
		rules = append(rules, rule)
	}
	sortRules(rules)
	return &RulesSnapshot{rules: rules}
}

// The priority claim filter holds across a rules reload: a claimed alert's
// incident must not get temporal matchers from lower-priority rules that
// could then capture unrelated alerts.
func TestAdoptRulesFiltersTemporalMatchersByClaim(t *testing.T) {
	rules := newPriorityRules(t,
		`{"priority":0,"strategies":[{"match":{"allOf":["tenant"]},"within":"24h","from":"start"}]}`,
		`{"priority":100,"strategies":[{"match":{},"within":"15m","from":"start"}]}`)
	start := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	gc, err := NewGroupsCollection(rules)
	require.NoError(t, err)
	assigned := gc.ProcessAlertsBatch([]model.LabelSet{
		{"alertname": "NodeError", "namespace": "om", "tenant": "t1"},
	}, start)
	id := string(assigned[0]["group_id"])
	require.NotEmpty(t, id)

	p := &processor{
		groupsCollection: gc,
		assignedAlerts: []model.LabelSet{
			{"alertname": "NodeError", "namespace": "om", "tenant": "t1", "group_id": model.LabelValue(id)},
		},
	}
	require.NoError(t, p.adoptRules(rules, start))
	for _, g := range p.groupsCollection.Groups {
		if g.RootGroupID != id {
			continue
		}
		require.NotEmpty(t, g.strategy.operator, "claimed incident must not get a time-only matcher")
	}
	// An unclaimed alert arriving after the reload must not join the tenant
	// incident through a lower-priority temporal matcher.
	got := p.groupsCollection.ProcessAlertsBatch([]model.LabelSet{
		{"alertname": "StorageError", "namespace": "om"},
	}, start.Add(time.Minute))
	require.NotEqual(t, id, string(got[0]["group_id"]), "unclaimed alert must not join the claimed incident")
}

// The Watchdog safeguard holds through the batch following a rules reload:
// adoptRules registers the recovered root, and the lazy materialization in
// forcedRoot must not add temporal matchers to the Watchdog incident.
func TestAdoptRulesKeepsWatchdogSafeguardThroughNextBatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	loader := mocks.NewMockPrometheusLoader(ctrl)
	am := mocks.NewMockAlertManagerLoader(ctrl)
	original := newTestGroupsCollection(t)
	now := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	assigned := original.ProcessAlertsBatch([]model.LabelSet{
		{"alertname": "Watchdog", "namespace": "openshift-monitoring"},
	}, now.Add(-time.Hour))
	id := string(assigned[0]["group_id"])
	require.NotEmpty(t, id)
	// A time-only strategy is the most aggressive one: without the safeguard
	// any alert in the window would join the Watchdog incident.
	replacement := testRules(t, `{"strategies":[{"match":{},"within":"24h","from":"start"}]}`)
	p := &processor{
		loader: loader, amLoader: am, groupsCollection: original,
		rulesSource: func() *RulesSnapshot { return replacement },
		assignedAlerts: []model.LabelSet{
			{"alertname": "Watchdog", "namespace": "openshift-monitoring", "group_id": model.LabelValue(id)},
		},
	}
	watchdog := model.LabelSet{"alertname": "Watchdog", "namespace": "openshift-monitoring"}
	unrelated := model.LabelSet{"alertname": "NodeDown", "namespace": "om", "node": "n1"}
	loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, now).Return([]model.LabelSet{watchdog, maps.Clone(unrelated)}, nil)
	am.EXPECT().SilencedAlerts().Return(nil, nil)
	got, err := p.loadAlerts(context.Background(), now)
	require.NoError(t, err)
	byName := map[string]string{}
	for _, alert := range got {
		byName[string(alert["alertname"])] = string(alert["group_id"])
	}
	require.Equal(t, id, byName["Watchdog"], "Watchdog must keep its incident")
	require.NotEqual(t, id, byName["NodeDown"], "unrelated alert must not join the Watchdog incident")
	for _, g := range p.groupsCollection.Groups {
		if g.RootGroupID != id {
			continue
		}
		require.NotEmpty(t, g.strategy.operator, "Watchdog incident must have no time-only matcher")
	}
}

// A recovered assignment must not create duplicate temporal matchers: after
// a reload the incident's temporal matchers exist once in adoptRules, and
// the lazy materialization must reuse the registered root instead of adding
// them again.
func TestAdoptRulesDoesNotDuplicateTemporalMatchers(t *testing.T) {
	rules := testRules(t, `{"strategies":[{"match":{},"within":"15m","from":"start"}]}`)
	gc, err := NewGroupsCollection(rules)
	require.NoError(t, err)
	start := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	labels := model.LabelSet{"alertname": "NodeDown", "namespace": "om"}
	assigned := gc.ProcessAlertsBatch([]model.LabelSet{maps.Clone(labels)}, start)
	id := string(assigned[0]["group_id"])
	require.NotEmpty(t, id)

	p := &processor{
		groupsCollection: gc,
		assignedAlerts: []model.LabelSet{
			func() model.LabelSet { l := maps.Clone(labels); l["group_id"] = model.LabelValue(id); return l }(),
		},
	}
	require.NoError(t, p.adoptRules(rules, start))
	// The assigned alert fires again in the next batch, triggering the lazy
	// root materialization.
	p.groupsCollection.ProcessAlertsBatch([]model.LabelSet{maps.Clone(labels)}, start.Add(time.Minute))
	temporal := 0
	for _, g := range p.groupsCollection.Groups {
		if g.RootGroupID == id && g.strategy != nil && g.strategy.operator == "" {
			temporal++
		}
	}
	require.Equal(t, 1, temporal, "incident must keep exactly one temporal matcher")
}

// Restart recovery: an alert with a recovered assignment pointing at a
// Watchdog incident must not grow temporal matchers when it materializes
// its root.
func TestForcedRootKeepsWatchdogSafeguardOnRecovery(t *testing.T) {
	rules := testRules(t, `{"strategies":[{"match":{},"within":"24h","from":"start"}]}`)
	gc, err := NewGroupsCollection(rules)
	require.NoError(t, err)
	start := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	watchdog := model.LabelSet{"alertname": "Watchdog", "namespace": "openshift-monitoring"}
	unrelated := model.LabelSet{"alertname": "NodeDown", "namespace": "om", "node": "n1"}
	gc.assignments = []*historicalAssignment{
		{labels: maps.Clone(watchdog), start: model.TimeFromUnixNano(start.Add(-time.Hour).UnixNano()), end: model.TimeFromUnixNano(start.Add(-time.Minute).UnixNano()), id: "recovered-watchdog"},
	}
	got := gc.ProcessAlertsBatch([]model.LabelSet{maps.Clone(watchdog), maps.Clone(unrelated)}, start)
	byName := map[string]string{}
	for _, alert := range got {
		byName[string(alert["alertname"])] = string(alert["group_id"])
	}
	require.Equal(t, "recovered-watchdog", byName["Watchdog"], "recovered Watchdog must keep its incident ID")
	require.NotEqual(t, "recovered-watchdog", byName["NodeDown"], "unrelated alert must not join the recovered Watchdog incident")
	for _, g := range gc.Groups {
		if g.RootGroupID != "recovered-watchdog" {
			continue
		}
		require.NotEmpty(t, g.strategy.operator, "recovered Watchdog incident must have no time-only matcher")
	}
}

// A higher-priority custom rule claims new alerts by labels: matchers from
// lower-priority rules (for example the default namespace grouping of a
// pre-existing incident) must not capture them, and their new incidents must
// not grow doors from lower-priority strategies.
func TestRulePriorityClaimsNewAlertsOverLowerPriorityMatchers(t *testing.T) {
	custom := `{"priority":0,"strategies":[{"match":{"allOf":["tenant"]},"within":"24h","from":"start","excludeAlerts":[{"alertname":"HighLatency"}]}]}`
	deflt := `{"priority":100,"strategies":[{"match":{"anyOf":["namespace"]},"within":"24h","from":"start"},{"match":{},"within":"15m","from":"start"}]}`
	var rules []*parsedRule
	for name, spec := range map[string]string{"custom": custom, "default": deflt} {
		spec, err := decodeRuleSpec([]byte(spec))
		require.NoError(t, err)
		rule, err := parseRule(name, spec)
		require.NoError(t, err)
		rules = append(rules, rule)
	}
	sortRules(rules)
	gc, err := NewGroupsCollection(&RulesSnapshot{rules: rules})
	require.NoError(t, err)

	start := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	// A pre-existing incident in the same namespace, older than the temporal
	// window so only its namespace matcher can capture new alerts.
	pre := gc.ProcessAlertsBatch([]model.LabelSet{{"alertname": "TargetDown", "namespace": "om"}}, start)
	preID := string(pre[0]["group_id"])
	require.NotEmpty(t, preID)

	at := start.Add(16 * time.Minute)
	got := gc.ProcessAlertsBatch([]model.LabelSet{
		{"alertname": "NodeError", "namespace": "om", "tenant": "t1"},
		{"alertname": "StorageError", "namespace": "om", "tenant": "t1"},
		{"alertname": "NodeError", "namespace": "om", "tenant": "t2"},
		{"alertname": "HighLatency", "namespace": "om", "tenant": "t2"},
	}, at)
	byName := map[string]string{}
	for _, alert := range got {
		byName[string(alert["alertname"])+string(alert["tenant"])] = string(alert["group_id"])
	}
	t1a, t1b := byName["NodeErrort1"], byName["StorageErrort1"]
	t2 := byName["NodeErrort2"]
	require.NotEmpty(t, t1a)
	require.Equal(t, t1a, t1b, "alerts sharing tenant t1 must share an incident")
	require.NotEmpty(t, t2)
	require.NotEqual(t, t1a, t2, "alerts with different tenants must not share an incident")
	require.NotEqual(t, preID, t1a, "tenant alerts must not join the pre-existing incident")
	require.NotEqual(t, preID, t2, "tenant alerts must not join the pre-existing incident")
	// HighLatency is excluded from the custom rule, so the default rules apply
	// to it: it may join the pre-existing same-namespace incident, but the
	// exclusion must keep it out of the tenant incidents.
	high := byName["HighLatencyt2"]
	require.NotEqual(t, t1a, high, "excluded alert must not join the t1 incident")
	require.NotEqual(t, t2, high, "excluded alert must not join the t2 incident")
}

func TestProcessorReloadKeepsAssignmentsWhenExclusionRemoved(t *testing.T) {
	twoRuleSnapshot := func(t *testing.T, excludeC bool) *RulesSnapshot {
		t.Helper()
		exclusion := ""
		if excludeC {
			exclusion = `,"excludeAlerts":[{"alertname":"ChaLifecycleC"}]`
		}
		custom := `{"priority":0,"strategies":[{"match":{"allOf":["testgroup"]},"within":"24h","from":"start"` + exclusion + `}]}`
		rules := make([]*parsedRule, 0, 2)
		for name, spec := range map[string]string{"custom": custom, "default": `{"priority":100,"strategies":[{"match":{"anyOf":["namespace"]},"within":"24h","from":"start"}]}`} {
			spec, err := decodeRuleSpec([]byte(spec))
			require.NoError(t, err)
			rule, err := parseRule(name, spec)
			require.NoError(t, err)
			rules = append(rules, rule)
		}
		sortRules(rules)
		return &RulesSnapshot{rules: rules}
	}

	ctrl := gomock.NewController(t)
	loader := mocks.NewMockPrometheusLoader(ctrl)
	am := mocks.NewMockAlertManagerLoader(ctrl)
	now := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	current := twoRuleSnapshot(t, true)
	alerts := []model.LabelSet{
		{"alertname": "ChaLifecycleA", "testgroup": "g1", "namespace": "ns1"},
		{"alertname": "ChaLifecycleB", "testgroup": "g1", "namespace": "ns1"},
		{"alertname": "TargetDown", "namespace": "ns2"},
		{"alertname": "ChaLifecycleC", "testgroup": "g1", "namespace": "ns2"},
	}
	p := &processor{
		loader: loader, amLoader: am,
		rulesSource: func() *RulesSnapshot { return current },
	}
	initialGroups, err := NewGroupsCollection(current)
	require.NoError(t, err)
	p.groupsCollection = initialGroups
	// First batch under the excluding rules: A and B share one incident via
	// testgroup, C joins TargetDown via namespace.
	// loadAlerts mutates the returned label sets (it adds group_id), so each
	// batch needs fresh copies like a real query would return.
	batch := func() []model.LabelSet {
		out := make([]model.LabelSet, len(alerts))
		for i, a := range alerts {
			out[i] = maps.Clone(a)
		}
		return out
	}
	loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, now).Return(batch(), nil)
	am.EXPECT().SilencedAlerts().Return(nil, nil)
	got, err := p.loadAlerts(context.Background(), now)
	require.NoError(t, err)
	byName := map[string]string{}
	for _, alert := range got {
		byName[string(alert["alertname"])] = string(alert["group_id"])
	}
	customID, defaultID := byName["ChaLifecycleA"], byName["ChaLifecycleC"]
	require.NotEmpty(t, customID)
	require.Equal(t, customID, byName["ChaLifecycleB"])
	require.Equal(t, defaultID, byName["TargetDown"])
	require.NotEqual(t, customID, defaultID)

	// Remove the exclusion and reload: existing assignments must not move.
	current = twoRuleSnapshot(t, false)
	// Assignments stay alive while the alert keeps firing: each poll
	// extends the covered interval, so batches chained within the tolerance
	// keep the incident indefinitely.
	for _, at := range []time.Time{now.Add(time.Minute), now.Add(10 * time.Minute), now.Add(20 * time.Minute)} {
		loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, at).Return(batch(), nil)
		am.EXPECT().SilencedAlerts().Return(nil, nil)
		got, err = p.loadAlerts(context.Background(), at)
		require.NoError(t, err)
		byName = map[string]string{}
		for _, alert := range got {
			byName[string(alert["alertname"])] = string(alert["group_id"])
		}
		require.Equal(t, customID, byName["ChaLifecycleA"], "at %s: assigned alert must keep its incident", at)
		require.Equal(t, customID, byName["ChaLifecycleB"], "at %s: assigned alert must keep its incident", at)
		require.Equal(t, defaultID, byName["ChaLifecycleC"], "at %s: assigned alert must keep its incident", at)
		require.Equal(t, defaultID, byName["TargetDown"], "at %s: assigned alert must keep its incident", at)
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

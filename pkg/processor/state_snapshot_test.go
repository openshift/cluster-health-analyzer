package processor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"go.uber.org/mock/gomock"

	"github.com/openshift/cluster-health-analyzer/pkg/test/mocks"
	"github.com/openshift/cluster-health-analyzer/pkg/utils"
	"github.com/stretchr/testify/require"
)

// The snapshot must be a faithful, self-contained copy of the matcher state,
// including a temporal matcher with no selectors and a recovered assignment.
func TestMatcherStateSnapshotCopiesState(t *testing.T) {
	ctrl := gomock.NewController(t)
	loader := mocks.NewMockPrometheusLoader(ctrl)
	am := mocks.NewMockAlertManagerLoader(ctrl)
	rules := testRules(t, `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"},{"match":{},"within":"15m","from":"start"}]}`)
	start := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	now := start.Add(3 * time.Hour)
	alertsRange := utils.RelativeIntervalsToRangeVectors([]utils.RelativeInterval{
		{Labels: model.LabelSet{"alertname": "A", "namespace": "prod", "severity": "warning", "node": "worker-1"}, Start: 0, End: 120},
	}, model.TimeFromUnixNano(start.UnixNano()), time.Minute)
	healthMap := utils.RelativeIntervalsToRangeVectors([]utils.RelativeInterval{
		{Labels: model.LabelSet{"group_id": "ID-1", "src_alertname": "A", "src_namespace": "prod", "src_severity": "warning"}, Start: 0, End: 120},
	}, model.TimeFromUnixNano(start.UnixNano()), time.Minute)
	p := &processor{loader: loader, amLoader: am, rulesSource: func() *RulesSnapshot { return rules }}
	loader.EXPECT().LoadAlertsRange(gomock.Any(), start, now, time.Minute).Return(alertsRange, nil)
	loader.EXPECT().LoadVectorRange(gomock.Any(), ClusterHealthComponentsMap, start, now, time.Minute).Return(healthMap, nil)
	require.NoError(t, p.InitGroupsCollection(context.Background(), start, now, time.Minute))

	snapshot := p.MatcherStateSnapshot()
	require.NotEmpty(t, snapshot.Groups)
	require.NotEmpty(t, snapshot.Assignments)
	require.Equal(t, "ID-1", snapshot.Assignments[0].IncidentID)
	require.Equal(t, "A", snapshot.Assignments[0].Labels["alertname"])

	foundTemporal, foundLabel := false, false
	for _, g := range snapshot.Groups {
		// Non-root matchers carry only RootGroupID; GroupID is set on roots.
		require.Contains(t, []string{"", "ID-1"}, g.GroupID)
		require.Equal(t, "ID-1", g.RootGroupID)
		require.False(t, g.ReferenceTime.IsZero(), "reference time must be resolved")
		if g.Strategy.Operator == "" && len(g.Selectors) == 0 {
			foundTemporal = true
			require.Equal(t, "15m0s", g.Strategy.Within)
		}
		if len(g.Selectors) == 1 && g.Selectors[0]["node"] == "worker-1" {
			foundLabel = true
		}
	}
	require.True(t, foundTemporal, "the time-only strategy must appear without selectors")
	require.True(t, foundLabel, "the node selector must appear with its labels")

	// A live batch must be reflected in the next snapshot.
	loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, now).Return([]model.LabelSet{
		{"alertname": "A", "namespace": "prod", "severity": "warning", "node": "worker-1"},
	}, nil)
	am.EXPECT().SilencedAlerts().Return(nil, nil)
	_, err := p.loadAlerts(context.Background(), now)
	require.NoError(t, err)
	updated := p.MatcherStateSnapshot()
	require.NotEmpty(t, updated.Groups)
	require.NotEmpty(t, updated.Assignments, "the recovered assignment must survive the live batch")
}

// A forced root is a synthetic anchor with no strategy of its own; the
// snapshot must still report which rule created its incident's matchers.
func TestForcedRootSnapshotReportsRuleName(t *testing.T) {
	rules := testRules(t, `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"},{"match":{},"within":"15m","from":"start"}]}`)
	gc, err := NewGroupsCollection(rules)
	require.NoError(t, err)
	start := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	i := Interval{Metric: model.LabelSet{"alertname": "A", "node": "worker-1"},
		Start: model.TimeFromUnixNano(start.UnixNano()), End: model.TimeFromUnixNano(start.Add(time.Hour).UnixNano())}
	gc.forcedRoot("ID-1", i)

	snapshot := gc.stateSnapshot()
	require.Len(t, snapshot.ForcedRoots, 1)
	require.Equal(t, "ID-1", snapshot.ForcedRoots[0].RootGroupID)
	require.Equal(t, "custom", snapshot.ForcedRoots[0].Strategy.RuleName,
		"forced root must report the rule of its incident's matchers")
}

// Concurrent snapshots during live processing must not race; run with
// -race (make test does).
func TestMatcherStateSnapshotConcurrentWithProcessing(t *testing.T) {
	ctrl := gomock.NewController(t)
	loader := mocks.NewMockPrometheusLoader(ctrl)
	am := mocks.NewMockAlertManagerLoader(ctrl)
	rules := testRules(t, `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`)
	start := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	now := start.Add(3 * time.Hour)
	alertsRange := utils.RelativeIntervalsToRangeVectors([]utils.RelativeInterval{
		{Labels: model.LabelSet{"alertname": "A", "namespace": "prod", "severity": "warning", "node": "worker-1"}, Start: 0, End: 120},
	}, model.TimeFromUnixNano(start.UnixNano()), time.Minute)
	p := &processor{loader: loader, amLoader: am, rulesSource: func() *RulesSnapshot { return rules }}
	loader.EXPECT().LoadAlertsRange(gomock.Any(), start, now, time.Minute).Return(alertsRange, nil)
	loader.EXPECT().LoadVectorRange(gomock.Any(), ClusterHealthComponentsMap, start, now, time.Minute).Return(nil, nil)
	require.NoError(t, p.InitGroupsCollection(context.Background(), start, now, time.Minute))

	batches := 3
	alerts := []model.LabelSet{
		{"alertname": "A", "namespace": "prod", "severity": "warning", "node": "worker-1"},
	}
	for i := 0; i < batches; i++ {
		loader.EXPECT().LoadQuery(gomock.Any(), `ALERTS{alertstate="firing"}`, gomock.Any()).Return(alerts, nil)
	}
	am.EXPECT().SilencedAlerts().Return(nil, nil).Times(batches)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = p.MatcherStateSnapshot()
			}
		}
	}()
	for i := 0; i < batches; i++ {
		_, err := p.loadAlerts(context.Background(), now.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
	}
	close(stop)
	wg.Wait()
}

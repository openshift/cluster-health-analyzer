package processor

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/utils"
)

// TestGroupsCollectionProcessAlertsBatch simulates processing of alerts as they arrive.
//
// We check that they get appropriate group_id assigned to them.
func TestGroupsCollectionProcessAlertsBatch(t *testing.T) {
	start := model.TimeFromUnixNano(
		time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())

	gc := GroupsCollection{}

	// Case 1: An alert arrives without any previous alerts history.
	//
	// A new group_id should be assigned to the alert.
	alerts := []prom.Alert{
		{Name: "Alert1", Labels: map[string]string{"alertname": "Alert1"}},
	}
	case1 := gc.ProcessAlertsBatch(alerts, start.Add(1*time.Hour+10*time.Minute).Time())

	assert.NotEmpty(t, case1[0].Labels["group_id"])

	// Case 2: Alert is within the time range of time-based matcher.
	//
	// It should match the group of previous alert.
	alerts = []prom.Alert{
		{Name: "Alert2", Labels: map[string]string{"alertname": "Alert2", "namespace": "ns2"}},
	}
	case2 := gc.ProcessAlertsBatch(alerts, start.Add(1*time.Hour+15*time.Minute).Time())

	assert.Equal(t, case1[0].Labels["group_id"], case2[0].Labels["group_id"])

	// Case 3: 3 alerts outside of the time range of time-based matcher,
	//
	// They should not match the original group, but they should both become part
	// of a new group.
	alerts = []prom.Alert{
		{Name: "Alert3.1", Labels: map[string]string{"alertname": "Alert3.1"}},
		{Name: "Alert3.2", Labels: map[string]string{"alertname": "Alert3.2"}},
	}
	case3 := gc.ProcessAlertsBatch(alerts, start.Add(3*time.Hour).Time())
	assert.NotEqual(t, "time-matcher", case3[0].Labels["group_id"])
	assert.Equal(t, case3[0].Labels["group_id"], case3[1].Labels["group_id"])

	// Case 4: Alert with same alertname as one from case 2 fires within
	// [processor.fuzzyMatchTimeDelta] time range.
	//
	// It should match the group created in case 3.
	alerts = []prom.Alert{
		{Name: "Alert3.1", Labels: map[string]string{"alertname": "Alert3.1"}},
	}
	case4 := gc.ProcessAlertsBatch(alerts, start.Add(7*time.Hour).Time())
	assert.Equal(t, case3[0].Labels["group_id"], case4[0].Labels["group_id"])

	// Case 5: Alert from the same namespace firing within [processor.fuzzyMatchTimeDelta]
	//
	// It should match with the last active group from the same namespace.
	alerts = []prom.Alert{
		{Name: "Alert5", Labels: map[string]string{
			"alertname": "Alert5", "namespace": "ns2"}},
	}
	case5 := gc.ProcessAlertsBatch(alerts, start.Add(7*time.Hour).Time())
	assert.Equal(t, case2[0].Labels["group_id"], case5[0].Labels["group_id"])

	// Case 6: A watchdog is part of the new group.
	//
	// It's a sign that the alerts in the group might not be related
	// to each other, even as they appeared at the same time. Only alerts from the
	// same namespace should be grouped together.
	alerts = []prom.Alert{
		{Name: "Watchdog", Labels: map[string]string{
			"alertname": "Watchdog", "namespace": "openshift-monitoring"}},
		{Name: "Alert6.1", Labels: map[string]string{
			"alertname": "Alert6.1", "namespace": "ns6.1"}},
		{Name: "Alert6.2", Labels: map[string]string{
			"alertname": "Alert6.2", "namespace": "ns6.1"}},
		{Name: "Alert6.3", Labels: map[string]string{
			"alertname": "Alert6.3", "namespace": "ns6.3"}},
	}
	case6 := gc.ProcessAlertsBatch(alerts, start.Add(10*time.Hour).Time())
	assert.NotEqual(t, case6[0].Labels["group_id"], case6[1].Labels["group_id"])
	// 1st and 2nd alert were from the same namespace, so they should be in the same group.
	assert.Equal(t, case6[1].Labels["group_id"], case6[2].Labels["group_id"])
	// 3rd alert was from a different namespace, so it should be in a different group.
	assert.NotEqual(t, case6[1].Labels["group_id"], case6[3].Labels["group_id"])
}

// TestGroupsCollectionPruneGroups tests pruning of old groups.
//
// We check that groups that are not relevant anymore are pruned after certain
// perdio of time.
func TestGroupsCollectionPruneGroups(t *testing.T) {
	start := model.TimeFromUnixNano(
		time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())

	gc := GroupsCollection{}

	// Time-based matcher should be pruned after [processor.fuzzyMatchTimeDelta]
	gc.AddGroup(&GroupMatcher{
		GroupID:  "time-matcher",
		Start:    start.Add(1 * time.Hour),
		Modified: start.Add(1 * time.Hour),
		End:      start.Add(3 * time.Hour),
		Distance: math.Inf(1)})

	// Fuzzy matcher should be pruned after [processor.fuzzyMatchTimeDelta]
	gc.AddGroup(&GroupMatcher{
		GroupID:  "fuzzy-matcher-old",
		Start:    start.Add(1 * time.Hour),
		Modified: start.Add(1 * time.Hour),
		End:      start.Add(3 * time.Hour),
		Distance: 1})

	// This fuzzy-matcher can be still relevant, as it was modified recently.
	// It should not be pruned.
	gc.AddGroup(&GroupMatcher{
		GroupID:  "fuzzy-matcher-recent",
		Start:    start.Add(1 * time.Hour),
		Modified: start.Add(24 * time.Hour),
		End:      start.Add(3 * time.Hour),
		Distance: 1})

	// Direct matcher should be pruned after [processor.directMatchTimeDelta]
	// It can be active for longer time and should not be pruned first.
	gc.AddGroup(&GroupMatcher{
		GroupID:  "direct-matcher",
		Start:    start.Add(1 * time.Hour),
		Modified: start.Add(1 * time.Hour),
		End:      start.Add(3 * time.Hour),
		Distance: 0})

	gc.PruneGroups(start.Add(26 * time.Hour).Time())

	assert.Equal(t, 2, len(gc.Groups))
	for _, g := range gc.Groups {
		assert.Contains(t, []string{"fuzzy-matcher-recent", "direct-matcher"}, g.GroupID)
	}

	// Simulate pruning after 5 days.
	gc.PruneGroups(start.Add((5*24 + 5) * time.Hour).Time())
	assert.Equal(t, 0, len(gc.Groups))
}

var alertsIntervals = []utils.RelativeInterval{
	{
		Labels: map[string]string{
			"alertname": "Watchdog",
			"namespace": "openshift-monitoring",
			"severity":  "none",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "AlertmanagerReceiversNotConfigured",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "ClusterNotUpgradeable",
			"namespace": "openshift-cluster-version",
			"severity":  "info",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "TargetDown",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
		},
		Start: 3000,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "KubeNodeNotReady",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"node":      "ip-10-0-58-248.us-east-2.compute.internal",
			"condition": "Ready",
		},
		Start: 3010,
		End:   4000,
	},
}

// TestGroupsCollectionProcessHistoricalAlerts tests processing of historical alerts.
//
// We generate a range vector of alerts happening over certain period of time
// and check that they get grouped correctly.
func TestGroupsCollectionProcessHistoricalAlerts(t *testing.T) {
	start := model.TimeFromUnixNano(
		time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())

	alerts := utils.RelativeIntervalsToRangeVectors(alertsIntervals, start, 1*time.Minute)

	gc := GroupsCollection{}

	gc.processHisotricalAlerts(alerts)

	// Group GroupMatchers by group_id
	groupsMap := make(map[string][]*GroupMatcher)
	for _, g := range gc.Groups {
		groupsMap[g.RootGroupID] = append(groupsMap[g.RootGroupID], g)
	}

	// Get the names of alerts in each group
	groupedAlerts := make([][]string, 0, len(groupsMap))
	for _, matchers := range groupsMap {
		alerts := make([]string, 0)
		// Iterate through each GroupMatcher
		for _, groupMatcher := range matchers {
			// Each group matcher can have multiple label matchers. While
			// most of the time, there is only one label matcher, it's
			// possible to have multiple label matchers in a single group
			// though the act of fuzzy-matching.
			for _, labelMatcher := range groupMatcher.Matchers {
				alert := labelMatcher.Labels["alertname"]
				if alert != "" && !slices.Contains(alerts, alert) {
					alerts = append(alerts, alert)
				}
			}
		}
		groupedAlerts = append(groupedAlerts, alerts)
	}

	// This is just high-level assertion that the alerts were grouped correctly.
	assert.Equal(t, 4, len(groupedAlerts))
	assert.Contains(t, groupedAlerts, []string{"Watchdog"})
	assert.Contains(t, groupedAlerts, []string{"AlertmanagerReceiversNotConfigured"})
	assert.Contains(t, groupedAlerts, []string{"ClusterNotUpgradeable"})
	assert.Contains(t, groupedAlerts, []string{"TargetDown", "KubeNodeNotReady"})
}

var mappingIntervals = []utils.RelativeInterval{
	{
		Labels: map[string]string{
			"group_id":      "group1",
			"src_alertname": "AlertmanagerReceiversNotConfigured",
			"src_namespace": "openshift-monitoring",
			"src_severity":  "warning",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"group_id":      "group2",
			"src_alertname": "TargetDown",
			"src_namespace": "openshift-monitoring",
			"src_severity":  "warning",
		},
		// 5 minutes after the alert - it should still be able to tollerate some delay.
		Start: 3005,
		End:   4000,
	},
}

// TestGroupsCollectionUpdateGroupUUIDs tests updating group UUIDs based on mappings.
//
// The [GroupsCollection.UpdateGroupUUIDs] function is used to reflect already
// existing incidents (loaded from Prometheus) and update the internal groups
// based on that information.
func TestGroupsCollectionUpdateGroupUUIDs(t *testing.T) {
	start := model.TimeFromUnixNano(
		time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())

	alerts := utils.RelativeIntervalsToRangeVectors(alertsIntervals, start, 1*time.Minute)

	gc := GroupsCollection{}
	gc.processHisotricalAlerts(alerts)

	mappings := utils.RelativeIntervalsToRangeVectors(mappingIntervals, start, 1*time.Minute)

	gc.UpdateGroupUUIDs(mappings)

	// Map from group_id to list of alert names.
	groupedAlerts := make(map[string][]string)
	for _, g := range gc.Groups {
		for _, labelMatcher := range g.Matchers {
			alert := labelMatcher.Labels["alertname"]
			if alert != "" && !slices.Contains(groupedAlerts[g.RootGroupID], alert) {
				groupedAlerts[g.RootGroupID] = append(groupedAlerts[g.RootGroupID], alert)
			}
		}
	}

	// Check that the group ids were property updated based on the mapping.
	assert.Equal(t, 4, len(groupedAlerts))
	assert.Equal(t, groupedAlerts["group1"], []string{"AlertmanagerReceiversNotConfigured"})
	assert.Equal(t, groupedAlerts["group2"], []string{"TargetDown", "KubeNodeNotReady"})
}

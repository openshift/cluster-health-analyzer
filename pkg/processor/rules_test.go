package processor

import (
	"fmt"
	"log/slog"
	"maps"
	"math"
	"math/rand"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/utils"
)

type legacyGroupMatcher struct {
	GroupID     string
	RootGroupID string
	Start       model.Time
	Modified    model.Time
	End         model.Time
	Distance    float64
	Matchers    []common.LabelsSubsetMatcher
}

type legacyGroupedInterval struct {
	Interval
	GroupMatcher *legacyGroupMatcher
}

type legacyMatch struct {
	GroupMatcher *legacyGroupMatcher
	TimeDist     time.Duration
}

// This test-only oracle is the grouping implementation from main at 774eadb.
var legacyNoMatchAlerts = []common.LabelsSubsetMatcher{
	{Labels: model.LabelSet{"alertname": "Watchdog", "namespace": "openshift-monitoring"}},
	{Labels: model.LabelSet{"alertname": "AlertmanagerReceiversNotConfigured", "namespace": "openshift-monitoring"}},
}

func newTestGroupsCollection(t *testing.T) *GroupsCollection {
	t.Helper()
	rules, err := LoadRulesFile("../../manifests/backend/09_incident_rule_defaults.yaml")
	require.NoError(t, err)
	groups, err := NewGroupsCollection(rules)
	require.NoError(t, err)
	return groups
}

const (
	legacyFuzzyMatchTimeDelta      = 24 * time.Hour
	legacyTimeMatchTimeDelta       = 15 * time.Minute
	legacyDirectMatchLongTimeDelta = 120 * time.Hour
)

func legacyNewGroupMatcherSubset(labels model.LabelSet, keys []model.LabelName, distance float64) *legacyGroupMatcher {
	labels = getMapSubset(labels, keys...)

	return &legacyGroupMatcher{
		Matchers: []common.LabelsSubsetMatcher{{Labels: labels}},
		Distance: distance,
	}
}

func legacyNewGroupMatcherExact(labels model.LabelSet) *legacyGroupMatcher {
	return &legacyGroupMatcher{
		Matchers: []common.LabelsSubsetMatcher{{Labels: labels}},
		Distance: 0,
	}
}

func legacyWatchdogAlert(i Interval) bool {
	return i.Metric["alertname"] == "Watchdog" &&
		i.Metric["namespace"] == "openshift-monitoring"
}

func legacyIsSubsetOf(g *legacyGroupMatcher, other *legacyGroupMatcher) bool {
	if g.Distance != other.Distance {
		return false
	}

	// Check if all matchers of g are in other.
	for _, m := range g.Matchers {
		contains := false
		for _, om := range other.Matchers {
			if om.Equals(m) {
				contains = true
				break
			}
		}
		if !contains {
			return false
		}
	}
	return true
}

func legacyExpandMatchers(g *legacyGroupMatcher, matchers []common.LabelsSubsetMatcher) {
	for _, m := range matchers {
		// Check if the matcher is already in the group.
		// If not, add it.
		found := false

		for _, gm := range g.Matchers {
			if gm.Equals(m) {
				found = true
				break
			}
		}

		if !found {
			g.Matchers = append(g.Matchers, m)
		}
	}
}

func legacyAlertFuzzyLabels(i Interval) model.LabelSet {
	for _, m := range legacyNoMatchAlerts {
		// For certain alerts, we don't want to do any fuzzy matching.
		if match, _ := m.Matches(i.Metric); match {
			return nil
		}
	}
	// TODO: add option for some alerts to match some known pairs, but not others.
	// E.g. APIRemovedInNextReleaseInUse and APIRemovedInNextEUSReleaseInUse
	return getMapSubset(i.Metric, "alertname", "namespace")
}

// alertGroupMatchers returns a list of matchers for the alert.
// This includes exact matcher with 0 distance, as well as various fuzzy matchers
// based on the alert labels.
func legacyAlertGroupMatchers(interval Interval) []*legacyGroupMatcher {
	labels := interval.Metric
	groups := []*legacyGroupMatcher{
		legacyNewGroupMatcherExact(labels),
		// Match on main subset of labels - should be still close enough.
		legacyNewGroupMatcherSubset(labels, []model.LabelName{"namespace", "alertname", "service", "job", "container"}, 1),
	}

	for k, v := range legacyAlertFuzzyLabels(interval) {
		groups = append(groups,
			legacyNewGroupMatcherSubset(model.LabelSet{k: v}, []model.LabelName{k}, 2),
		)
	}
	for _, g := range groups {
		g.Start = interval.Start
		g.Modified = interval.Start
		g.End = interval.End
	}
	return groups
}

type legacyGroupsCollection struct {
	Groups []*legacyGroupMatcher
}

func (gc *legacyGroupsCollection) AddGroup(g *legacyGroupMatcher) {
	gc.Groups = append(gc.Groups, g)
}

func (gc *legacyGroupsCollection) ProcessIntervalsBatch(intervals []Interval) []legacyGroupedInterval {
	slog.Info("Processing", "intervals", len(intervals), "groups", len(gc.Groups))
	groupedIntervals, unmatched := gc.tryMatchIntervals(intervals)

	if len(unmatched) > 0 {
		// Create new groups for the unmatched intervals.
		newGroupedIntervals := gc.addIntervalsGroups(unmatched, nil)
		groupedIntervals = append(groupedIntervals, newGroupedIntervals...)
	}

	return groupedIntervals
}

func (gc *legacyGroupsCollection) processHistoricalAlerts(alertsRange prom.RangeVector) {
	changes := MetricsChanges(alertsRange)

	for _, change := range changes {
		gc.ProcessIntervalsBatch(change.Intervals)
	}
}

func (gc *legacyGroupsCollection) ProcessAlertsBatch(alerts []model.LabelSet, timestamp time.Time) []model.LabelSet {
	modelT := model.TimeFromUnixNano(timestamp.UnixNano())

	intervals := make([]Interval, 0, len(alerts))
	for _, a := range alerts {
		intervals = append(intervals, Interval{
			Metric: a,
			Start:  modelT,
			End:    modelT,
		})
	}

	groupedIntervals := gc.ProcessIntervalsBatch(intervals)

	ret := make([]model.LabelSet, 0, len(alerts))
	for _, gi := range groupedIntervals {
		alert := gi.Metric
		if gi.GroupMatcher != nil {
			alert["group_id"] = model.LabelValue(gi.GroupMatcher.RootGroupID)
		}
		ret = append(ret, alert)
	}
	return ret
}

// PruneGroups removes groups that can't be matched anymore.
//
// It calculates the threshold based on the provided time and removes groups.
func (gc *legacyGroupsCollection) PruneGroups(t time.Time) {
	// Directs matches have longer retention times.
	gc.pruneGroupsBefore(0, 0, t.Add(-1*legacyDirectMatchLongTimeDelta))
	// Fuzzy matches have shorter retention times.
	gc.pruneGroupsBefore(1, math.Inf(1), t.Add(-1*legacyFuzzyMatchTimeDelta))
}

func (gc *legacyGroupsCollection) pruneGroupsBefore(minDistance, maxDistance float64, t time.Time) {
	mt := model.TimeFromUnixNano(t.UnixNano())

	newGroups := make([]*legacyGroupMatcher, 0, len(gc.Groups))

	for _, g := range gc.Groups {
		if g.Distance >= minDistance && g.Distance <= maxDistance && g.Modified.Before(mt) {
			continue
		}
		newGroups = append(newGroups, g)
	}
	gc.Groups = newGroups
}

func (gc *legacyGroupsCollection) tryMatchIntervals(intervals []Interval) ([]legacyGroupedInterval, []Interval) {
	var ret []legacyGroupedInterval
	var unmatched []Interval
	for _, i := range intervals {
		matchedGroup := gc.bestMatch(i)
		if matchedGroup == nil {
			unmatched = append(unmatched, i)
			continue
		}

		if matchedGroup.Distance > 0 {
			// We don't update modified time for flapping alerts,
			matchedGroup.Modified = i.Start
		}
		matchedGroup.End = max(matchedGroup.End, i.End)

		newGroupedIntervals := gc.addIntervalsGroups([]Interval{i}, matchedGroup)
		ret = append(ret, newGroupedIntervals...)
	}
	return ret, unmatched
}

func (gc *legacyGroupsCollection) newRootGroup(i Interval, inactive bool) *legacyGroupMatcher {
	rootGroupID := uuid.New().String()

	ret := legacyGroupMatcher{
		GroupID:     rootGroupID,
		RootGroupID: rootGroupID,
		Start:       i.Start,
		Modified:    i.Start,
		End:         i.End,
		Distance:    math.Inf(1),
	}
	if inactive {
		// For inactive group, we set the modified to 0 so that it doesn't match
		// any new alert.
		ret.Modified = 0
	}

	gc.AddGroup(&ret)
	return &ret
}

func (gc *legacyGroupsCollection) addIntervalsGroups(intervals []Interval, groupMatcher *legacyGroupMatcher) []legacyGroupedInterval {
	if len(intervals) == 0 {
		return nil
	}
	newGc := &legacyGroupsCollection{}

	isWatchdogGroup := false
	for _, i := range intervals {
		if legacyWatchdogAlert(i) {
			isWatchdogGroup = true
			break
		}
	}

	ret := make([]legacyGroupedInterval, 0, len(intervals))
	if groupMatcher == nil && !isWatchdogGroup {
		// If not provided, create a new root group for all intervals in this batch.
		// We don't do this if watchdog is present in the group, as it indicates
		// the alerts are together by accident (perhaps due to a restart or data
		// outage).
		groupMatcher = newGc.newRootGroup(intervals[0], isWatchdogGroup)
	}

	for _, i := range intervals {
		var iGroupMatcher *legacyGroupMatcher
		iGroupMatcher = groupMatcher

		if iGroupMatcher == nil {
			iGroupMatcher = newGc.bestMatch(i)
		}

		if iGroupMatcher == nil {
			iGroupMatcher = newGc.newRootGroup(i, isWatchdogGroup)
		}

		// If we didn't have a direct match, add additional fuzzy matchers
		// for this interval. If Distance is 0, we assume the fuzzy matchers
		// to be already present.
		if iGroupMatcher.Distance > 0 {
			newGroupCands := legacyAlertGroupMatchers(i)
			for _, g := range newGroupCands {
				if g.Distance == iGroupMatcher.Distance && legacyIsSubsetOf(iGroupMatcher, g) {
					legacyExpandMatchers(iGroupMatcher, g.Matchers)
					if g.Distance > 0 {
						// We don't update modified time for flapping alerts,
						// as we don't consider that being a significant change
						// for the group.
						iGroupMatcher.Modified = i.Start
					}
					iGroupMatcher.End = max(iGroupMatcher.End, i.End)
				} else {
					g.RootGroupID = iGroupMatcher.RootGroupID
					newGc.AddGroup(g)
				}
			}
		}

		ret = append(ret, legacyGroupedInterval{i, iGroupMatcher})
	}
	for _, g := range newGc.Groups {
		gc.AddGroup(g)
	}
	return ret
}

func (gc *legacyGroupsCollection) bestMatch(interval Interval) *legacyGroupMatcher {
	matches := gc.matches(interval)
	var directLongMatch *legacyMatch
	var shortCandidates []legacyMatch
	var shortMatch *legacyMatch
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].TimeDist < matches[j].TimeDist
	})

	for _, m := range matches {
		if m.TimeDist <= legacyFuzzyMatchTimeDelta {
			shortCandidates = append(shortCandidates, m)
			continue
		}

		if m.TimeDist <= legacyDirectMatchLongTimeDelta && m.GroupMatcher.Distance == 0 {
			directLongMatch = &m
			// Given matches are sorted by time and we crossed the legacyFuzzyMatchTimeDelta,
			// there is no change to match anything better at this point.
			break
		}
	}

	if len(shortCandidates) > 0 {
		// Try to find the match with the smallest distance.
		// In case of the same distance, it's fine for the first match to win,
		// as we sort the matches by time initially.
		shortMatch = &shortCandidates[0]
		for i := 1; i < len(shortCandidates); i++ {
			if shortCandidates[i].GroupMatcher.Distance < shortMatch.GroupMatcher.Distance {
				shortMatch = &shortCandidates[i]
			}
		}
	}

	if shortMatch != nil {
		return shortMatch.GroupMatcher
	}
	if directLongMatch != nil {
		return directLongMatch.GroupMatcher
	}

	return nil
}

func (gc *legacyGroupsCollection) matches(interval Interval) []legacyMatch {
	var ret []legacyMatch
	allLabels := interval.Metric
	fuzzyLabels := legacyAlertFuzzyLabels(interval)
	for _, g := range gc.Groups {
		var timeDist time.Duration
		if g.Distance == 0 {
			// for direct matches, we compare with the end of the interval
			timeDist = interval.Start.Sub(g.End)
		} else {
			// In fuzzy matching, we compare with the last time the group was modified.
			timeDist = interval.Start.Sub(g.Modified)
		}

		if interval.Start < g.Start {
			// We don't consider groups from the future
			continue
		}

		// Pure time-based grouping
		if g.Distance == math.Inf(1) && timeDist <= legacyTimeMatchTimeDelta {
			ret = append(ret, legacyMatch{g, timeDist})
			continue
		}

		labels := allLabels
		// For fuzzy matching, we use only a subset of labels that can be overriden
		// on per-alert basis.
		if g.Distance >= 2 {
			labels = fuzzyLabels
		}
		for _, m := range g.Matchers {
			if matched, _ := m.Matches(labels); matched {
				ret = append(ret, legacyMatch{g, timeDist})
				// We found a match for this group: no need to check other matchers.
				break
			}
		}
	}
	return ret
}

// Default rules preserve legacy grouping while pruning between batches.
func TestDefaultRulesLegacyEquivalence(t *testing.T) {
	start := model.TimeFromUnixNano(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	for _, watchdog := range []bool{false, true} {
		t.Run(fmt.Sprintf("watchdog=%v", watchdog), func(t *testing.T) {
			current := newTestGroupsCollection(t)
			previous := &legacyGroupsCollection{}
			currentIDs, previousIDs := map[string]int{}, map[string]int{}
			compare := func(intervals []Interval) {
				t.Helper()
				got := current.ProcessIntervalsBatch(intervals)
				want := previous.ProcessIntervalsBatch(intervals)
				require.Equal(t, canonicalLegacyIntervals(want, previousIDs), canonicalIntervals(got, currentIDs))
				current.PruneGroups(intervals[0].Start.Time())
				previous.PruneGroups(intervals[0].Start.Time())
			}
			initial := []Interval{
				{Metric: model.LabelSet{"alertname": "A", "namespace": "ns1", "node": "n1"}, Start: start, End: start.Add(2 * time.Hour)},
				{Metric: model.LabelSet{"alertname": "B", "namespace": "ns2", "node": "n2"}, Start: start, End: start},
			}
			if watchdog {
				initial = append(initial, Interval{Metric: model.LabelSet{"alertname": "Watchdog", "namespace": "openshift-monitoring"}, Start: start, End: start})
			}
			compare(initial)
			for _, delta := range []time.Duration{15*time.Minute - time.Millisecond, 15 * time.Minute, 15*time.Minute + time.Millisecond, 24*time.Hour - time.Millisecond, 24 * time.Hour, 24*time.Hour + time.Millisecond, 120 * time.Hour, 120*time.Hour + time.Millisecond} {
				compare([]Interval{{Metric: model.LabelSet{"alertname": "A", "namespace": "ns1", "node": "n1"}, Start: start.Add(delta), End: start.Add(delta)}})
				compare([]Interval{{Metric: model.LabelSet{"alertname": "New", "namespace": "ns2"}, Start: start.Add(delta), End: start.Add(delta)}})
			}
		})
	}
	for seed := int64(0); seed < 20; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			current := newTestGroupsCollection(t)
			previous := &legacyGroupsCollection{}
			currentIDs, previousIDs := map[string]int{}, map[string]int{}
			now := start
			for step := 0; step < 80; step++ {
				now = now.Add(time.Duration(1+rng.Intn(48)) * time.Hour)
				var intervals []Interval
				for n := 0; n < 1+rng.Intn(4); n++ {
					namespace := fmt.Sprintf("ns%d", rng.Intn(3))
					labels := model.LabelSet{"alertname": model.LabelValue("Alert-" + namespace), "namespace": model.LabelValue(namespace), "node": model.LabelValue(fmt.Sprintf("node%d", rng.Intn(3)))}
					intervals = append(intervals, Interval{Metric: labels, Start: now, End: now.Add(time.Duration(rng.Intn(60)) * time.Minute)})
				}
				if rng.Intn(4) == 0 {
					intervals = append(intervals, Interval{Metric: model.LabelSet{"alertname": "Watchdog", "namespace": "openshift-monitoring"}, Start: now, End: now})
				}
				got := current.ProcessIntervalsBatch(intervals)
				want := previous.ProcessIntervalsBatch(intervals)
				require.Equal(t, canonicalLegacyIntervals(want, previousIDs), canonicalIntervals(got, currentIDs), "step %d", step)
				current.PruneGroups(now.Time())
				previous.PruneGroups(now.Time())
			}
		})
	}
}

func canonicalID(id string, ids map[string]int) int {
	if n, found := ids[id]; found {
		return n
	}
	n := len(ids)
	ids[id] = n
	return n
}

func canonicalIntervals(intervals []GroupedInterval, ids map[string]int) []string {
	var result []string
	for _, i := range intervals {
		result = append(result, fmt.Sprintf("%s:%d:%d:incident=%d", i.Metric, i.Start, i.End, canonicalID(i.GroupMatcher.RootGroupID, ids)))
	}
	return result
}

func canonicalLegacyIntervals(intervals []legacyGroupedInterval, ids map[string]int) []string {
	var result []string
	for _, i := range intervals {
		result = append(result, fmt.Sprintf("%s:%d:%d:incident=%d", i.Metric, i.Start, i.End, canonicalID(i.GroupMatcher.RootGroupID, ids)))
	}
	return result
}

func canonicalLegacySelectors(groups []*legacyGroupMatcher) []string {
	var result []string
	for _, group := range groups {
		for _, matcher := range group.Matchers {
			result = append(result, fmt.Sprintf("%v:%s", group.Distance, matcher.Labels))
		}
	}
	slices.Sort(result)
	return result
}

func canonicalSelectors(groups []*GroupMatcher) []string {
	var result []string
	for _, group := range groups {
		// The fallback exact strategy duplicates selectors, not their semantics.
		if group.strategy.Order == 4 {
			continue
		}
		distance := map[string]int{"exact": 0, "allOf": 1, "anyOf": 2}[group.strategy.operator]
		for _, matcher := range group.Matchers {
			result = append(result, fmt.Sprintf("%v:%s", distance, matcher.Labels))
		}
	}
	slices.Sort(result)
	return result
}

func TestDefaultRulesHistoricalLegacyEquivalence(t *testing.T) {
	start := model.TimeFromUnixNano(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	current := newTestGroupsCollection(t)
	previous := &legacyGroupsCollection{}
	currentIDs, previousIDs := map[string]int{}, map[string]int{}
	rangeVector := utils.RelativeIntervalsToRangeVectors(alertsIntervals, start, time.Minute)
	current.processHistoricalAlerts(rangeVector)
	previous.processHistoricalAlerts(rangeVector)
	require.Equal(t, canonicalLegacySelectors(previous.Groups), canonicalSelectors(current.Groups))
	current = newTestGroupsCollection(t)
	previous = &legacyGroupsCollection{}
	for _, change := range MetricsChanges(rangeVector) {
		got := current.ProcessIntervalsBatch(change.Intervals)
		want := previous.ProcessIntervalsBatch(change.Intervals)
		require.Equal(t, canonicalLegacyIntervals(want, previousIDs), canonicalIntervals(got, currentIDs))
	}
}

func TestDefaultRulesAlertsBatchLegacyEquivalence(t *testing.T) {
	current := newTestGroupsCollection(t)
	previous := &legacyGroupsCollection{}
	currentIDs, previousIDs := map[string]int{}, map[string]int{}
	start := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	canonicalAlerts := func(alerts []model.LabelSet, ids map[string]int) []string {
		var result []string
		for _, alert := range alerts {
			labels := maps.Clone(alert)
			labels["group_id"] = model.LabelValue(fmt.Sprintf("incident-%d", canonicalID(string(labels["group_id"]), ids)))
			result = append(result, labels.String())
		}
		return result
	}
	for _, delta := range []time.Duration{0, time.Minute, 15 * time.Minute, time.Hour, 25 * time.Hour} {
		input := []model.LabelSet{
			{"alertname": "Watchdog", "namespace": "openshift-monitoring"},
			{"alertname": "A", "namespace": "ns1", "node": "n1"},
			{"alertname": "B", "namespace": "ns2", "node": "n2"},
		}
		clone := func() []model.LabelSet {
			var result []model.LabelSet
			for _, labels := range input {
				result = append(result, maps.Clone(labels))
			}
			return result
		}
		got := current.ProcessAlertsBatch(clone(), start.Add(delta))
		want := previous.ProcessAlertsBatch(clone(), start.Add(delta))
		require.Equal(t, canonicalAlerts(want, previousIDs), canonicalAlerts(got, currentIDs))

	}
}

func TestDefaultRulesMatcherGenerationLegacyEquivalence(t *testing.T) {
	gc := newTestGroupsCollection(t)
	for _, labels := range []model.LabelSet{
		{}, {"alertname": "A"}, {"namespace": "ns"},
		{"alertname": "A", "namespace": "ns", "node": "n1", "service": "svc"},
		{"alertname": "Watchdog", "namespace": "openshift-monitoring"},
		{"alertname": "Watchdog", "namespace": "other"},
		{"alertname": "AlertmanagerReceiversNotConfigured", "namespace": "openshift-monitoring"},
		{"alertname": "AlertmanagerReceiversNotConfigured", "namespace": "other"},
	} {
		t.Run(labels.String(), func(t *testing.T) {
			interval := Interval{Metric: labels, Start: 10, End: 20}
			got := gc.rules.alertGroupMatchers(interval)
			want := legacyAlertGroupMatchers(interval)
			require.Equal(t, canonicalLegacySelectors(want), canonicalSelectors(got))
		})
	}
}

func TestDefaultRulesSelectionLegacyEquivalence(t *testing.T) {
	start := model.TimeFromUnixNano(time.Date(2024, 7, 10, 0, 0, 0, 0, time.UTC).UnixNano())
	for _, test := range []struct {
		name        string
		olderAge    time.Duration
		olderLabels model.LabelSet
		recent      bool
		temporal    bool
		endAge      time.Duration
		want        string
	}{
		{name: "recent exact precedes nearer fuzzy", olderAge: 20 * time.Hour, recent: true, want: "older"},
		{name: "recent fuzzy precedes old exact", olderAge: 30 * time.Hour, recent: true, want: "recent"},
		{name: "exact at 120h", olderAge: 120 * time.Hour, want: "older"},
		{name: "exact beyond 120h", olderAge: 120*time.Hour + time.Millisecond},
		{name: "fuzzy at 24h", olderAge: 24 * time.Hour, olderLabels: model.LabelSet{"alertname": "B", "namespace": "ns", "node": "n2"}, want: "older"},
		{name: "fuzzy beyond 24h", olderAge: 24*time.Hour + time.Millisecond, olderLabels: model.LabelSet{"alertname": "B", "namespace": "ns", "node": "n2"}},
		{name: "temporal at 15m", olderAge: 15 * time.Minute, temporal: true, want: "older"},
		{name: "temporal beyond 15m", olderAge: 15*time.Minute + time.Millisecond, temporal: true},
		{name: "exact uses End rather than Modified", olderAge: 200 * time.Hour, endAge: time.Hour, want: "older"},
		{name: "future group rejected", olderAge: -time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := newTestGroupsCollection(t)
			previous := &legacyGroupsCollection{}
			target := Interval{Metric: model.LabelSet{"alertname": "A", "namespace": "ns", "node": "n1"}, Start: start, End: start}
			labels := test.olderLabels
			if labels == nil {
				labels = target.Metric
			}
			older := Interval{Metric: labels, Start: start.Add(-test.olderAge), End: start.Add(-test.olderAge)}
			if test.endAge != 0 {
				older.End = start.Add(-test.endAge)
			}
			gotGroups := current.rules.alertGroupMatchers(older)
			wantGroups := legacyAlertGroupMatchers(older)
			if test.temporal {
				gotGroups = current.rules.rootGroupMatchers(older, "older")
				wantGroups = []*legacyGroupMatcher{{Start: older.Start, Modified: older.Start, End: older.End, Distance: math.Inf(1)}}
			}
			for _, group := range gotGroups {
				group.RootGroupID = "older"
				current.AddGroup(group)
			}
			for _, group := range wantGroups {
				group.RootGroupID = "older"
				previous.AddGroup(group)
			}
			if test.recent {
				recent := Interval{Metric: model.LabelSet{"alertname": "B", "namespace": "ns", "node": "n2"}, Start: start.Add(-time.Hour), End: start.Add(-time.Hour)}
				for _, group := range current.rules.alertGroupMatchers(recent) {
					group.RootGroupID = "recent"
					current.AddGroup(group)
				}
				for _, group := range legacyAlertGroupMatchers(recent) {
					group.RootGroupID = "recent"
					previous.AddGroup(group)
				}
			}
			got, want := current.bestMatch(target), previous.bestMatch(target)
			if test.want == "" {
				require.Nil(t, want)
				require.Nil(t, got)
			} else {
				require.NotNil(t, want)
				require.NotNil(t, got)
				require.Equal(t, test.want, want.RootGroupID)
				require.Equal(t, want.RootGroupID, got.RootGroupID)
			}
		})
	}
}

func TestDefaultRulesRetentionMatchesLegacy(t *testing.T) {
	current := newTestGroupsCollection(t)
	previous := &legacyGroupsCollection{}
	start := model.TimeFromUnixNano(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	intervals := []Interval{{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start, End: start.Add(200 * time.Hour)}}
	current.ProcessIntervalsBatch(intervals)
	previous.ProcessIntervalsBatch(intervals)
	current.PruneGroups(start.Add(201 * time.Hour).Time())
	previous.PruneGroups(start.Add(201 * time.Hour).Time())
	// Recent End does not renew retention when Modified remains unchanged.
	require.Empty(t, current.Groups)
	require.Empty(t, previous.Groups)
}

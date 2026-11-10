package processor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func testRules(t *testing.T, specJSON string) *RulesSnapshot {
	t.Helper()
	spec, err := decodeRuleSpec([]byte(specJSON))
	require.NoError(t, err)
	rule, err := parseRule("custom", spec)
	require.NoError(t, err)
	return &RulesSnapshot{rules: []*parsedRule{rule}}
}

func TestParseRuleValidation(t *testing.T) {
	for name, data := range map[string]string{
		"missing strategies":   `{}`,
		"empty strategies":     `{"strategies":[]}`,
		"missing match":        `{"strategies":[{"within":"24h","from":"start"}]}`,
		"null match":           `{"strategies":[{"match":null,"within":"24h","from":"start"}]}`,
		"null exact":           `{"strategies":[{"match":{"exact":null},"within":"24h","from":"start"}]}`,
		"null allOf":           `{"strategies":[{"match":{"allOf":null},"within":"24h","from":"start"}]}`,
		"null anyOf":           `{"strategies":[{"match":{"anyOf":null},"within":"24h","from":"start"}]}`,
		"nonstring key":        `{"strategies":[{"match":{"allOf":[1]},"within":"24h","from":"start"}]}`,
		"false exact":          `{"strategies":[{"match":{"exact":false},"within":"24h","from":"start"}]}`,
		"combined operators":   `{"strategies":[{"match":{"exact":true,"allOf":["node"]},"within":"24h","from":"end"}]}`,
		"empty allOf":          `{"strategies":[{"match":{"allOf":[]},"within":"24h","from":"start"}]}`,
		"empty anyOf":          `{"strategies":[{"match":{"anyOf":[]},"within":"24h","from":"start"}]}`,
		"duplicate keys":       `{"strategies":[{"match":{"allOf":["node","node"]},"within":"24h","from":"start"}]}`,
		"empty label key":      `{"strategies":[{"match":{"anyOf":[""]},"within":"24h","from":"start"}]}`,
		"empty selector":       `{"strategies":[{"match":{"anyOf":["node"]},"excludeAlerts":[{}],"within":"24h","from":"start"}]}`,
		"temporal exclusion":   `{"strategies":[{"match":{},"excludeAlerts":[{"alertname":"A"}],"within":"24h","from":"start"}]}`,
		"unsupported operator": `{"strategies":[{"match":{"nOf":{"keys":["node"],"min":1}},"within":"24h","from":"start"}]}`,
		"missing within":       `{"strategies":[{"match":{},"from":"start"}]}`,
		"missing from":         `{"strategies":[{"match":{},"within":"2h"}]}`,
		"invalid from":         `{"strategies":[{"match":{},"within":"2h","from":"creation"}]}`,
		"zero within":          `{"strategies":[{"match":{},"within":"0h","from":"start"}]}`,
		"negative within":      `{"strategies":[{"match":{},"within":"-1h","from":"start"}]}`,
		"invalid within":       `{"strategies":[{"match":{},"within":"tomorrow","from":"start"}]}`,
		"duration overflow":    `{"strategies":[{"match":{},"within":"999999999999999h","from":"start"}]}`,
		"unsupported unit":     `{"strategies":[{"match":{},"within":"1d","from":"start"}]}`,
		"priority overflow":    `{"priority":2147483648,"strategies":[{"match":{},"within":"1h","from":"start"}]}`,
		"priority too high":    `{"priority":101,"strategies":[{"match":{},"within":"1h","from":"start"}]}`,
		"priority negative":    `{"priority":-1,"strategies":[{"match":{},"within":"1h","from":"start"}]}`,
		"unsupported within":   `{"within":"2h","strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			spec, err := decodeRuleSpec([]byte(data))
			if err == nil {
				_, err = parseRule("custom", spec)
			}
			require.Error(t, err)
		})
	}
	spec, err := decodeRuleSpec([]byte(`{"strategies":[{"match":{},"within":"24h","from":"start"}]}`))
	require.NoError(t, err)
	_, err = parseRule("", spec)
	require.Error(t, err)
	_, err = NewGroupsCollection(nil)
	require.Error(t, err)
	_, err = NewGroupsCollection(&RulesSnapshot{})
	require.Error(t, err)
}

func TestCustomRulesReplaceBuiltinMatchers(t *testing.T) {
	rules := testRules(t, `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`)
	gc, err := NewGroupsCollection(rules)
	require.NoError(t, err)
	start := model.Time(1_000_000)
	first := gc.ProcessIntervalsBatch([]Interval{{Metric: model.LabelSet{"alertname": "A", "namespace": "same", "node": "n1"}, Start: start, End: start}})
	second := gc.ProcessIntervalsBatch([]Interval{{Metric: model.LabelSet{"alertname": "A", "namespace": "same", "node": "n2"}, Start: start.Add(time.Minute), End: start.Add(time.Minute)}})
	require.NotEqual(t, first[0].GroupMatcher.RootGroupID, second[0].GroupMatcher.RootGroupID)
	third := gc.ProcessIntervalsBatch([]Interval{{Metric: model.LabelSet{"alertname": "Different", "namespace": "other", "node": "n1"}, Start: start.Add(time.Hour), End: start.Add(time.Hour)}})
	require.Equal(t, first[0].GroupMatcher.RootGroupID, third[0].GroupMatcher.RootGroupID)
	for _, g := range gc.Groups {
		require.Equal(t, "allOf", g.strategy.operator)
		for _, matcher := range g.Matchers {
			require.Len(t, matcher.Labels, 1)
			require.Contains(t, matcher.Labels, model.LabelName("node"))
		}
	}
}

func TestExcludedAlertsCannotMatchExistingFuzzyMatcher(t *testing.T) {
	gc := newTestGroupsCollection(t)
	start := model.Time(1_000_000)
	matchers := gc.rules.alertGroupMatchers(Interval{Metric: model.LabelSet{"alertname": "A", "namespace": "openshift-monitoring"}, Start: start, End: start})
	for _, matcher := range matchers {
		if matcher.strategy.operator == "anyOf" {
			gc.AddGroup(matcher)
		}
	}
	for _, name := range []model.LabelValue{"Watchdog", "AlertmanagerReceiversNotConfigured"} {
		interval := Interval{Metric: model.LabelSet{"alertname": name, "namespace": "openshift-monitoring"}, Start: start.Add(time.Hour), End: start.Add(time.Hour)}
		require.Empty(t, gc.matches(interval))
		for _, matcher := range gc.rules.alertGroupMatchers(interval) {
			require.NotEqual(t, "anyOf", matcher.strategy.operator)
		}
		interval.Metric["namespace"] = "elsewhere"
		generated := gc.rules.alertGroupMatchers(interval)
		// The allOf strategy generates no matcher for this alert: it lacks the
		// service/job/container labels the default allOf strategy requires.
		require.Len(t, generated, 4)
	}
}

func TestLoadRulesFileRejectsMultipleResources(t *testing.T) {
	data, err := os.ReadFile("../../manifests/backend/09_incident_rule_defaults.yaml")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(path, append(append(data, []byte("\n---\n")...), data...), 0600))
	_, err = LoadRulesFile(path)
	require.Error(t, err)
}

func TestRuleWindowConfiguration(t *testing.T) {
	for _, operator := range []string{`{"exact":true}`, `{"allOf":["namespace"]}`, `{"anyOf":["namespace"]}`, `{}`} {
		t.Run(operator, func(t *testing.T) {
			rules := testRules(t, `{"strategies":[{"match":`+operator+`,"within":"48h","from":"start"}]}`)
			gc, err := NewGroupsCollection(rules)
			require.NoError(t, err)
			start := model.TimeFromUnixNano(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())
			gc.ProcessIntervalsBatch([]Interval{{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start, End: start}})
			target := Interval{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start.Add(48 * time.Hour), End: start.Add(48 * time.Hour)}
			require.NotNil(t, gc.bestMatch(target))
			gc.PruneGroups(target.Start.Time())
			require.NotNil(t, gc.bestMatch(target))
			target.Start = target.Start.Add(time.Millisecond)
			require.Nil(t, gc.bestMatch(target))
			expires := start.Add(rules.rules[0].Strategies[0].Retention)
			gc.PruneGroups(expires.Time())
			require.NotEmpty(t, gc.Groups)
			gc.PruneGroups(expires.Add(time.Millisecond).Time())
			require.Empty(t, gc.Groups)
		})
	}
}

func TestFromControlsUpdatesIndependentlyOfOperator(t *testing.T) {
	for _, operator := range []string{`{"exact":true}`, `{"allOf":["namespace"]}`, `{"anyOf":["namespace"]}`, `{}`} {
		for _, from := range []string{"start", "end"} {
			t.Run(operator+"/"+from, func(t *testing.T) {
				rules := testRules(t, `{"strategies":[{"match":`+operator+`,"within":"8h","from":"`+from+`"}]}`)
				gc, err := NewGroupsCollection(rules)
				require.NoError(t, err)
				start := model.Time(1_000_000)
				first := Interval{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start, End: start.Add(4 * time.Hour)}
				gc.ProcessIntervalsBatch([]Interval{first})
				matcher := gc.Groups[0]
				next := Interval{Metric: first.Metric, Start: start.Add(time.Hour), End: start.Add(time.Hour)}
				grouped := gc.ProcessIntervalsBatch([]Interval{next})
				require.Same(t, matcher, grouped[0].GroupMatcher)
				require.Equal(t, first.End, matcher.End)
				if from == "start" {
					require.Equal(t, next.Start, matcher.Modified)
				} else {
					require.Equal(t, first.Start, matcher.Modified)
				}
				next.Start, next.End = start.Add(5*time.Hour), start.Add(6*time.Hour)
				gc.ProcessIntervalsBatch([]Interval{next})
				require.Equal(t, next.End, matcher.End)
				if from == "start" {
					require.Equal(t, next.Start, matcher.Modified)
				} else {
					require.Equal(t, first.Start, matcher.Modified)
				}
			})
		}
	}
}

func TestMatcherRetentionUsesStrategyReference(t *testing.T) {
	for _, test := range []struct {
		operator  string
		within    string
		retention time.Duration
	}{
		{`{"exact":true}`, "24h", 120 * time.Hour},
		{`{"exact":true}`, "120h", 120 * time.Hour},
		{`{"exact":true}`, "168h", 168 * time.Hour},
		{`{"allOf":["namespace"]}`, "2h", 24 * time.Hour},
		{`{"allOf":["namespace"]}`, "48h", 48 * time.Hour},
		{`{"anyOf":["namespace"]}`, "24h", 24 * time.Hour},
		{`{"anyOf":["namespace"]}`, "48h", 48 * time.Hour},
		{`{}`, "15m", 24 * time.Hour},
		{`{}`, "48h", 48 * time.Hour},
	} {
		for _, from := range []string{"start", "end"} {
			t.Run(test.operator+"/"+test.within+"/"+from, func(t *testing.T) {
				rules := testRules(t, `{"strategies":[{"match":`+test.operator+`,"within":"`+test.within+`","from":"`+from+`"}]}`)
				require.Equal(t, test.retention, rules.rules[0].Strategies[0].Retention)
				gc, err := NewGroupsCollection(rules)
				require.NoError(t, err)
				start := model.Time(1_000_000)
				duration := 100 * time.Hour
				gc.ProcessIntervalsBatch([]Interval{{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start, End: start.Add(duration)}})
				matcher := gc.Groups[0]
				// Retention expires relative to the strategy's reference time:
				// the interval end for end-anchored strategies, the creation
				// start (no further matches here) for start-anchored ones.
				expires := start.Add(duration + test.retention)
				if from == "start" {
					expires = start.Add(test.retention)
				}
				gc.PruneGroups(expires.Time())
				require.Contains(t, gc.Groups, matcher)
				gc.PruneGroups(expires.Add(time.Millisecond).Time())
				require.Empty(t, gc.Groups)
			})
		}
	}
}

func TestStrategyOrderOverridesOperatorCategories(t *testing.T) {
	gc, err := NewGroupsCollection(testRules(t, `{"strategies":[{"match":{"anyOf":["namespace"]},"within":"48h","from":"start"},{"match":{"exact":true},"within":"120h","from":"end"}]}`))
	require.NoError(t, err)
	start := model.Time(1_000_000)
	for _, matcher := range gc.rules.alertGroupMatchers(Interval{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start, End: start}) {
		gc.AddGroup(matcher)
	}
	matched := gc.bestMatch(Interval{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start.Add(time.Hour)})
	require.NotNil(t, matched)
	require.Equal(t, "anyOf", matched.strategy.operator)
}

func TestRulePriorityPrecedesStrategyOrder(t *testing.T) {
	for _, equalPriority := range []bool{false, true} {
		t.Run(fmt.Sprintf("equalPriority=%v", equalPriority), func(t *testing.T) {
			first := testRules(t, `{"priority":50,"strategies":[{"match":{"exact":true},"within":"24h","from":"end"}]}`).rules[0]
			priority := 10
			if equalPriority {
				priority = 50
			}
			spec, err := decodeRuleSpec([]byte(fmt.Sprintf(`{"priority":%d,"strategies":[{"match":{},"within":"1h","from":"start"}]}`, priority)))
			require.NoError(t, err)
			preferred, err := parseRule("a-preferred", spec)
			require.NoError(t, err)
			gc, err := NewGroupsCollection(&RulesSnapshot{rules: []*parsedRule{first, preferred}})
			require.NoError(t, err)
			start := model.Time(1_000_000)
			interval := Interval{Metric: model.LabelSet{"alertname": "A"}, Start: start, End: start}
			for _, matcher := range gc.rules.alertGroupMatchers(interval) {
				gc.AddGroup(matcher)
			}
			for _, matcher := range gc.rules.rootGroupMatchers(interval, "root") {
				gc.AddGroup(matcher)
			}
			matched := gc.bestMatch(interval)
			require.NotNil(t, matched)
			require.Equal(t, "a-preferred", matched.strategy.RuleName)
		})
	}
}

func TestExactFallbackKeepsLatestObservation(t *testing.T) {
	gc := newTestGroupsCollection(t)
	start := model.Time(1_000_000)
	interval := Interval{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start, End: start}
	first := gc.ProcessIntervalsBatch([]Interval{interval})[0].GroupMatcher.RootGroupID
	interval.Start, interval.End = start.Add(20*time.Hour), start.Add(20*time.Hour)
	gc.ProcessIntervalsBatch([]Interval{interval})
	interval.Start, interval.End = start.Add(100*time.Hour), start.Add(100*time.Hour)
	gc.PruneGroups(interval.Start.Time())
	matched := gc.bestMatch(interval)
	require.NotNil(t, matched)
	require.Equal(t, 4, matched.strategy.Order)
	require.Equal(t, first, matched.RootGroupID)
	require.Equal(t, start.Add(20*time.Hour), matched.End)
	require.Equal(t, start, matched.Modified)
}

func TestWatchdogDoesNotCreateTemporalMatchers(t *testing.T) {
	gc, err := NewGroupsCollection(testRules(t, `{"strategies":[{"match":{},"within":"168h","from":"start"}]}`))
	require.NoError(t, err)
	start := model.TimeFromUnixNano(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	first := gc.ProcessIntervalsBatch([]Interval{{Metric: model.LabelSet{"alertname": "Watchdog", "namespace": "openshift-monitoring"}, Start: start, End: start}})
	require.Empty(t, gc.Groups)
	next := gc.ProcessIntervalsBatch([]Interval{{Metric: model.LabelSet{"alertname": "A"}, Start: start.Add(time.Minute), End: start.Add(time.Minute)}})
	require.NotEqual(t, first[0].GroupMatcher.RootGroupID, next[0].GroupMatcher.RootGroupID)
}

func TestExactFallbackRestoresRecentPrecedenceAfterPruning(t *testing.T) {
	gc := newTestGroupsCollection(t)
	start := model.TimeFromUnixNano(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	interval := Interval{Metric: model.LabelSet{"alertname": "A", "namespace": "ns"}, Start: start, End: start}
	originalID := gc.ProcessIntervalsBatch([]Interval{interval})[0].GroupMatcher.RootGroupID
	gc.PruneGroups(start.Add(25 * time.Hour).Time())
	interval.Start, interval.End = start.Add(30*time.Hour), start.Add(30*time.Hour)
	fallback := gc.ProcessIntervalsBatch([]Interval{interval})[0].GroupMatcher
	require.Equal(t, 4, fallback.strategy.Order)
	gc.PruneGroups(interval.Start.Time())
	// A different incident supplies a more recent fuzzy candidate.
	other := Interval{Metric: model.LabelSet{"alertname": "B", "namespace": "ns"}, Start: start.Add(30*time.Hour + 30*time.Minute), End: start.Add(30*time.Hour + 30*time.Minute)}
	for _, matcher := range gc.rules.alertGroupMatchers(other) {
		matcher.RootGroupID = "other-incident"
		gc.AddGroup(matcher)
	}
	interval.Start, interval.End = start.Add(31*time.Hour), start.Add(31*time.Hour)
	matched := gc.bestMatch(interval)
	require.NotNil(t, matched)
	require.Equal(t, originalID, matched.RootGroupID)
	require.Equal(t, 0, matched.strategy.Order)
}

func TestRuleWindowMaximum(t *testing.T) {
	// The inclusive upper bound is accepted.
	_, err := NewGroupsCollection(testRules(t, `{"strategies":[{"match":{},"within":"168h","from":"start"}]}`))
	require.NoError(t, err)

	// Anything above it is invalid.
	spec, err := decodeRuleSpec([]byte(`{"strategies":[{"match":{},"within":"168h1m","from":"start"}]}`))
	require.NoError(t, err)
	_, err = parseRule("custom", spec)
	require.ErrorContains(t, err, "within must be at most")
}

// The Watchdog safeguard suppresses time-only grouping for the whole unmatched
// set: under a temporal-only configuration, Watchdog, A and B observed together
// produce three incidents regardless of Watchdog's position, while A and B
// alone share one incident through the time-only strategy.
func TestWatchdogSafeguardSeparatesUnmatchedAlerts(t *testing.T) {
	newTemporalOnly := func(t *testing.T) *GroupsCollection {
		t.Helper()
		gc, err := NewGroupsCollection(testRules(t, `{"strategies":[{"match":{},"within":"15m","from":"start"}]}`))
		require.NoError(t, err)
		return gc
	}
	start := model.TimeFromUnixNano(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	watchdog := Interval{Metric: model.LabelSet{"alertname": "Watchdog", "namespace": "openshift-monitoring"}, Start: start, End: start}
	disk := Interval{Metric: model.LabelSet{"alertname": "KubeNodeDown", "namespace": "openshift-monitoring"}, Start: start, End: start}
	network := Interval{Metric: model.LabelSet{"alertname": "NetworkError", "namespace": "openshift-monitoring"}, Start: start, End: start}

	// Without Watchdog, the time-only strategy groups the two alerts.
	got := newTemporalOnly(t).ProcessIntervalsBatch([]Interval{disk, network})
	require.Equal(t, got[0].GroupMatcher.RootGroupID, got[1].GroupMatcher.RootGroupID)

	// With Watchdog anywhere in the unmatched set, no temporal matchers are
	// created: all three alerts get separate incidents.
	for name, batch := range map[string][]Interval{
		"watchdog first":  {watchdog, disk, network},
		"watchdog middle": {disk, watchdog, network},
		"watchdog last":   {disk, network, watchdog},
	} {
		t.Run(name, func(t *testing.T) {
			got := newTemporalOnly(t).ProcessIntervalsBatch(batch)
			ids := map[string]struct{}{}
			for _, gi := range got {
				ids[gi.GroupMatcher.RootGroupID] = struct{}{}
			}
			require.Len(t, ids, 3, "each alert must get its own incident")
		})
	}
}

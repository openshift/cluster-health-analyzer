package processor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/prometheus/common/model"
	"go.yaml.in/yaml/v3"

	"maps"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
)

// IncidentRuleSpec configures matcher generation, precedence, and time windows.
type IncidentRuleSpec struct {
	Priority   int32          `json:"priority,omitempty" yaml:"priority,omitempty"`
	Strategies []StrategySpec `json:"strategies" yaml:"strategies"`
}

type StrategySpec struct {
	Within        string                 `json:"within" yaml:"within"`
	From          string                 `json:"from" yaml:"from"`
	ExcludeAlerts []model.LabelSet       `json:"excludeAlerts,omitempty" yaml:"excludeAlerts,omitempty"`
	Match         map[string]interface{} `json:"match" yaml:"match"`
}

const (
	windowFromEnd         = "end"
	windowFromStart       = "start"
	withinDurationPattern = `^([0-9]+(\.[0-9]+)?(h|m|s))+$`
)

// MaxRuleWindow bounds the matching window of a strategy. It matches the
// history horizon loaded at startup, so every configured window can be
// reconstructed from the loaded history.
const MaxRuleWindow = 7 * 24 * time.Hour

var withinDurationRE = regexp.MustCompile(withinDurationPattern)

type parsedStrategy struct {
	Within    time.Duration
	Retention time.Duration
	From      string
	Priority  int32
	RuleName  string
	Order     int
	Expand    bool
	operator  string
	Keys      []model.LabelName
	Excluded  []common.LabelsSubsetMatcher
}

func (s *parsedStrategy) allows(labels model.LabelSet) bool {
	for _, excluded := range s.Excluded {
		if matched, _ := excluded.Matches(labels); matched {
			return false
		}
	}
	return true
}

type parsedRule struct {
	Name       string
	Priority   int32
	Strategies []*parsedStrategy
}

// RulesSnapshot is immutable after publication by the watcher.
type RulesSnapshot struct {
	rules []*parsedRule
}

func parseRule(name string, spec IncidentRuleSpec) (*parsedRule, error) {
	if name == "" {
		return nil, fmt.Errorf("rule name is required")
	}
	if len(spec.Strategies) == 0 {
		return nil, fmt.Errorf("at least one strategy is required")
	}

	rule := &parsedRule{Name: name, Priority: spec.Priority}
	for i, strategy := range spec.Strategies {
		if strategy.Match == nil {
			return nil, fmt.Errorf("strategy %d: match is required", i)
		}
		if len(strategy.Match) > 1 {
			return nil, fmt.Errorf("strategy %d: match operators cannot be combined", i)
		}
		if len(strategy.Within) > 32 || !withinDurationRE.MatchString(strategy.Within) {
			return nil, fmt.Errorf("strategy %d: within must be a positive duration using h, m, or s", i)
		}
		within, err := time.ParseDuration(strategy.Within)
		if err != nil || within <= 0 {
			return nil, fmt.Errorf("strategy %d: within must be a positive duration", i)
		}
		if within > MaxRuleWindow {
			return nil, fmt.Errorf("strategy %d: within must be at most %s", i, MaxRuleWindow)
		}
		if strategy.From != windowFromEnd && strategy.From != windowFromStart {
			return nil, fmt.Errorf("strategy %d: from must be end or start", i)
		}
		parsed := &parsedStrategy{
			Within: within, Retention: max(24*time.Hour, within), From: strategy.From, Priority: spec.Priority,
			RuleName: name, Order: i, Expand: true,
		}
		for operator, value := range strategy.Match {
			parsed.operator = operator
			switch operator {
			case "exact":
				if exact, ok := value.(bool); !ok || !exact {
					return nil, fmt.Errorf("strategy %d: exact must be true", i)
				}
				parsed.Expand = false
				parsed.Retention = max(120*time.Hour, within)
			case "allOf", "anyOf":
				keys, ok := value.([]interface{})
				if !ok || len(keys) == 0 {
					return nil, fmt.Errorf("strategy %d: label keys must be a nonempty array", i)
				}
				seen := make(map[string]bool, len(keys))
				for _, value := range keys {
					key, ok := value.(string)
					if !ok || !model.UTF8Validation.IsValidLabelName(key) || seen[key] {
						return nil, fmt.Errorf("strategy %d: label keys must be valid and unique strings", i)
					}
					seen[key] = true
					parsed.Keys = append(parsed.Keys, model.LabelName(key))
				}
			default:
				return nil, fmt.Errorf("strategy %d: unsupported match operator", i)
			}
		}
		if parsed.operator == "" && len(strategy.ExcludeAlerts) > 0 {
			return nil, fmt.Errorf("strategy %d: temporal strategy cannot filter individual alerts", i)
		}
		for _, labels := range strategy.ExcludeAlerts {
			if len(labels) == 0 {
				return nil, fmt.Errorf("strategy %d: excluded alert selector cannot be empty", i)
			}
			for key := range labels {
				if !model.UTF8Validation.IsValidLabelName(string(key)) {
					return nil, fmt.Errorf("strategy %d: excluded alert selector has an invalid label key", i)
				}
			}
			parsed.Excluded = append(parsed.Excluded, common.LabelsSubsetMatcher{Labels: labels})
		}
		rule.Strategies = append(rule.Strategies, parsed)
	}
	return rule, nil
}

// rulesConfig is the `rules` section of the cluster-health-analyzer ConfigMap.
type rulesConfig struct {
	Rules []ruleConfig `json:"rules" yaml:"rules"`
}

type ruleConfig struct {
	Name       string         `json:"name" yaml:"name"`
	Priority   int32          `json:"priority,omitempty" yaml:"priority,omitempty"`
	Strategies []StrategySpec `json:"strategies" yaml:"strategies"`
}

func decodeRuleSpec(data []byte) (IncidentRuleSpec, error) {
	var spec IncidentRuleSpec
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&spec)
	return spec, err
}

// parseRulesConfig parses the `rules.yaml` payload of the analyzer ConfigMap
// and returns the rules sorted by priority and name.
func parseRulesConfig(data []byte) ([]*parsedRule, error) {
	var config rulesConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("invalid rules config: %w", err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("invalid rules config: expected a single document")
	}
	if len(config.Rules) == 0 {
		return nil, fmt.Errorf("invalid rules config: at least one rule is required")
	}
	rules := make([]*parsedRule, 0, len(config.Rules))
	for i, entry := range config.Rules {
		rule, err := parseRule(entry.Name, IncidentRuleSpec{Priority: entry.Priority, Strategies: entry.Strategies})
		if err != nil {
			return nil, fmt.Errorf("invalid rule %d: %w", i, err)
		}
		rules = append(rules, rule)
	}
	sortRules(rules)
	return rules, nil
}

// LoadRulesFile loads the rules section of the analyzer config for offline
// simulation and tests. The file holds the same payload as the ConfigMap key.
func LoadRulesFile(path string) (*RulesSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Accept either a full ConfigMap manifest or the bare rules.yaml payload.
	var resource struct {
		Kind string            `yaml:"kind"`
		Data map[string]string `yaml:"data"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&resource); err == nil && resource.Kind == "ConfigMap" {
		var extra interface{}
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("expected a single config document")
		}
		payload, found := resource.Data[RulesConfigKey]
		if !found {
			return nil, fmt.Errorf("config is missing the %s key", RulesConfigKey)
		}
		data = []byte(payload)
	}
	rules, err := parseRulesConfig(data)
	if err != nil {
		return nil, err
	}
	return &RulesSnapshot{rules: rules}, nil
}
func NewGroupsCollection(rules *RulesSnapshot) (*GroupsCollection, error) {
	if rules == nil || len(rules.rules) == 0 {
		return nil, fmt.Errorf("no valid IncidentRules loaded")
	}
	return &GroupsCollection{rules: rules}, nil
}

// rootGroupMatchers attaches temporal matchers to a newly created incident.
func (rules *RulesSnapshot) rootGroupMatchers(interval Interval, rootGroupID string) []*GroupMatcher {
	var groups []*GroupMatcher
	for _, rule := range rules.rules {
		for _, strategy := range rule.Strategies {
			if strategy.operator != "" {
				continue
			}
			groups = append(groups, &GroupMatcher{
				GroupID: rootGroupID, RootGroupID: rootGroupID,
				matcherTimeline: &matcherTimeline{Start: interval.Start, Modified: interval.Start, End: interval.End},
				strategy:        strategy,
			})
		}
	}
	return groups
}

// hasAllLabels reports whether every key is present in the label set.
func hasAllLabels(labels model.LabelSet, keys []model.LabelName) bool {
	for _, key := range keys {
		if _, ok := labels[key]; !ok {
			return false
		}
	}
	return true
}

func (rules *RulesSnapshot) alertGroupMatchers(interval Interval) []*GroupMatcher {
	var groups []*GroupMatcher
	for _, rule := range rules.rules {
		// Equivalent exact strategies share observations so fallback windows stay current.
		exactTimelines := make(map[string]*matcherTimeline)
		for _, strategy := range rule.Strategies {
			if strategy.operator == "" || !strategy.allows(interval.Metric) {
				continue
			}
			var labels []model.LabelSet
			switch strategy.operator {
			case "exact":
				// Clone the labels: the caller may mutate the alert's map (for
				// example by adding group_id), and the matcher must keep the
				// observation's original labels.
				labels = append(labels, maps.Clone(interval.Metric))
			case "allOf":
				// All listed labels are mandatory: skip the strategy when any
				// is missing, so a missing label cannot narrow or empty the
				// selector.
				if hasAllLabels(interval.Metric, strategy.Keys) {
					labels = append(labels, getMapSubset(interval.Metric, strategy.Keys...))
				}
			case "anyOf":
				// Keep the legacy map iteration semantics for equally ranked matchers.
				for key, value := range getMapSubset(interval.Metric, strategy.Keys...) {
					labels = append(labels, model.LabelSet{key: value})
				}
			}
			for _, selector := range labels {
				timeline := &matcherTimeline{Start: interval.Start, Modified: interval.Start, End: interval.End}
				if strategy.operator == "exact" {
					key := strategy.From + fmt.Sprint(strategy.Excluded)
					if previous := exactTimelines[key]; previous != nil {
						timeline = previous
					} else {
						exactTimelines[key] = timeline
					}
				}
				groups = append(groups, &GroupMatcher{
					matcherTimeline: timeline,
					Matchers:        []common.LabelsSubsetMatcher{{Labels: selector}},
					strategy:        strategy,
				})
			}
		}
	}
	return groups
}

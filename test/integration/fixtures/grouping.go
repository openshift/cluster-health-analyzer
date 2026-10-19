package fixtures

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/openshift/cluster-health-analyzer/test/integration/framework"
)

const GroupingTestType = "incident-grouping-long"

type GroupingAlert struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Severity  string            `yaml:"severity"`
	Delay     time.Duration     `yaml:"delay"`
	Duration  string            `yaml:"duration"`
	Labels    map[string]string `yaml:"labels"`
}

func (a GroupingAlert) TestName(runID string) string {
	return a.Name + "Test" + runID
}

func (a GroupingAlert) Lifetime() (time.Duration, error) {
	if a.Duration == "-1" {
		return -1, nil
	}
	return time.ParseDuration(a.Duration)
}

type GroupingCheckpoint struct {
	After  time.Duration `yaml:"after"`
	Groups [][]int       `yaml:"groups"`
}

type GroupingScenario struct {
	ID          string               `yaml:"id"`
	Name        string               `yaml:"name"`
	Alerts      []GroupingAlert      `yaml:"alerts"`
	Checkpoints []GroupingCheckpoint `yaml:"checkpoints"`
}

func LoadGroupingScenarios() ([]GroupingScenario, error) {
	content, err := testDataFS.ReadFile("testdata/grouping_scenarios.yaml")
	if err != nil {
		return nil, err
	}
	var data struct {
		Scenarios []GroupingScenario `yaml:"scenarios"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode grouping scenarios: %w", err)
	}
	validID := regexp.MustCompile(`^integration-[a-z0-9]+(-[a-z0-9]+)*$`)
	validName := regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	validLabel := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	seen := make(map[string]bool)
	if len(data.Scenarios) == 0 {
		return nil, fmt.Errorf("no grouping scenarios")
	}
	for _, s := range data.Scenarios {
		if !validID.MatchString(s.ID) || seen[s.ID] || s.Name == "" || len(s.Alerts) == 0 || len(s.Checkpoints) == 0 {
			return nil, fmt.Errorf("invalid or duplicate grouping scenario %q", s.ID)
		}
		seen[s.ID] = true
		var start time.Duration
		for _, a := range s.Alerts {
			if !validName.MatchString(a.Name) || a.Namespace == "" || a.Delay < 0 || a.Delay > time.Duration(1<<63-1)-start {
				return nil, fmt.Errorf("invalid alert in scenario %q", s.ID)
			}
			start += a.Delay
			duration, err := a.Lifetime()
			if err != nil || (duration != -1 && (duration <= 0 || duration > time.Duration(1<<63-1)-start)) {
				return nil, fmt.Errorf("invalid duration in scenario %q alert %q", s.ID, a.Name)
			}
			if a.Severity != "none" && a.Severity != "info" && a.Severity != "warning" && a.Severity != "critical" {
				return nil, fmt.Errorf("invalid severity in scenario %q", s.ID)
			}
			for key := range a.Labels {
				if !validLabel.MatchString(key) || strings.HasPrefix(key, "__") || key == "alertname" || key == "namespace" || key == "severity" || key == "test_run" || key == "test_type" || key == "test_case" || key == "scenario_start" {
					return nil, fmt.Errorf("reserved or invalid label %q in scenario %q", key, s.ID)
				}
			}
		}
		// Checkpoints must coincide with cumulative alert offsets: the test evaluates
		// them only when an alert fires, so any other offset would be late or unverified.
		offsets := make(map[time.Duration]bool, len(s.Alerts))
		var cumulative time.Duration
		for _, a := range s.Alerts {
			cumulative += a.Delay
			offsets[cumulative] = true
		}
		previous := time.Duration(-1)
		for _, point := range s.Checkpoints {
			if point.After <= previous || len(point.Groups) == 0 || !offsets[point.After] {
				return nil, fmt.Errorf("invalid checkpoint in scenario %q: must match a cumulative alert offset", s.ID)
			}
			previous = point.After
		}
	}
	return data.Scenarios, nil
}

type groupingRule struct {
	Alert  string
	Expr   string
	Labels map[string]string
}

// RenderGroupingRule emits real alerting rules, not mock CHA incident metrics.
// startEpoch anchors the scenario clock; pass 0 for promtool replay and time.Now().Unix()
// for a real cluster, where a constant keeps scenario_start identical across replicas.
func RenderGroupingRule(ruleName, runID string, s GroupingScenario, startEpoch int64) (string, error) {
	if !regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(ruleName) || len(ruleName) > 63 || !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(runID) {
		return "", fmt.Errorf("invalid grouping rule name or run ID")
	}
	// Constant epoch so both Prometheus replicas record the same clock; a per-replica
	// time() value would yield different scenario_start labels and break dedup.
	clock := "vector(" + strconv.FormatInt(startEpoch, 10) + ")"
	elapsed := "(time() - " + clock + ")"
	var rules []groupingRule
	positions := make(map[string]int)
	var start time.Duration
	for _, a := range s.Alerts {
		start += a.Delay
		duration, err := a.Lifetime()
		if err != nil {
			return "", err
		}
		condition := elapsed + " >= " + strconv.FormatFloat(start.Seconds(), 'f', -1, 64)
		if duration != -1 {
			condition = "(" + condition + ") and on() (" + elapsed + " < " + strconv.FormatFloat((start+duration).Seconds(), 'f', -1, 64) + ")"
		}
		expr := "(" + clock + " and on() (" + condition + "))"
		labels := map[string]string{
			"namespace": a.Namespace, "severity": a.Severity,
			"test_run": runID, "test_type": GroupingTestType, "test_case": s.ID,
			"scenario_start": `{{ printf "%.0f" $value }}`,
		}
		for key, value := range a.Labels {
			labels[key] = value
		}
		name := a.TestName(runID)
		identity, err := json.Marshal(struct {
			Name   string
			Labels map[string]string
		}{name, labels})
		if err != nil {
			return "", err
		}
		if position, found := positions[string(identity)]; found {
			// Repeated identical windows must share one rule and exactly the same alert identity.
			rules[position].Expr += " or " + expr
		} else {
			positions[string(identity)] = len(rules)
			rules = append(rules, groupingRule{Alert: name, Expr: expr, Labels: labels})
		}
	}
	content, err := testDataFS.ReadFile("testdata/grouping_prometheusrule.yaml")
	if err != nil {
		return "", err
	}
	return framework.RenderTemplate(string(content), struct {
		RuleName, RunID, CaseID, TestType, Clock string
		Rules                                    []groupingRule
	}{ruleName, runID, s.ID, GroupingTestType, clock, rules})
}

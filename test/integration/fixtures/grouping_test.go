package fixtures

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestGroupingRuleFixtures(t *testing.T) {
	scenarios, err := LoadGroupingScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) != 5 {
		t.Fatalf("got %d scenarios", len(scenarios))
	}
	for _, s := range scenarios {
		t.Run(s.ID, func(t *testing.T) {
			const runID = "unit123"
			text, err := RenderGroupingRule(s.ID+"-"+runID, runID, s)
			if err != nil {
				t.Fatal(err)
			}
			var resource struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Namespace string            `yaml:"namespace"`
					Labels    map[string]string `yaml:"labels"`
				} `yaml:"metadata"`
				Spec yaml.Node `yaml:"spec"`
			}
			if err := yaml.Unmarshal([]byte(text), &resource); err != nil {
				t.Fatal(err)
			}
			if resource.Kind != "PrometheusRule" || resource.Metadata.Namespace != "openshift-monitoring" || resource.Metadata.Labels["test-type"] != GroupingTestType || resource.Metadata.Labels["grouping-case"] != s.ID {
				t.Fatal("incorrect resource or cleanup labels")
			}
			rules, err := yaml.Marshal(&resource.Spec)
			if err != nil {
				t.Fatal(err)
			}
			var parsed struct {
				Groups []struct {
					Rules []struct {
						Record string            `yaml:"record"`
						Alert  string            `yaml:"alert"`
						Expr   string            `yaml:"expr"`
						For    string            `yaml:"for"`
						Labels map[string]string `yaml:"labels"`
					} `yaml:"rules"`
				} `yaml:"groups"`
			}
			if err := yaml.Unmarshal(rules, &parsed); err != nil {
				t.Fatal(err)
			}
			if len(parsed.Groups) != 1 || len(parsed.Groups[0].Rules) < 2 {
				t.Fatal("missing clock or alerting rules")
			}
			entries := parsed.Groups[0].Rules
			if entries[0].Record != "cha_integration_start_seconds" || !strings.Contains(entries[0].Expr, "or on() vector(time())") {
				t.Fatal("missing persistent scenario clock")
			}
			replayRules := 0
			for _, rule := range entries[1:] {
				if rule.For != "30s" || rule.Labels["test_run"] != runID || rule.Labels["test_case"] != s.ID || rule.Labels["scenario_start"] != `{{ printf "%.0f" $value }}` {
					t.Fatal("incorrect pending period or alert identity")
				}
				if rule.Alert == s.Alerts[0].TestName(runID) {
					replayRules++
					if s.ID == "integration-exact-replay" && !strings.Contains(rule.Expr, " or ") {
						t.Fatal("exact replay windows were not merged into one rule")
					}
				}
			}
			if s.ID == "integration-exact-replay" && replayRules != 1 {
				t.Fatal("exact replay must retain one rule and label set")
			}
			if s.ID == "integration-subset-matching" && replayRules != 2 {
				t.Fatal("severity escalation requires two rules with the same alert name")
			}
			checkGroupingRuleTimeline(t, s, runID, rules)
		})
	}
	if _, err := RenderGroupingRule("invalid/name", "unit123", scenarios[0]); err == nil {
		t.Fatal("invalid resource name accepted")
	}
	if _, err := RenderGroupingRule("valid-name", "invalid\nrun", scenarios[0]); err == nil {
		t.Fatal("invalid run ID accepted")
	}
}

func checkGroupingRuleTimeline(t *testing.T, s GroupingScenario, runID string, rules []byte) {
	t.Helper()
	promtool, err := exec.LookPath("promtool")
	if err != nil {
		t.Log("promtool unavailable: skipped rule-engine evaluation; YAML/identity assertions still ran")
		return
	}
	directory := t.TempDir()
	rulePath := filepath.Join(directory, "rules.yaml")
	if err := os.WriteFile(rulePath, rules, 0600); err != nil {
		t.Fatal(err)
	}
	starts := make([]time.Duration, len(s.Alerts))
	ends := make([]time.Duration, len(s.Alerts))
	instants := []time.Duration{0}
	names := make(map[string]bool)
	for i, a := range s.Alerts {
		starts[i] = a.Delay
		if i > 0 {
			starts[i] += starts[i-1]
		}
		duration, err := a.Lifetime()
		if err != nil {
			t.Fatal(err)
		}
		ends[i] = -1
		if duration != -1 {
			ends[i] = starts[i] + duration
			instants = append(instants, ends[i]-15*time.Second, ends[i])
		}
		instants = append(instants, starts[i], starts[i]+15*time.Second, starts[i]+30*time.Second)
		names[a.TestName(runID)] = true
	}
	slices.Sort(instants)
	instants = slices.Compact(instants)
	checks := []map[string]any{}
	for _, at := range instants {
		if at < 0 {
			continue
		}
		for name := range names {
			expected := []map[string]any{}
			for i, a := range s.Alerts {
				if a.TestName(runID) != name || at < starts[i]+30*time.Second || (ends[i] != -1 && at >= ends[i]) {
					continue
				}
				labels := map[string]string{
					"namespace": a.Namespace, "severity": a.Severity, "test_run": runID,
					"test_type": GroupingTestType, "test_case": s.ID, "scenario_start": "0",
				}
				for key, value := range a.Labels {
					labels[key] = value
				}
				expected = append(expected, map[string]any{"exp_labels": labels, "exp_annotations": map[string]string{
					"summary": "Synthetic incident grouping test: " + name,
				}})
			}
			checks = append(checks, map[string]any{"eval_time": strconv.FormatFloat(at.Seconds(), 'f', -1, 64) + "s", "alertname": name, "exp_alerts": expected})
		}
	}
	testData, err := yaml.Marshal(map[string]any{
		"rule_files": []string{rulePath}, "evaluation_interval": "15s",
		"tests": []map[string]any{{"name": s.ID, "interval": "15s", "alert_rule_test": checks}},
	})
	if err != nil {
		t.Fatal(err)
	}
	testPath := filepath.Join(directory, "rules.test.yaml")
	if err := os.WriteFile(testPath, testData, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(promtool, "test", "rules", testPath).CombinedOutput(); err != nil {
		t.Fatalf("promtool timeline check failed: %v\n%s", err, output)
	}
}

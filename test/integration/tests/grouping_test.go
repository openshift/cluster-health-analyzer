package tests

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cluster-health-analyzer/test/integration/fixtures"
	"github.com/openshift/cluster-health-analyzer/test/integration/framework"
)

var _ = Describe("Long incident grouping scenarios", Serial, Ordered, Label("long", "grouping"), func() {
	DescribeTable("should preserve the expected incident groups", func(specCtx SpecContext, scenarioID string) {
		scenarios, err := fixtures.LoadGroupingScenarios()
		Expect(err).NotTo(HaveOccurred())
		position := slices.IndexFunc(scenarios, func(s fixtures.GroupingScenario) bool { return s.ID == scenarioID })
		Expect(position).To(BeNumerically(">=", 0))
		scenario := scenarios[position]
		runID := strconv.FormatInt(time.Now().UnixNano(), 10)
		ruleName := scenario.ID + "-" + runID
		testCluster := cluster.InNamespace("openshift-monitoring")
		selector := "test-type=" + fixtures.GroupingTestType + ",grouping-case=" + scenario.ID

		By("Cleaning up only previous rules for this grouping case")
		Expect(testCluster.DeleteByLabel(specCtx, "prometheusrule", selector)).To(Succeed())
		client, err := framework.NewPrometheusClient(cfg.ThanosURL, cfg.ThanosToken)
		Expect(err).NotTo(HaveOccurred())

		By("Rendering and applying the synthetic PrometheusRule")
		ruleYAML, err := fixtures.RenderGroupingRule(ruleName, runID, scenario)
		Expect(err).NotTo(HaveOccurred())
		names := make([]string, len(scenario.Alerts))
		for i, alert := range scenario.Alerts {
			names[i] = regexp.QuoteMeta(alert.TestName(runID))
		}
		pattern := "^(" + strings.Join(names, "|") + ")$"
		DeferCleanup(func(cleanupCtx SpecContext) {
			By("Removing the PrometheusRule created by this run")
			Eventually(func() (bool, error) {
				if err := testCluster.Delete(cleanupCtx, "prometheusrule", ruleName); err != nil {
					return false, err
				}
				return testCluster.IsGone(cleanupCtx, "prometheusrule", ruleName)
			}, time.Minute, 5*time.Second).WithContext(cleanupCtx).Should(BeTrue(), "Test PrometheusRule was not removed")

			By("Waiting for test alerts and current metrics to disappear")
			Eventually(func() (bool, error) {
				present, err := client.HasGroupingData(cleanupCtx, pattern, runID)
				return !present, err
			}, 5*time.Minute, 5*time.Second).WithContext(cleanupCtx).Should(BeTrue(), "Test alerts or metrics remain after cleanup")
		}, NodeTimeout(7*time.Minute))

		GinkgoWriter.Printf("Grouping test: scenario=%s rule=%s run=%s\n", scenario.ID, ruleName, runID)
		Expect(testCluster.Apply(specCtx, ruleYAML)).To(Succeed())

		observedAlerts := make([]*framework.Alert, len(scenario.Alerts))
		observedGroups := make([]string, len(scenario.Alerts))
		var start time.Time
		var offset time.Duration
		checkpoint := 0
		for i, alert := range scenario.Alerts {
			offset += alert.Delay
			By(fmt.Sprintf("Waiting for %s in %s at offset %s", alert.TestName(runID), alert.Namespace, offset))
			observedAlerts[i] = awaitGroupingAlert(specCtx, client, alert, runID, alert.Delay+5*time.Minute)
			clock, err := strconv.ParseInt(observedAlerts[i].Labels["scenario_start"], 10, 64)
			Expect(err).NotTo(HaveOccurred(), "Missing or invalid scenario clock")
			if start.IsZero() {
				start = time.Unix(clock, 0)
			} else {
				Expect(clock).To(Equal(start.Unix()), "Scenario clock changed during the run")
			}
			Expect(time.Now().Add(2*time.Second).Before(start.Add(offset))).To(BeFalse(), "Alert fired before its configured offset; check host/cluster clock synchronization")
			for key, value := range alert.Labels {
				Expect(observedAlerts[i].Labels).To(HaveKeyWithValue(key, value))
			}

			By("Waiting for CHA to expose a valid incident")
			incident := awaitGroupingIncident(specCtx, client, alert, runID)
			Expect(incident).To(framework.BeValidIncident())
			observedGroups[i] = incident.Labels["group_id"]
			GinkgoWriter.Printf("Observed window %d: alert=%s namespace=%s severity=%s group=%s\n", i, alert.TestName(runID), alert.Namespace, alert.Severity, observedGroups[i])

			if i == 2 && scenario.Alerts[0].Name == alert.Name && scenario.Alerts[0].Namespace == alert.Namespace {
				expected := maps.Clone(observedAlerts[0].Labels)
				expected["severity"] = alert.Severity
				Expect(maps.Equal(expected, observedAlerts[i].Labels)).To(BeTrue(), "Replay changed labels other than the configured severity")
			}
			for checkpoint < len(scenario.Checkpoints) && scenario.Checkpoints[checkpoint].After <= offset {
				assertGroupingPartition(observedGroups[:i+1], scenario.Checkpoints[checkpoint].Groups)
				checkpoint++
			}

			duration, err := alert.Lifetime()
			Expect(err).NotTo(HaveOccurred())
			if duration != -1 {
				By("Confirming the finite alert and its incident resolve before observing a replay")
				Eventually(func() (bool, error) {
					alerts, err := client.GetAlerts(specCtx, groupingPattern(alert, runID), time.Time{})
					if err != nil {
						return false, err
					}
					for _, a := range alerts {
						if a.Labels["namespace"] == alert.Namespace && a.Labels["severity"] == alert.Severity && a.Labels["test_run"] == runID {
							return false, nil
						}
					}
					incidents, err := client.GetIncidents(specCtx, groupingPattern(alert, runID), time.Time{})
					if err != nil {
						return false, err
					}
					for _, incident := range incidents {
						if incident.Labels["src_namespace"] == alert.Namespace && incident.Labels["src_severity"] == alert.Severity {
							return false, nil
						}
					}
					return true, nil
				}, duration+3*time.Minute, 5*time.Second).WithContext(specCtx).Should(BeTrue(), "Finite alert did not resolve")
			}
		}
		Expect(checkpoint).To(Equal(len(scenario.Checkpoints)), "Not all grouping checkpoints were verified")
	},
		Entry("integration-time-proximity", "integration-time-proximity", Label("integration-time-proximity"), SpecTimeout(40*time.Minute)),
		Entry("integration-fuzzy-namespace", "integration-fuzzy-namespace", Label("integration-fuzzy-namespace"), SpecTimeout(40*time.Minute)),
		Entry("integration-fuzzy-alert-name", "integration-fuzzy-alert-name", Label("integration-fuzzy-alert-name"), SpecTimeout(40*time.Minute)),
		Entry("integration-exact-replay", "integration-exact-replay", Label("integration-exact-replay"), SpecTimeout(40*time.Minute)),
		Entry("integration-subset-matching", "integration-subset-matching", Label("integration-subset-matching"), SpecTimeout(40*time.Minute)),
	)
})

func groupingPattern(alert fixtures.GroupingAlert, runID string) string {
	return "^" + regexp.QuoteMeta(alert.TestName(runID)) + "$"
}

func awaitGroupingAlert(ctx context.Context, client *framework.PrometheusClient, alert fixtures.GroupingAlert, runID string, timeout time.Duration) *framework.Alert {
	var result *framework.Alert
	Eventually(func() (bool, error) {
		alerts, err := client.GetAlerts(ctx, groupingPattern(alert, runID), time.Time{})
		if err != nil {
			return false, err
		}
		for _, candidate := range alerts {
			if candidate.Labels["namespace"] == alert.Namespace && candidate.Labels["severity"] == alert.Severity && candidate.Labels["test_run"] == runID {
				result = candidate
				return true, nil
			}
		}
		return false, nil
	}, timeout, 5*time.Second).WithContext(ctx).Should(BeTrue(), "Alert %s in %s (%s) did not fire", alert.TestName(runID), alert.Namespace, alert.Severity)
	return result
}

func awaitGroupingIncident(ctx context.Context, client *framework.PrometheusClient, alert fixtures.GroupingAlert, runID string) *framework.Incident {
	var result *framework.Incident
	Eventually(func() (bool, error) {
		incidents, err := client.GetIncidents(ctx, groupingPattern(alert, runID), time.Time{})
		if err != nil {
			return false, err
		}
		groups := make(map[string]bool)
		for _, candidate := range incidents {
			if candidate.Labels["src_namespace"] == alert.Namespace && candidate.Labels["src_severity"] == alert.Severity && candidate.Labels["group_id"] != "" {
				result = candidate
				groups[candidate.Labels["group_id"]] = true
			}
		}
		return len(groups) == 1, nil
	}, 3*time.Minute, 5*time.Second).WithContext(ctx).Should(BeTrue(), "Expected one group for %s in %s (%s)", alert.TestName(runID), alert.Namespace, alert.Severity)
	return result
}

func assertGroupingPartition(observed []string, expected [][]int) {
	groups := make(map[string]bool)
	covered := make(map[int]bool)
	for _, members := range expected {
		Expect(members).NotTo(BeEmpty())
		var group string
		for _, member := range members {
			Expect(member).To(BeNumerically(">=", 0))
			Expect(member).To(BeNumerically("<", len(observed)))
			Expect(covered[member]).To(BeFalse(), "Window appears in multiple expected groups")
			covered[member] = true
			Expect(observed[member]).NotTo(BeEmpty())
			if group == "" {
				group = observed[member]
			} else {
				Expect(observed[member]).To(Equal(group), "Windows %v should share a group", members)
			}
		}
		Expect(groups[group]).To(BeFalse(), "Expected groups unexpectedly merged")
		groups[group] = true
	}
	Expect(covered).To(HaveLen(len(observed)), "Checkpoint must cover all observed windows, including resolved ones")
}

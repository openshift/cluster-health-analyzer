package tests

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cluster-health-analyzer/test/integration/framework"
)

// Long scenario for non-retroactive rule changes: removing an exclusion from
// the custom IncidentRules ConfigMap must not move alerts that already have
// an incident. Mirrors the manual test plan in bug_da_verificare_e_fixare.md.
var _ = Describe("Long incident rules reload scenarios", Serial, Ordered, Label("long", "rules-reload"), func() {
	const customRulesConfigMap = "cluster-health-analyzer-custom-rules"

	renderCustomRules := func(runID, excludedAlert string) string {
		exclusion := ""
		if excludedAlert != "" {
			exclusion = fmt.Sprintf("          excludeAlerts:\n            - alertname: %s\n", excludedAlert)
		}
		return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  rules.yaml: |
    rules:
      - name: by-testgroup-%s
        priority: 0
        strategies:
          - match:
              allOf: [testgroup]
%s            within: 24h
            from: start
`, customRulesConfigMap, cfg.Namespace, runID, exclusion)
	}

	renderAlertRule := func(runID string, alertNames []string) string {
		rules := make([]string, 0, len(alertNames))
		for _, name := range alertNames {
			rules = append(rules, fmt.Sprintf(`        - alert: %s
          expr: vector(1)
          for: 30s
          labels:
            severity: warning
            test_run: %s
            testgroup: tg-%s
`, name, runID, runID))
		}
		return fmt.Sprintf(`apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: cha-rules-reload-%s
  namespace: openshift-monitoring
  labels:
    prometheus: k8s
    role: alert-rules
    test-type: cha-rules-reload
spec:
  groups:
    - name: cha-rules-reload-%s
      interval: 15s
      rules:
%s`, runID, runID, strings.Join(rules, ""))
	}

	It("should preserve incident assignments when an exclusion is removed", SpecTimeout(40*time.Minute), func(specCtx SpecContext) {
		runID := strconv.FormatInt(time.Now().UnixNano(), 10)
		alertA := "ChaRrldA" + runID
		alertB := "ChaRrldB" + runID
		alertC := "ChaRrldC" + runID
		pattern := "^(" + regexp.QuoteMeta(alertA) + "|" + regexp.QuoteMeta(alertB) + "|" + regexp.QuoteMeta(alertC) + ")$"
		chaNS := cluster.InNamespace(cfg.Namespace)
		monitoring := cluster.InNamespace("openshift-monitoring")
		client, err := framework.NewPrometheusClient(cfg.ThanosURL, cfg.ThanosToken)
		Expect(err).NotTo(HaveOccurred())

		By("Saving the custom rules ConfigMap state for restore")
		// The watcher only sees the ConfigMap in CHA's own namespace (or
		// INCIDENT_RULES_NAMESPACE), so the test reuses cfg.Namespace.
		gone, err := chaNS.IsGone(specCtx, "configmap", customRulesConfigMap)
		Expect(err).NotTo(HaveOccurred())
		preexisting := !gone
		var saved string
		if preexisting {
			saved, err = chaNS.Get(specCtx, "configmap", customRulesConfigMap)
			Expect(err).NotTo(HaveOccurred())
		}
		DeferCleanup(func(cleanupCtx SpecContext) {
			By("Removing the test PrometheusRule")
			Eventually(func() (bool, error) {
				if err := monitoring.Delete(cleanupCtx, "prometheusrule", "cha-rules-reload-"+runID); err != nil {
					return false, err
				}
				return monitoring.IsGone(cleanupCtx, "prometheusrule", "cha-rules-reload-"+runID)
			}, time.Minute, 5*time.Second).WithContext(cleanupCtx).Should(BeTrue(), "Test PrometheusRule was not removed")

			By("Waiting for test alerts and incidents to disappear")
			Eventually(func() (bool, error) {
				present, err := client.HasGroupingData(cleanupCtx, pattern, runID)
				return !present, err
			}, 5*time.Minute, 5*time.Second).WithContext(cleanupCtx).Should(BeTrue(), "Test alerts or incidents remain after cleanup")

			By("Restoring the custom rules ConfigMap")
			if preexisting {
				Expect(chaNS.Apply(cleanupCtx, saved)).To(Succeed())
			} else {
				Eventually(func() (bool, error) {
					if err := chaNS.Delete(cleanupCtx, "configmap", customRulesConfigMap); err != nil {
						return false, err
					}
					return chaNS.IsGone(cleanupCtx, "configmap", customRulesConfigMap)
				}, time.Minute, 5*time.Second).WithContext(cleanupCtx).Should(BeTrue(), "Test ConfigMap was not removed")
			}
		}, NodeTimeout(8*time.Minute))

		By("Applying custom rules that group by testgroup, excluding alert C")
		Expect(chaNS.Apply(specCtx, renderCustomRules(runID, alertC))).To(Succeed())
		// shortcut: no metric exposes the loaded rules, so wait for the
		// informer event plus one processing cycle before firing alerts;
		// firing earlier would let default rules assign the alerts first.
		time.Sleep(2 * time.Minute)

		By("Firing alerts A, B and C sharing the same testgroup")
		Expect(monitoring.Apply(specCtx, renderAlertRule(runID, []string{alertA, alertB, alertC}))).To(Succeed())

		groupsByAlert := func() (map[string]string, error) {
			incidents, err := client.GetIncidents(specCtx, pattern, time.Time{})
			if err != nil {
				return nil, err
			}
			groups := make(map[string]string)
			for _, incident := range incidents {
				name, group := incident.Labels["src_alertname"], incident.Labels["group_id"]
				if name == "" || group == "" {
					continue
				}
				if previous, seen := groups[name]; seen && previous != group {
					return nil, fmt.Errorf("alert %s is exposed with conflicting groups %s and %s", name, previous, group)
				}
				groups[name] = group
			}
			return groups, nil
		}

		By("Waiting for A and B to share an incident while C is excluded")
		Eventually(func() (bool, error) {
			groups, err := groupsByAlert()
			if err != nil {
				return false, err
			}
			return groups[alertA] != "" && groups[alertA] == groups[alertB] && groups[alertC] != "" && groups[alertC] != groups[alertA], nil
		}, 10*time.Minute, 10*time.Second).WithContext(specCtx).Should(BeTrue(),
			"Expected A and B grouped by testgroup and C in a separate incident; groups: %s", groupsByAlert)
		before, err := groupsByAlert()
		Expect(err).NotTo(HaveOccurred())

		By("Removing the exclusion from the custom rules")
		Expect(chaNS.Apply(specCtx, renderCustomRules(runID, ""))).To(Succeed())

		By("Verifying assignments survive the rule change")
		// Two refresh cycles for the reload, then the groups must not move.
		time.Sleep(2 * time.Minute)
		Consistently(func() (bool, error) {
			groups, err := groupsByAlert()
			if err != nil {
				return false, err
			}
			return groups[alertA] == before[alertA] && groups[alertB] == before[alertB] && groups[alertC] == before[alertC], nil
		}, 3*time.Minute, 15*time.Second).WithContext(specCtx).Should(BeTrue(),
			"Assignments changed after the rule reload; before: %v, now: %s", before, groupsByAlert)
	})
})

package tests

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cluster-health-analyzer/test/integration/framework"
	"github.com/openshift/cluster-health-analyzer/test/integration/simulate"
)

var _ = Describe("Stress Test via Simulate", Label("stress-simulate"), func() {
	const (
		prometheusNS = "openshift-monitoring"
	)

	var (
		alertCount  int
		alertPrefix string
		promClient  *framework.PrometheusClient
		injector    *simulate.Injector
	)

	BeforeEach(func() {
		alertCount = framework.GetEnvInt("STRESS_ALERT_COUNT", 100)
		// Generate unique prefix for each test run to avoid conflicts
		alertPrefix = fmt.Sprintf("StressSim%d", time.Now().UnixNano()/1e6)
		GinkgoWriter.Printf("Stress simulate test: alerts=%d, prefix=%s\n", alertCount, alertPrefix)

		var err error
		// Uses default pods (prometheus-k8s-0, prometheus-k8s-1) with fallback
		injector, err = simulate.NewInjector(prometheusNS)
		Expect(err).NotTo(HaveOccurred())

		promClient, err = framework.NewPrometheusClient(cfg.ThanosURL, cfg.ThanosToken)
		Expect(err).NotTo(HaveOccurred())

		// Wipe all Prometheus data unless KEEP_TEST_DATA=true is set
		// This deletes everything and restarts pods to guarantee a clean slate
		if !framework.GetEnvBool("KEEP_TEST_DATA", false) {
			GinkgoWriter.Printf("Wiping all Prometheus data (set KEEP_TEST_DATA=true to skip)\n")
			if err := injector.WipePrometheusData(); err != nil {
				GinkgoWriter.Printf("Warning: failed to wipe Prometheus data: %v\n", err)
				// Continue anyway - it's best effort
			}
		} else {
			GinkgoWriter.Printf("Keeping existing Prometheus data (KEEP_TEST_DATA=true)\n")
		}
	})

	It("should inject simulated alerts and verify processing", func() {
		By(fmt.Sprintf("Injecting %d simulated alerts into Prometheus", alertCount))
		// Use fixed timing: alerts from minute 3000 to 4000 (relative to reference point)
		// The unique prefix prevents grouping with alerts from other test runs
		scenario := simulate.NewScenarioBuilder().
			AddStressAlerts(alertCount, alertPrefix, "openshift-monitoring", 3000, 4000)

		result, err := injector.Inject(scenario)
		Expect(err).NotTo(HaveOccurred())
		GinkgoWriter.Printf("Injection completed:\n")
		GinkgoWriter.Printf("  - Used pod: %s\n", result.UsedPod)
		GinkgoWriter.Printf("  - Query time: %s\n", result.QueryTime.Format("2006-01-02 15:04:05 MST"))

		By("Waiting for Prometheus to load the blocks")
		time.Sleep(30 * time.Second)

		By("Verifying alerts are visible in Prometheus")
		alertPattern := alertPrefix + ".*"
		Eventually(func() (int, error) {
			alerts, err := promClient.GetAlerts(ctx, alertPattern, result.QueryTime)
			GinkgoWriter.Printf("Alerts found: %d/%d\n", len(alerts), alertCount)
			return len(alerts), err
		}, "5m", "10s").Should(BeNumerically(">=", alertCount*99/100),
			"Expected at least 80%% of alerts to be visible")

		By("Verifying cluster-health-analyzer processed the alerts")
		var incidents []*framework.Incident
		Eventually(func() (int, error) {
			var err error
			// Use time.Now() for incidents - they're generated with current timestamps
			incidents, err = promClient.GetIncidents(ctx, alertPattern, time.Time{})
			GinkgoWriter.Printf("Incidents found: %d/%d\n", len(incidents), alertCount)
			return len(incidents), err
		}, "10m", "15s").Should(BeNumerically(">=", alertCount*99/100),
			"Expected at least 50%% of alerts to have incidents")

		By("Verifying all incidents have valid labels")
		groupIDs := make(map[string]int)
		for i, incident := range incidents {
			GinkgoWriter.Printf("Incident %d: group_id=%s, labels=%v\n",
				i+1, incident.Labels["group_id"], incident.Labels)
			Expect(incident).To(framework.BeValidIncident())
			groupIDs[incident.Labels["group_id"]]++
		}

		// Log group_id summary
		GinkgoWriter.Printf("\nGroup ID summary:\n")
		for groupID, count := range groupIDs {
			GinkgoWriter.Printf("  group_id=%s: %d incidents\n", groupID, count)
		}

		// All alerts in the same namespace with similar timing should be grouped together
		GinkgoWriter.Printf("\nFound %d unique group_ids across %d incidents\n", len(groupIDs), len(incidents))
		Expect(len(groupIDs)).To(BeNumerically("<=", 5),
			"Expected alerts to be grouped into few incidents, got %d groups", len(groupIDs))

		GinkgoWriter.Printf("Stress simulate test completed: %d alerts injected, %d groups\n", alertCount, len(groupIDs))
	})
})

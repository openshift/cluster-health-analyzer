package tests

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cluster-health-analyzer/test/integration/fixtures"
	"github.com/openshift/cluster-health-analyzer/test/integration/framework"
)

var _ = Describe("KubePodCrashLooping Alert Processing", func() {
	const (
		testNamespace = "openshift-monitoring"
	)

	var (
		promClient     *framework.PrometheusClient
		testCluster    *framework.Cluster
		deploymentName string
		ruleName       string
		alertName      string
	)

	BeforeEach(func() {
		// Generate unique names for each test run
		suffix := fmt.Sprintf("%d", time.Now().UnixNano()%100000)
		deploymentName = "crashloop-test-" + suffix
		ruleName = "crashloop-rule-" + suffix
		alertName = "KubePodCrashLoopingTest" + suffix

		GinkgoWriter.Printf("Test resources: deployment=%s, rule=%s, alert=%s\n",
			deploymentName, ruleName, alertName)

		testCluster = cluster.InNamespace(testNamespace)

		By("Cleaning up any leftover test resources")
		_ = testCluster.Delete(ctx, "deployment", deploymentName)
		_ = testCluster.Delete(ctx, "prometheusrule", ruleName)

		Eventually(func() bool {
			gone1, _ := testCluster.IsGone(ctx, "deployment", deploymentName)
			gone2, _ := testCluster.IsGone(ctx, "prometheusrule", ruleName)
			return gone1 && gone2
		}, "30s", "1s").Should(BeTrue(), "Cleanup timed out")

		By("Initializing Prometheus client")
		var err error
		promClient, err = framework.NewPrometheusClient(cfg.ThanosURL, cfg.ThanosToken)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should process a crash looping pod and expose it as an incident in metrics", func() {
		By("Creating PrometheusRule from template")
		ruleYAML, err := fixtures.RenderRule(ruleName, alertName, deploymentName, "crashloop")
		Expect(err).NotTo(HaveOccurred())
		Expect(testCluster.Apply(ctx, ruleYAML)).To(Succeed())

		By("Creating crashing Deployment from template")
		deploymentYAML, err := fixtures.RenderDeployment(deploymentName, testNamespace, "crashloop")
		Expect(err).NotTo(HaveOccurred())
		Expect(testCluster.Apply(ctx, deploymentYAML)).To(Succeed())

		By("Waiting for pod to enter CrashLoopBackOff state")
		Eventually(func() (bool, error) {
			return testCluster.IsPodCrashLooping(ctx, fixtures.PodSelector(deploymentName))
		}, "3m", "10s").Should(BeTrue(), "Pod did not enter CrashLoopBackOff state")

		By(fmt.Sprintf("Waiting for %s alert to fire in Prometheus", alertName))
		Eventually(func() (bool, error) {
			alerts, err := promClient.GetAlerts(ctx, alertName, time.Time{})
			return len(alerts) > 0, err
		}, "5m", "30s").Should(BeTrue(), "Alert %s did not fire within timeout", alertName)

		By("Waiting for cluster-health-analyzer to process the alert")
		var incident framework.Incident
		Eventually(func() (bool, error) {
			incidents, err := promClient.GetIncidents(ctx, alertName, time.Time{})
			if len(incidents) > 0 {
				incident = incidents[0]
			}
			return len(incidents) > 0, err
		}, "3m", "30s").Should(BeTrue(), "Incident for %s was not processed", alertName)

		By("Verifying the incident has correct labels")
		Expect(incident).To(framework.BeValidIncident())
		Expect(map[string]string(incident)).To(HaveKeyWithValue("src_alertname", alertName))
		Expect(map[string]string(incident)).To(HaveKeyWithValue("src_severity", "warning"))

		By(fmt.Sprintf("Test completed - resources %s and %s left for inspection", deploymentName, ruleName))
	})
})

package integration_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cluster-health-analyzer/test/integration/framework"
)

var (
	cfg     *framework.Config
	cluster *framework.Cluster
	ctx     context.Context
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster Health Analyzer Integration Suite")
}

var _ = BeforeSuite(func() {
	ctx = context.Background()

	By("Loading test configuration")
	cfg = framework.LoadConfig()
	GinkgoWriter.Printf("Testing deployment: %s/%s\n", cfg.Namespace, cfg.DeploymentName)
	GinkgoWriter.Printf("Expected image: %s\n", cfg.Image)

	By("Initializing cluster client")
	cluster = framework.NewCluster(cfg.Namespace)

	By("Verifying deployment exists (pre-flight check)")
	available, err := cluster.IsDeploymentAvailable(ctx, cfg.DeploymentName)
	Expect(err).NotTo(HaveOccurred(),
		"Deployment %s/%s not found. Did you run 'make deploy-integration' first?",
		cfg.Namespace, cfg.DeploymentName)
	Expect(available).To(BeTrue(), "Deployment is not available")
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		By("Collecting debug information due to test failure")
		collectDebugInfo()
	}
})

func collectDebugInfo() {
	if cluster == nil {
		return
	}

	selector, err := cluster.GetSelector(ctx, "deployment", cfg.DeploymentName)
	if err != nil {
		GinkgoWriter.Printf("Failed to get selector: %v\n", err)
		return
	}

	status, err := cluster.GetPodStatus(ctx, selector)
	if err != nil {
		GinkgoWriter.Printf("Failed to get pod status: %v\n", err)
		return
	}
	GinkgoWriter.Printf("\n=== Pod Status ===\n%s\n", status)

	logs, err := cluster.GetLogs(ctx, "deployment/"+cfg.DeploymentName, 100)
	if err != nil {
		GinkgoWriter.Printf("Failed to get logs: %v\n", err)
		return
	}
	GinkgoWriter.Printf("\n=== Last 100 lines of logs ===\n%s\n", logs)
}

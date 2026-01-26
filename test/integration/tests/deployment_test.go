package tests

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster Health Analyzer Deployment", func() {
	var selector string

	BeforeEach(func() {
		var err error
		selector, err = cluster.GetSelector(ctx, "deployment", cfg.DeploymentName)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Deployment status", func() {
		It("should be available", func() {
			available, err := cluster.IsDeploymentAvailable(ctx, cfg.DeploymentName)
			Expect(err).NotTo(HaveOccurred())
			Expect(available).To(BeTrue(), "Deployment is not available")
		})

		It("should not have any unavailable replicas", func() {
			hasUnavailable, err := cluster.HasUnavailableReplicas(ctx, cfg.DeploymentName)
			Expect(err).NotTo(HaveOccurred())
			Expect(hasUnavailable).To(BeFalse(), "Deployment has unavailable replicas")
		})
	})

	Describe("Pod status", func() {
		It("should have at least one pod", func() {
			hasPods, err := cluster.HasPods(ctx, selector)
			Expect(err).NotTo(HaveOccurred())
			Expect(hasPods).To(BeTrue(), "No pods found for deployment")
		})

		It("should have all pods in Running state", func() {
			running, err := cluster.ArePodsRunning(ctx, selector)
			Expect(err).NotTo(HaveOccurred())
			Expect(running).To(BeTrue(), "Not all pods are running")
		})

		It("should have all containers ready", func() {
			ready, err := cluster.AreContainersReady(ctx, selector)
			Expect(err).NotTo(HaveOccurred())
			Expect(ready).To(BeTrue(), "Not all containers are ready")
		})
	})
})

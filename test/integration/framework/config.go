// Package framework provides utilities for integration testing.
package framework

import (
	"os"
)

// Config holds the configuration for integration tests.
// Values are loaded from environment variables with sensible defaults.
type Config struct {
	// Image is the container image expected to be deployed
	Image string

	// ManifestsPath is the path to the manifest files
	ManifestsPath string

	// DeploymentName is the name of the deployment to verify
	DeploymentName string

	// Namespace is the Kubernetes namespace where the deployment lives
	Namespace string

	// Kubeconfig is the path to the kubeconfig file (optional, uses default if empty)
	Kubeconfig string

	// ThanosURL is the URL for querying Thanos/Prometheus
	ThanosURL string

	// ThanosToken is the bearer token for Thanos authentication (optional)
	ThanosToken string
}

// LoadConfig creates a Config from environment variables.
func LoadConfig() *Config {
	return &Config{
		Image:          getEnvOrDefault("CHA_IMAGE", "quay.io/openshiftanalytics/cluster-health-analyzer:latest"),
		ManifestsPath:  getEnvOrDefault("MANIFESTS_PATH", "manifests/backend"),
		DeploymentName: getEnvOrDefault("DEPLOYMENT_NAME", "cluster-health-analyzer"),
		Namespace:      getEnvOrDefault("NAMESPACE", "openshift-cluster-health-analyzer"),
		Kubeconfig:     os.Getenv("KUBECONFIG"),
		ThanosURL:      getEnvOrDefault("THANOS_URL", "http://localhost:9090"),
		ThanosToken:    os.Getenv("THANOS_TOKEN"),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}


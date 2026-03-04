package fixtures

import (
	"embed"

	"github.com/openshift/cluster-health-analyzer/test/integration/framework"
)

//go:embed testdata/*.yaml
var testDataFS embed.FS

const (
	DeploymentFile = "testdata/crashing_deployment.yaml"
	RuleFile       = "testdata/crashloop_prometheusrule.yaml"
)

// RenderDeployment loads the embedded deployment template and renders it with the given parameters.
func RenderDeployment(name, namespace, testType string) (string, error) {
	content, err := testDataFS.ReadFile(DeploymentFile)
	if err != nil {
		return "", err
	}

	data := map[string]string{
		"Name":      name,
		"Namespace": namespace,
		"TestType":  testType,
	}

	return framework.Render("deployment", content, data)
}

// RenderRule loads the embedded PrometheusRule template and renders it with the given parameters.
func RenderRule(ruleName, alertName, podPrefix, testType string) (string, error) {
	content, err := testDataFS.ReadFile(RuleFile)
	if err != nil {
		return "", err
	}

	data := map[string]string{
		"RuleName":  ruleName,
		"AlertName": alertName,
		"PodPrefix": podPrefix,
		"TestType":  testType,
	}

	return framework.Render("rule", content, data)
}

// PodSelector returns the label selector for pods created by a deployment.
func PodSelector(deploymentName string) string {
	return "app=" + deploymentName
}

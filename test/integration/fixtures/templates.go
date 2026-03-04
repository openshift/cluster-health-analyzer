package fixtures

import (
	"embed"

	"github.com/openshift/cluster-health-analyzer/test/integration/framework"
)

//go:embed testdata/*.yaml
var testDataFS embed.FS

// Template paths relative to testdata directory.
const (
	DeploymentTemplate     = "testdata/crashing_deployment.yaml"
	PrometheusRuleTemplate = "testdata/crashloop_prometheusrule.yaml"
)

// DeploymentData holds the template parameters for a crashing deployment.
type DeploymentData struct {
	Name      string
	Namespace string
	TestType  string
}

// RuleData holds the template parameters for a PrometheusRule.
type RuleData struct {
	RuleName  string
	AlertName string
	PodPrefix string
	TestType  string
}

// PodSelector returns the label selector for pods created by a deployment.
func PodSelector(deploymentName string) string {
	return "app=" + deploymentName
}

// RenderDeployment loads the embedded deployment template and renders it with the given parameters.
func RenderDeployment(name, namespace, testType string) (string, error) {
	content, err := testDataFS.ReadFile(DeploymentTemplate)
	if err != nil {
		return "", err
	}
	return framework.RenderTemplate(string(content), DeploymentData{
		Name:      name,
		Namespace: namespace,
		TestType:  testType,
	})
}

// RenderRule loads the embedded PrometheusRule template and renders it with the given parameters.
func RenderRule(ruleName, alertName, podPrefix, testType string) (string, error) {
	content, err := testDataFS.ReadFile(PrometheusRuleTemplate)
	if err != nil {
		return "", err
	}
	return framework.RenderTemplate(string(content), RuleData{
		RuleName:  ruleName,
		AlertName: alertName,
		PodPrefix: podPrefix,
		TestType:  testType,
	})
}

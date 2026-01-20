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

// DeploymentReplacements returns the standard replacements for a deployment template.
func DeploymentReplacements(name, namespace, testType string) map[string]string {
	return map[string]string{
		"{{NAME}}":      name,
		"{{NAMESPACE}}": namespace,
		"{{TEST_TYPE}}": testType,
	}
}

// RuleReplacements returns the standard replacements for a PrometheusRule template.
func RuleReplacements(ruleName, alertName, podPrefix, testType string) map[string]string {
	return map[string]string{
		"{{RULE_NAME}}":  ruleName,
		"{{ALERT_NAME}}": alertName,
		"{{POD_PREFIX}}": podPrefix,
		"{{TEST_TYPE}}":  testType,
	}
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
	return framework.RenderTemplate(string(content), DeploymentReplacements(name, namespace, testType)), nil
}

// RenderRule loads the embedded PrometheusRule template and renders it with the given parameters.
func RenderRule(ruleName, alertName, podPrefix, testType string) (string, error) {
	content, err := testDataFS.ReadFile(PrometheusRuleTemplate)
	if err != nil {
		return "", err
	}
	return framework.RenderTemplate(string(content), RuleReplacements(ruleName, alertName, podPrefix, testType)), nil
}

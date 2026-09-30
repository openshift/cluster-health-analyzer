package incidents

import (
	"github.com/containers/kubernetes-mcp-server/pkg/api"
	analyzer "github.com/openshift/cluster-health-analyzer/pkg/mcp"
)

// ToolsetName is the registration name of the incident toolset.
const ToolsetName = "observability/incidents"

// Toolset provides tools for investigating cluster health incidents.
type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

// GetName returns the incident toolset's registration name.
func (t *Toolset) GetName() string {
	return ToolsetName
}

// GetDescription returns the shared get_incidents description.
func (t *Toolset) GetDescription() string {
	return analyzer.GetIncidentsDescription
}

// GetTools returns the get_incidents tool.
func (t *Toolset) GetTools(_ api.FilteringProvider) []api.ServerTool {
	return initGetIncidents()
}

// GetPrompts returns no prompts.
func (t *Toolset) GetPrompts() []api.ServerPrompt {
	return nil
}

// GetResources returns no resources.
func (t *Toolset) GetResources() []api.ServerResource {
	return nil
}

// GetResourceTemplates returns no resource templates.
func (t *Toolset) GetResourceTemplates() []api.ServerResourceTemplate {
	return nil
}

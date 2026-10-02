package toolset

import (
	"github.com/containers/kubernetes-mcp-server/pkg/toolsets"
	"github.com/openshift/cluster-health-analyzer/pkg/toolset/incidents"
)

func init() {
	toolsets.Register(&incidents.Toolset{})
}

package health

import (
	"github.com/inecas/kube-health/pkg/status"
	"github.com/prometheus/common/model"
)

type ComponentsConfig struct {
	Components []Component `yaml:"components"`
}

// Component is a type representing component
// as defined in the "external" YAML config
type Component struct {
	Name            string          `yaml:"name"`
	fullName        string          `yaml:"-"`
	parent          *Component      `yaml:"-"`
	Objects         []K8sObject     `yaml:"objects"`
	ChildComponents []Component     `yaml:"children"`
	AlertsSelectors AlertsSelectors `yaml:"alerts"`
}

func (c *Component) AddParent(p *Component) {
	c.parent = p
}

// K8sObject is a type representing
// Kubernetes object/resource as defined in the
// "external" YAML config
type K8sObject struct {
	Group            string     `yaml:"group"`
	Resource         string     `yaml:"resource"`
	Name             string     `yaml:"name"`
	Namespace        string     `yaml:"namespace"`
	ObjectsSelectors []Selector `yaml:"selectors"`
}

type AlertsSelectors struct {
	Selectors []Selector `yaml:"selectors"`
}

type Selector struct {
	MatchLabels map[string][]string `yaml:"matchLabels"`
}

type HealthStatus int

func (h HealthStatus) IsOK() bool {
	return h == OK
}

func (h HealthStatus) IsError() bool {
	return h == Error
}

func (h HealthStatus) IsWarning() bool {
	return h == Warning
}

func (h HealthStatus) String() string {
	switch h {
	case 0:
		return "OK"
	case 1:
		return "warning"
	case 2:
		return "error"
	default:
		return "unknown"
	}
}

func ParseKubeHealthStatus(s status.Result) HealthStatus {
	switch s {
	case 0:
		return Unknown
	case 1:
		return OK
	case 2:
		return Warning
	case 3:
		return Error
	default:
		return Unknown
	}
}

const (
	OK      HealthStatus = 0
	Warning HealthStatus = 1
	Error   HealthStatus = 2
	Unknown HealthStatus = -1
)

type ComponentHealth struct {
	name            string
	parent          *ComponentHealth
	childComponents []*ComponentHealth
	alerts          []model.LabelSet
	healthStatus    HealthStatus
	objectStatuses  []ObjectStatus
	// to recognize if the alert evaluation happened with errors or not
	alertsErr error
}

func (c *ComponentHealth) AddChild(ch *ComponentHealth) *ComponentHealth {
	ch.parent = c
	c.childComponents = append(c.childComponents, ch)
	return c
}

func (c *ComponentHealth) HasChildren() bool {
	return len(c.childComponents) > 0
}

type ObjectStatus struct {
	Name         string
	Namespace    string
	Resource     string
	HealthStatus HealthStatus
	Progressing  bool
}

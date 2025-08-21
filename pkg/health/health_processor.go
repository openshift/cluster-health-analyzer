package health

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/alertmanager"
	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type healthProcessor struct {
	interval                time.Duration
	alertMatcher            alertMatcher
	componentAlertsMetrics  prom.MetricSet
	componentObjectsMetrics prom.MetricSet
	componentsMetrics       prom.MetricSet
	khChecker               HealthChecker
	config                  *ComponentsConfig
	clusterOperatorNames    []string
}

// NewHealthProcessor initializes all the required objects (alert loader, alert matcher and kube-health checker)
// and creates a new instance of the health processor.
func NewHealthProcessor(interval time.Duration,
	alertsMetrics, objectMetrics, componentsMetrics prom.MetricSet,
	kubeConfigPath string,
	config *ComponentsConfig, alertManagerURL string) (*healthProcessor, error) {
	alertLoader, err := alertmanager.NewAlertLoader(alertManagerURL)
	if err != nil {
		return nil, err
	}

	restConfig, err := common.GetKubeConfig(kubeConfigPath)
	if err != nil {
		return nil, err
	}

	alertMatcher := NewAlertMatcher(alertLoader)
	khChecker, err := NewKubeHealthChecker(restConfig)
	if err != nil {
		return nil, err
	}

	clusterOperatorNames, err := getClusterOperatorNames(restConfig)
	if err != nil {
		return nil, err
	}

	return &healthProcessor{
		interval:                interval,
		alertMatcher:            alertMatcher,
		componentAlertsMetrics:  alertsMetrics,
		componentObjectsMetrics: objectMetrics,
		componentsMetrics:       componentsMetrics,
		khChecker:               khChecker,
		config:                  config,
		clusterOperatorNames:    clusterOperatorNames,
	}, nil
}

// Start starts the processor in a goroutine and returns immediately.
func (p *healthProcessor) Start(ctx context.Context) {
	go p.Run(ctx)
}

// Run periodically runs the processor and blocks until the provided context is done.
func (p *healthProcessor) Run(ctx context.Context) {
	healthStatuses := p.evaluateComponentsHealth(ctx, p.config.Components)
	p.updateAllMetrics(createHealthMetrics(healthStatuses))
	ticker := time.NewTicker(p.interval)
	for {
		select {
		case <-ticker.C:
			slog.Info("Evaluating health of the components")
			healthStatuses = p.evaluateComponentsHealth(ctx, p.config.Components)
			p.updateAllMetrics(createHealthMetrics(healthStatuses))
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}
}

func (p *healthProcessor) evaluateComponentsHealth(ctx context.Context, components []Component) []*ComponentHealth {
	var componentHealths []*ComponentHealth
	for _, c := range components {
		slog.Debug("Evaluating health of component", "component", c.Name)
		cHealth, err := p.evaluateComponent(ctx, &c)
		if err != nil {
			slog.Error("Failed to evaluate health of component", "name", c.Name, "error", err)
			continue
		}
		componentHealths = append(componentHealths, cHealth)
	}
	return componentHealths
}

// evaluateComponent evaluates the health of the provided component
// and recursively the health of all its child components.
func (p *healthProcessor) evaluateComponent(ctx context.Context, c *Component) (*ComponentHealth, error) {
	cHealth := ComponentHealth{name: c.Name}

	if c.parent != nil {
		c.fullName = fmt.Sprintf("%s.%s", c.parent.fullName, c.Name)
	} else {
		c.fullName = c.Name
	}

	for i := range c.ChildComponents {
		ch := &c.ChildComponents[i]
		ch.AddParent(c)
		childHealth, err := p.evaluateComponent(ctx, ch)
		if err != nil {
			return nil, err
		}
		cHealth.AddChild(childHealth)
	}
	if slices.Contains(p.clusterOperatorNames, c.Name) && c.parent.fullName == "control-plane.operators" {
		c.Objects = append(c.Objects, K8sObject{
			Group:    "config.openshift.io",
			Name:     c.Name,
			Resource: "clusteroperators",
		})
	}

	objectStatuses := p.khChecker.EvaluateObjects(ctx, c.Objects)
	alerts, alertsErr := p.alertMatcher.evaluateAlerts(c.AlertsSelectors)
	cHealth.alertsErr = alertsErr
	cHealth.alerts = alerts
	cHealth.objectStatuses = objectStatuses
	cHealth.healthStatus = cHealth.calculateHealthStatus()
	return &cHealth, nil
}

// updateAllMetrics updates all the metrics - for active alerts, for object statuses
// and overall component metrics
func (p *healthProcessor) updateAllMetrics(alertMetrics, objectMetrics, componentMetrics []prom.Metric) {
	p.componentAlertsMetrics.Update(alertMetrics)
	p.componentObjectsMetrics.Update(objectMetrics)
	p.componentsMetrics.Update(componentMetrics)
}

// createHealthMetrics creates all Prometheus metrics for the slice of
// ComponentHealths
func createHealthMetrics(componentHealth []*ComponentHealth) ([]prom.Metric, []prom.Metric, []prom.Metric) {
	var alertMetrics, objectMetrics, componentMetrics []prom.Metric
	for _, c := range componentHealth {
		aMetrics, oMetrics, cMetrics := componentHealthMetrics(c)
		alertMetrics = append(alertMetrics, aMetrics...)
		objectMetrics = append(objectMetrics, oMetrics...)
		componentMetrics = append(componentMetrics, cMetrics...)
	}
	return alertMetrics, objectMetrics, componentMetrics
}

// componentHealthMetrics creates list of alert, object and component metrics
// based on provided component health. If the component health has some active/firing
// alerts then the alert metrics are created. If the component has some childs, then
// component metric is created. The object metrics are created for all the objects
// regardless their status.
func componentHealthMetrics(cHealth *ComponentHealth) ([]prom.Metric, []prom.Metric, []prom.Metric) {
	var alertMetrics, objectMetrics, componentMetrics []prom.Metric
	for _, child := range cHealth.childComponents {
		childAlertMetrics, childObjectMetrics, childComponentMetrics := componentHealthMetrics(child)
		alertMetrics = append(alertMetrics, childAlertMetrics...)
		objectMetrics = append(objectMetrics, childObjectMetrics...)
		componentMetrics = append(componentMetrics, childComponentMetrics...)
	}
	componentName := fullComponentName(cHealth)

	// if component has children then only create metric with component name and status
	if cHealth.HasChildren() {
		m := metricWithNameAndStatus(componentName, cHealth.healthStatus)
		componentMetrics = append(componentMetrics, m)
	} else {
		for _, a := range cHealth.alerts {
			m := metricWithNameAndStatus(componentName, cHealth.healthStatus)
			maps.Copy(m.Labels, a)
			alertMetrics = append(alertMetrics, m)
		}
		for _, o := range cHealth.objectStatuses {
			m := metricWithObjectAttributes(componentName, o)
			objectMetrics = append(objectMetrics, m)
		}
		// if there was an alert error then create alert metric
		// with no labels and with unknown status
		if cHealth.alertsErr != nil {
			m := metricWithNameAndStatus(componentName, cHealth.healthStatus)
			alertMetrics = append(alertMetrics, m)
		}
	}
	return alertMetrics, objectMetrics, componentMetrics
}

// calculateHealthStatus calculates HealthStatus of the component health
func (ch *ComponentHealth) calculateHealthStatus() HealthStatus {
	worstChildStatus := OK
	for _, child := range ch.childComponents {
		childStatus := child.calculateHealthStatus()
		if childStatus > worstChildStatus {
			worstChildStatus = childStatus
		}
	}

	if worstChildStatus.IsError() {
		return Error
	}

	if worstChildStatus.IsWarning() {
		return Warning
	}

	if ch.alertsErr != nil {
		return Unknown
	}

	healthStatus := OK
	// iterate over alerts and check their severity
	for _, alert := range ch.alerts {
		severity := string(alert["src_severity"])
		hv := HealthStatus(processor.ParseHealthValue(severity))
		if hv > healthStatus {
			healthStatus = hv
		}
	}
	// iterate over obejcts and check their health status
	for _, objStatus := range ch.objectStatuses {
		if objStatus.HealthStatus > healthStatus {
			healthStatus = objStatus.HealthStatus
		}
	}
	return healthStatus
}

// metricWithNameAndStatus is a helper function creating a Prometheus metric
// with provided name and status labels and with the value reflecting the
// health status
func metricWithNameAndStatus(name string, status HealthStatus) prom.Metric {
	return prom.Metric{
		Labels: model.LabelSet{
			"component": model.LabelValue(name),
			"status":    model.LabelValue(status.String()),
		},
		Value: float64(status),
	}
}

// metricWithObjectAttributes is a helper function creating a Prometheus
// metrics with provided name and object attributes labels. The object attributes
// are:
// - resource - the K8s resource
// - name - the K8s object name
// - result - health status result
// - namespace - added only when the namespace is not empty (namespaced objects)
// - progressing - bool telling whether the object is in progressing state
func metricWithObjectAttributes(name string, o ObjectStatus) prom.Metric {
	labels := model.LabelSet{
		"component":   model.LabelValue(name),
		"resource":    model.LabelValue(o.Resource),
		"name":        model.LabelValue(o.Name),
		"result":      model.LabelValue(o.HealthStatus.String()),
		"progressing": model.LabelValue(strconv.FormatBool(o.Progressing)),
	}
	if o.Namespace != "" {
		labels["namespace"] = model.LabelValue(o.Namespace)
	}

	return prom.Metric{
		Labels: labels,
		Value:  float64(o.HealthStatus),
	}
}

// fullComponentName recursively (by looking at the parent of the component)
// creates the name of the component and returns it as string
func fullComponentName(c *ComponentHealth) string {
	name := c.name
	if c.parent != nil {
		pName := fullComponentName(c.parent)
		name = fmt.Sprintf("%s.%s", pName, name)
	}
	return name
}

// getClusterOperatorNames reads clusteroperator names from the cluster API
func getClusterOperatorNames(restConfig *rest.Config) ([]string, error) {
	cli, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	coList, err := cli.Resource(schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var coNames []string
	for _, it := range coList.Items {
		coNames = append(coNames, it.GetName())
	}

	return coNames, nil
}

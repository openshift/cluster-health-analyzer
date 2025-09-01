package health

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/alertmanager"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/test/mocks"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/assert/yaml"
)

func TestEvaluateComponentsHealth(t *testing.T) {
	tests := []struct {
		name                    string
		testComponentsFile      string
		expectedNameStatusPairs []nameStatusPair
	}{
		{
			name:               "basic",
			testComponentsFile: "test-data/simple-components.yaml",
			expectedNameStatusPairs: []nameStatusPair{
				{
					name:   "control-plane.nodes",
					status: OK,
				},
				{
					name:   "control-plane.capacity.cpu",
					status: Warning,
				},
				{
					name:   "control-plane.capacity.memory",
					status: OK,
				},
				{
					name:   "control-plane.capacity",
					status: Warning,
				},
				{
					name:   "control-plane",
					status: Warning,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAlertLoader := mocks.NewMockAlertLoader(
				[]models.Alert{
					{
						Labels: models.LabelSet{
							"alertname": "KubeCPUOvercommit",
							"part_of":   "foos",
							"severity":  "warning",
						},
					},
					{
						Labels: models.LabelSet{
							"alertname":  "BarAlert",
							"test_label": "foo",
							"severity":   "critical",
						},
					},
				}, nil, nil)

			testProcessor := createTestHealthProcessor(mockAlertLoader, newMockHealthChecker(OK), nil)
			testConf, err := loadConfig(tt.testComponentsFile)
			assert.NoError(t, err)
			componentsHealths := testProcessor.evaluateComponentsHealth(context.Background(), testConf.Components)
			assert.Equal(t, tt.expectedNameStatusPairs, componentHealthToNameStatusPairs(componentsHealths))
		})
	}
}

func TestEvaluateComponentHealth(t *testing.T) {
	tests := []struct {
		name      string
		component *Component
		// help to mock health result from kube-health
		mockKubeHealthChecker   HealthChecker
		expectedComponentHealth *ComponentHealth
	}{
		{
			name: "healthy component with healthy childs",
			component: &Component{
				Name:            "foo",
				AlertsSelectors: AlertsSelectors{},
				ChildComponents: []Component{
					{
						Name: "bar",
						AlertsSelectors: AlertsSelectors{
							Selectors: []Selector{
								{
									MatchLabels: map[string][]string{
										"no_matching_label": []string{"bars"},
									},
								},
							},
						},
					},
					{
						Name:            "baz",
						AlertsSelectors: AlertsSelectors{},
						Objects: []K8sObject{
							{
								Group:     "test.group",
								Resource:  "bazs",
								Name:      "test-baz",
								Namespace: "baz-namespace",
							},
							{
								Group:    "another.group",
								Resource: "corges",
								Name:     "corge",
							},
						},
					},
				},
			},
			mockKubeHealthChecker: newMockHealthChecker(OK),
			expectedComponentHealth: newComponentHealth("foo", OK).
				AddChild(
					&ComponentHealth{
						name:         "bar",
						healthStatus: OK,
						alerts:       nil,
					}).
				AddChild(
					&ComponentHealth{
						name:         "baz",
						healthStatus: OK,
						objectStatuses: []ObjectStatus{
							{
								Name:         "test-baz",
								Namespace:    "baz-namespace",
								Resource:     "bazs",
								HealthStatus: OK,
								Progressing:  false,
							},
							{
								Name:         "corge",
								Resource:     "corges",
								HealthStatus: OK,
								Progressing:  false,
							},
						},
					},
				),
		},
		{
			name: "component with one error child",
			component: &Component{
				Name:            "foo",
				AlertsSelectors: AlertsSelectors{},
				ChildComponents: []Component{
					{
						Name: "bar",
						AlertsSelectors: AlertsSelectors{
							Selectors: []Selector{
								{
									MatchLabels: map[string][]string{
										"part_of": []string{"bars"},
									},
								},
							},
						},
					},
					{
						Name:            "baz",
						AlertsSelectors: AlertsSelectors{},
					},
				},
			},
			mockKubeHealthChecker: newMockHealthChecker(OK),
			expectedComponentHealth: newComponentHealth("foo", Error).
				AddChild(
					&ComponentHealth{
						name:         "bar",
						healthStatus: Error,
						alerts: []model.LabelSet{
							{
								srcSeverity:  "critical",
								srcAlertname: "BarAlert",
								srcNamespace: "",
								"part_of":    "bars",
							},
						},
					}).
				AddChild(
					&ComponentHealth{
						name:         "baz",
						healthStatus: OK,
					},
				),
		},
		{
			name: "component with one warn child",
			component: &Component{
				Name:            "foo",
				AlertsSelectors: AlertsSelectors{},
				ChildComponents: []Component{
					{
						Name: "bar",
						AlertsSelectors: AlertsSelectors{
							Selectors: []Selector{
								{
									MatchLabels: map[string][]string{
										"part_of": []string{"foos"},
									},
								},
							},
						},
					},
					{
						Name:            "baz",
						AlertsSelectors: AlertsSelectors{},
					},
				},
			},
			mockKubeHealthChecker: newMockHealthChecker(OK),
			expectedComponentHealth: newComponentHealth("foo", Warning).
				AddChild(
					&ComponentHealth{
						name:         "bar",
						healthStatus: Warning,
						alerts: []model.LabelSet{
							{
								srcSeverity:  "warning",
								srcAlertname: "FooAlert",
								srcNamespace: "",
								"part_of":    "foos",
							},
						},
					}).
				AddChild(
					&ComponentHealth{
						name:         "baz",
						healthStatus: OK,
					},
				),
		},
		{
			name: "component with one warning alert and one error object",
			component: &Component{
				Name:            "foo",
				AlertsSelectors: AlertsSelectors{},
				ChildComponents: []Component{
					{
						Name: "bar",
						AlertsSelectors: AlertsSelectors{
							Selectors: []Selector{
								{
									MatchLabels: map[string][]string{
										"part_of": []string{"foos"},
									},
								},
							},
						},
					},
					{
						Name:            "baz",
						AlertsSelectors: AlertsSelectors{},
						Objects: []K8sObject{
							{Group: "testgroup", Resource: "bazes", Name: "bazy", Namespace: "baz-namespace"},
						},
					},
				},
			},
			mockKubeHealthChecker: newMockHealthChecker(Error),
			expectedComponentHealth: newComponentHealth("foo", Error).
				AddChild(
					&ComponentHealth{
						name:         "bar",
						healthStatus: Warning,
						alerts: []model.LabelSet{
							{
								srcSeverity:  "warning",
								srcAlertname: "FooAlert",
								srcNamespace: "",
								"part_of":    "foos",
							},
						},
					}).
				AddChild(
					&ComponentHealth{
						name:         "baz",
						healthStatus: Error,
						objectStatuses: []ObjectStatus{
							{
								Name:         "bazy",
								Namespace:    "baz-namespace",
								Resource:     "bazes",
								HealthStatus: Error,
								Progressing:  false,
							},
						},
					},
				),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAlertLoader := mocks.NewMockAlertLoader(
				[]models.Alert{
					{
						Labels: models.LabelSet{
							"alertname": "FooAlert",
							"part_of":   "foos",
							"severity":  "warning",
						},
					},
					{
						Labels: models.LabelSet{
							"alertname": "BarAlert",
							"part_of":   "bars",
							"severity":  "critical",
						},
					},
				}, nil, nil)
			testProcessor := createTestHealthProcessor(mockAlertLoader, tt.mockKubeHealthChecker, nil)
			componentsHealth, err := testProcessor.evaluateComponent(context.Background(), tt.component)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedComponentHealth, componentsHealth)
		})
	}
}

func TestComponentHealthsToMetrics(t *testing.T) {
	tests := []struct {
		name                     string
		componentsHealth         []*ComponentHealth
		expectedAlertMetrics     []prom.Metric
		expectedObjectMetrics    []prom.Metric
		expectedComponentMetrics []prom.Metric
	}{
		{
			name: "healthy component doesn't create a new metric",
			componentsHealth: []*ComponentHealth{
				{
					name:         "healthy-component",
					healthStatus: OK,
				},
			},
			expectedAlertMetrics: nil,
		},
		{
			name: "one component with 2 alerts firing creates 2 metrics",
			componentsHealth: []*ComponentHealth{
				{
					name:         "bar",
					healthStatus: Warning,
					alerts: []model.LabelSet{
						{
							srcAlertname: "BarAlert",
							"part_of":    "bars",
						},
						{
							srcAlertname: "AnotherBarAlert",
							"part_of":    "bars",
						},
					},
				},
			},
			expectedAlertMetrics: []prom.Metric{
				{
					Labels: model.LabelSet{
						"component":  "bar",
						srcAlertname: "BarAlert",
						"part_of":    "bars",
						"status":     "warning",
					},
					Value: 1,
				},
				{
					Labels: model.LabelSet{
						"component":  "bar",
						srcAlertname: "AnotherBarAlert",
						"part_of":    "bars",
						"status":     "warning",
					},
					Value: 1,
				},
			},
		},
		{
			name: "component with parent and childs",
			componentsHealth: []*ComponentHealth{
				newComponentHealth("testParent", Error).
					AddChild(
						newComponentHealth("foo", Error).
							AddChild(&ComponentHealth{
								name:         "bar",
								healthStatus: Error,
								parent: &ComponentHealth{
									name: "foo",
									parent: &ComponentHealth{
										name: "testParent",
									},
								},
								alerts: []model.LabelSet{
									{
										srcAlertname: "BarAlert",
										"part_of":    "bars",
									},
								},
							}).
							AddChild(
								&ComponentHealth{
									name:         "baz",
									healthStatus: OK,
								},
							).
							AddChild(&ComponentHealth{
								name:         "qux",
								healthStatus: Warning,
								objectStatuses: []ObjectStatus{
									{
										Name:         "test-qux",
										Namespace:    "test-namespace",
										HealthStatus: Warning,
										Resource:     "quxes",
										Progressing:  true,
									},
								},
							})),
			},
			expectedAlertMetrics: []prom.Metric{
				{
					Labels: model.LabelSet{
						"component":  "testParent.foo.bar",
						srcAlertname: "BarAlert",
						"part_of":    "bars",
						"status":     "error",
					},
					Value: 2,
				},
			},
			expectedComponentMetrics: []prom.Metric{
				{
					Labels: model.LabelSet{
						"component": "testParent.foo",
						"status":    "error",
					},
					Value: 2,
				},
				{
					Labels: model.LabelSet{
						"component": "testParent",
						"status":    "error",
					},
					Value: 2,
				},
			},
			expectedObjectMetrics: []prom.Metric{
				{
					Labels: model.LabelSet{
						"component":   "testParent.foo.qux",
						"name":        "test-qux",
						"namespace":   "test-namespace",
						"progressing": "true",
						"resource":    "quxes",
						"result":      "warning",
					},
					Value: 1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alertMetrics, objectMetrics, componentMetrics := createHealthMetrics(tt.componentsHealth)
			assert.ElementsMatch(t, tt.expectedAlertMetrics, alertMetrics)
			assert.ElementsMatch(t, tt.expectedComponentMetrics, componentMetrics)
			assert.ElementsMatch(t, tt.expectedObjectMetrics, objectMetrics)
		})
	}
}

func TestFinalizeComponentTree(t *testing.T) {
	tests := []struct {
		name                        string
		testClusterOperatorNames    []string
		testComponents              []Component
		expectedFinalizedComponents []Component
	}{
		{
			name:                     "nothing is added if there is no control-plane -> operators",
			testClusterOperatorNames: []string{"etcd"},
			testComponents: []Component{
				{Name: "control-plane"},
				{Name: "test"},
				{Name: "foo"},
			},
			expectedFinalizedComponents: []Component{
				{Name: "control-plane"},
				{Name: "test"},
				{Name: "foo"},
			},
		},
		{
			name:                     "existing clusteroperator definition is appended",
			testClusterOperatorNames: []string{"etcd"},
			testComponents: []Component{
				{
					Name: "control-plane",
					ChildComponents: []Component{
						{
							Name: "operators", ChildComponents: []Component{
								{
									Name: "etcd",
									AlertsSelectors: AlertsSelectors{
										Selectors: []Selector{
											{
												MatchLabels: map[string][]string{
													"alert": {"TestEtcdAlert"},
												},
											},
										},
									},
									Objects: []K8sObject{
										{
											Group:    "test.group",
											Resource: "etcds",
											Name:     "cluster",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedFinalizedComponents: []Component{
				{
					Name: "control-plane",
					ChildComponents: []Component{
						{
							Name: "operators", ChildComponents: []Component{
								{
									Name: "etcd",
									AlertsSelectors: AlertsSelectors{
										Selectors: []Selector{
											{
												MatchLabels: map[string][]string{
													"alert": {"TestEtcdAlert"},
												},
											},
										},
									},
									Objects: []K8sObject{
										{
											Group:    "test.group",
											Resource: "etcds",
											Name:     "cluster",
										},
										{
											Group:    configOpenShiftGroup,
											Resource: clusteroperators,
											Name:     "etcd",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:                     "clusteroperator objects are added to the control-plane -> operators",
			testClusterOperatorNames: []string{"etcd", "dns"},
			testComponents: []Component{
				{
					Name: "addons",
					ChildComponents: []Component{
						{Name: "operators"},
						{Name: "foo"},
					},
				},
				{
					Name: "control-plane",
					ChildComponents: []Component{
						{Name: "bar"},
						{Name: "operators"},
					},
				},
			},
			expectedFinalizedComponents: []Component{
				{
					Name: "addons",
					ChildComponents: []Component{
						{Name: "operators"},
						{Name: "foo"},
					},
				},
				{
					Name: "control-plane",
					ChildComponents: []Component{
						{Name: "bar"},
						{Name: "operators", ChildComponents: []Component{
							{
								Name: "etcd",
								Objects: []K8sObject{
									{Group: configOpenShiftGroup, Resource: clusteroperators, Name: "etcd"},
								},
							},
							{
								Name: "dns",
								Objects: []K8sObject{
									{Group: configOpenShiftGroup, Resource: clusteroperators, Name: "dns"},
								},
							},
						}},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testProcessor := createTestHealthProcessor(nil, nil, tt.testClusterOperatorNames)
			components := testProcessor.finalizeComponentTree(tt.testComponents)
			assert.Equal(t, tt.expectedFinalizedComponents, components)
		})
	}
}

func createTestHealthProcessor(al alertmanager.AlertLoader, healthChecker HealthChecker, clusterOperatorNames []string) healthProcessor {
	alertMatcher := NewAlertMatcher(al)
	return healthProcessor{
		khChecker:            healthChecker,
		alertMatcher:         alertMatcher,
		interval:             0 * time.Second,
		clusterOperatorNames: clusterOperatorNames,
	}
}

func componentHealthToNameStatusPairs(componentHealths []*ComponentHealth) []nameStatusPair {
	var res []nameStatusPair
	for _, c := range componentHealths {
		childRes := componentHealthToNameStatusPairs(c.childComponents)
		res = append(res, childRes...)
		res = append(res, nameStatusPair{
			name:   fullComponentName(c),
			status: c.healthStatus,
		})
	}
	return res
}

func newComponentHealth(name string, health HealthStatus) *ComponentHealth {
	return &ComponentHealth{
		name:         name,
		healthStatus: health,
	}
}

type nameStatusPair struct {
	name   string
	status HealthStatus
}

func newMockHealthChecker(status HealthStatus) HealthChecker {
	return &mockKubeHealthChecker{status: status}
}

type mockKubeHealthChecker struct {
	status HealthStatus
}

// EvaluateObjects creates ObjectStatus for each K8s Object.
// All the objects are created with the HealthStatus set in the mockKubeHealthChecker.
func (m *mockKubeHealthChecker) EvaluateObjects(ctx context.Context, objects []K8sObject) []ObjectStatus {
	var objectStatuses []ObjectStatus
	for _, o := range objects {
		objectStatus := ObjectStatus{
			Name:         o.Name,
			Namespace:    o.Namespace,
			Resource:     o.Resource,
			HealthStatus: m.status,
		}
		objectStatuses = append(objectStatuses, objectStatus)
	}

	return objectStatuses
}

// loadConfig reads the file
// and unmarshals the component config.
func loadConfig(filePath string) (*ComponentsConfig, error) {
	conf := &ComponentsConfig{}
	cData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(cData, conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}

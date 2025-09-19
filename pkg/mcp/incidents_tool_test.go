package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/openshift/cluster-health-analyzer/pkg/alertmanager"
	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/test/mocks"
	"github.com/prometheus/alertmanager/api/v2/models"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

var (
	testMcpTool = mcp.Tool{
		Name: "get_incidents",
		Description: `List the current firing incidents in the cluster. 
		One incident is a group of related alerts that are likely triggered by the same root cause.
		Use this tool to analyze the cluster health status and determine why a component is failing or degraded.`,
		Annotations: mcp.ToolAnnotation{
			Title: "Provides information about Incidents in the cluster",
		},
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"max_age_hours": map[string]any{
					"type":        "number",
					"description": "Maximum age of incidents to include in hours (max 360 for 15 days). Default: 360",
					"minimum":     1,
					"maximum":     360,
				},
			},
		},
	}
)

func TestIncidentTool_IncidentsHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		silencedOn = time.Now()
	)

	type args struct {
		ctx     context.Context
		request mcp.CallToolRequest
	}

	tests := []struct {
		name           string
		promLoader     prom.Loader
		amLoader       alertmanager.Loader
		args           args
		expectedResult *mcp.CallToolResult
		expectedErr    error
	}{
		{
			name: "happy path",
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)

				mocked.EXPECT().LoadVectorRange(gomock.Any(), processor.ClusterHealthComponentsMap, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"group_id":      "123",
							"src_alertname": "ClusterOperatorDown",
							"src_namespace": "openshift-monitoring",
							"src_severity":  "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"group_id":      "123",
							"src_alertname": "UpdateAvailable",
							"src_namespace": "openshift-monitoring",
							"src_severity":  "info",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
				}, nil)

				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "ClusterOperatorDown",
							"namespace":  "openshift-monitoring",
							"severity":   "warning",
							"pod":        "bar",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "ClusterOperatorDown",
							"namespace":  "openshift-monitoring",
							"severity":   "warning",
							"pod":        "foo",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "UpdateAvailable",
							"namespace":  "openshift-monitoring",
							"severity":   "info",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
						},
					},
				}, nil)
				return mocked
			}(),
			amLoader: func() alertmanager.Loader {
				silencedAlerts := []models.Alert{
					{
						Labels: map[string]string{
							"alertname": "ClusterOperatorDown",
							"namespace": "openshift-monitoring",
							"severity":  "warning",
							"pod":       "foo",
						},
					},
					{
						Labels: map[string]string{
							"alertname": "UpdateAvailable",
							"namespace": "openshift-monitoring",
							"severity":  "info",
						},
					},
				}
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(silencedAlerts, nil)
				mocked.EXPECT().GetSilencesByLabels([]string{"alertname=ClusterOperatorDown", "namespace=openshift-monitoring", "severity=warning"}).Return([]models.Silence{
					{
						StartsAt: func(t time.Time) *strfmt.DateTime {
							dt := strfmt.DateTime(t)
							return &dt
						}(silencedOn),
					},
				}, nil).AnyTimes()
				mocked.EXPECT().GetSilencesByLabels([]string{"alertname=UpdateAvailable", "namespace=openshift-monitoring", "severity=info"}).Return([]models.Silence{
					{
						StartsAt: func(t time.Time) *strfmt.DateTime {
							dt := strfmt.DateTime(t)
							return &dt
						}(silencedOn),
					},
				}, nil).AnyTimes()
				return mocked
			}(),
			args: args{
				ctx: context.WithValue(context.Background(), authHeaderStr, "test"),
				request: mcp.CallToolRequest{
					Params: mcp.CallToolParams{
						Arguments: map[string]any{
							"max_age_hours": "300",
						},
					},
				},
			},
			expectedResult: func() *mcp.CallToolResult {
				baseTime := model.Now().Add(-20 * time.Minute).Time()
				r := Response{
					Incidents: Incidents{
						Total: 1,
						Incidents: []Incident{
							{
								GroupId:            "123",
								Severity:           "warning",
								Status:             "firing",
								StartTime:          baseTime.Add(19 * time.Minute).Format(time.RFC3339),
								AffectedComponents: []string{""},
								Alerts: []model.LabelSet{
									{
										"name":       "ClusterOperatorDown",
										"namespace":  "openshift-monitoring",
										"severity":   "warning",
										"status":     "resolved",
										"start_time": model.LabelValue(baseTime.Format(time.RFC3339)),
										"end_time":   model.LabelValue(baseTime.Format(time.RFC3339)),
									},
									{
										"name":        "UpdateAvailable",
										"namespace":   "openshift-monitoring",
										"severity":    "info",
										"status":      "resolved",
										"start_time":  model.LabelValue(baseTime.Format(time.RFC3339)),
										"end_time":    model.LabelValue(baseTime.Format(time.RFC3339)),
										"silenced":    "true",
										"silenced_on": model.LabelValue(silencedOn.Format(time.RFC3339)),
									},
								},
							},
						},
					},
				}
				data, _ := json.Marshal(r)
				response := fmt.Sprintf(getIncidentsResponseTemplate, string(data))
				return mcp.NewToolResultText(response)
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := IncidentTool{
				Tool: testMcpTool,
				getPrometheusLoaderFn: func(url, _ string) (prom.Loader, error) {
					return tt.promLoader, nil
				},
				getAlertManagerLoaderFn: func(url, token string) (alertmanager.Loader, error) {
					return tt.amLoader, nil
				},
			}
			got, err := tool.IncidentsHandler(tt.args.ctx, tt.args.request)

			// making assertion on sorted string content because
			// map order is not guarantee in golang and this can cause flakyness
			// on response assertions
			gotContent := (*got).Content[0].(mcp.TextContent).Text
			bytes := []byte(gotContent)
			slices.Sort(bytes)

			expectedContent := (*tt.expectedResult).Content[0].(mcp.TextContent).Text
			bytes = []byte(expectedContent)
			slices.Sort(bytes)

			assert.Equal(t, expectedContent, gotContent)
			assert.Equal(t, tt.expectedErr, err)
		})
	}

}

func TestTransformPromValueToIncident(t *testing.T) {
	tests := []struct {
		name              string
		testInput         prom.RangeVector
		expectedIncidents map[string]Incident
	}{
		{
			name: "Two alerts with same group_id are one incident",
			testInput: prom.RangeVector{
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Samples: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Samples: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
			},
			expectedIncidents: map[string]Incident{
				"1": {
					GroupId:            "1",
					Severity:           processor.Healthy.String(),
					Status:             "firing",
					StartTime:          time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
					AffectedComponents: []string{"console", "monitoring"},
					ComponentsSet:      map[string]struct{}{"monitoring": {}, "console": {}},
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "openshift-monitoring", "severity": "warning"},
						{"alertname": "Alert2", "namespace": "openshift-console", "severity": "warning"},
					},
					AlertsSet: map[string]struct{}{
						"{alertname=\"Alert2\", namespace=\"openshift-console\", severity=\"warning\"}":    {},
						"{alertname=\"Alert1\", namespace=\"openshift-monitoring\", severity=\"warning\"}": {},
					},
				},
			},
		},
		{
			name: "Two alerts with same group_id and same component are one incident",
			testInput: prom.RangeVector{
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Samples: []model.SamplePair{
						{
							Value:     1,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Samples: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
			},
			expectedIncidents: map[string]Incident{
				"1": {
					GroupId:            "1",
					Severity:           processor.Warning.String(),
					Status:             "firing",
					StartTime:          time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
					AffectedComponents: []string{"monitoring"},
					ComponentsSet:      map[string]struct{}{"monitoring": {}},
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "openshift-monitoring", "severity": "warning"},
						{"alertname": "Alert2", "namespace": "openshift-monitoring", "severity": "warning"},
					},
					AlertsSet: map[string]struct{}{
						"{alertname=\"Alert1\", namespace=\"openshift-monitoring\", severity=\"warning\"}": {},
						"{alertname=\"Alert2\", namespace=\"openshift-monitoring\", severity=\"warning\"}": {},
					},
				},
			},
		},
		{
			name: "Two different incidents and alert with severity=None is ignored",
			testInput: prom.RangeVector{
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Samples: []model.SamplePair{
						{
							Value:     1,
							Timestamp: model.Now().Add(-25 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert3",
						"group_id":      "2",
						"src_severity":  "none",
						"component":     "none",
					},
					Samples: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "critical",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Samples: []model.SamplePair{
						{
							Value:     2,
							Timestamp: model.Now().Add(-25 * time.Minute),
						},
						{
							Value:     2,
							Timestamp: model.Now().Add(-11 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert4",
						"group_id":      "2",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Samples: []model.SamplePair{
						{
							Value:     1,
							Timestamp: model.Now().Add(-15 * time.Minute),
						},
					},
				},
			},
			expectedIncidents: map[string]Incident{
				"1": {
					GroupId:            "1",
					Severity:           "critical",
					Status:             "resolved",
					StartTime:          time.Now().Add(-25 * time.Minute).Format(time.RFC3339),
					EndTime:            time.Now().Add(-11 * time.Minute).Format(time.RFC3339),
					AffectedComponents: []string{"console", "monitoring"},
					ComponentsSet:      map[string]struct{}{"monitoring": {}, "console": {}},
					Alerts: []model.LabelSet{
						{"alertname": "Alert2", "namespace": "openshift-console", "severity": "warning"},
						{"alertname": "Alert1", "namespace": "openshift-monitoring", "severity": "critical"},
					},
					AlertsSet: map[string]struct{}{
						"{alertname=\"Alert2\", namespace=\"openshift-console\", severity=\"warning\"}":     {},
						"{alertname=\"Alert1\", namespace=\"openshift-monitoring\", severity=\"critical\"}": {},
					},
				},
				"2": {
					GroupId:            "2",
					Severity:           "warning",
					Status:             "resolved",
					StartTime:          time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
					EndTime:            time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
					AffectedComponents: []string{"console"},
					ComponentsSet:      map[string]struct{}{"console": {}},
					Alerts: []model.LabelSet{
						{"alertname": "Alert4", "namespace": "openshift-console", "severity": "warning"},
					},
					AlertsSet: map[string]struct{}{
						"{alertname=\"Alert4\", namespace=\"openshift-console\", severity=\"warning\"}": {},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTool := IncidentTool{}
			incidents, err := testTool.transformPromValueToIncident(tt.testInput, v1.Range{
				Start: time.Now().Add(-30 * time.Minute),
				End:   time.Now(),
				Step:  300 * time.Second,
			})
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedIncidents, incidents)
		})
	}
}

func TestGetAlertDataForIncidents(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		fooSilencedOn = time.Now()
		barSilencedOn = fooSilencedOn.Add(3 * time.Hour)
	)

	tests := []struct {
		name              string
		promLoader        prom.Loader
		amLoader          alertmanager.Loader
		incidentsMap      map[string]Incident
		expectedIncidents []Incident
		wantErr           error
	}{
		{
			name: "Same alerts in different namespace are matched correctly",
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"alertstate": "firing",
							"severity":   "critical",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-25 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"alertstate": "firing",
							"severity":   "critical",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-24 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
				}, nil)
				return mocked
			}(),
			amLoader: func() alertmanager.Loader {
				silencedAlerts := []models.Alert{}
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(silencedAlerts, nil)
				return mocked
			}(),
			incidentsMap: map[string]Incident{
				"1": {
					GroupId: "1",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo"},
						{"alertname": "Alert1", "namespace": "bar"},
					},
				},
			},
			expectedIncidents: []Incident{
				{
					GroupId: "1",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"severity":   "critical",
							"status":     "firing",
							"start_time": model.LabelValue(model.Now().Add(-25 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
							"severity":   "critical",
							"status":     "firing",
							"start_time": model.LabelValue(model.Now().Add(-24 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
			},
		},
		{
			name: "Same alert in more incidents",
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"alertstate": "resolved",
							"severity":   "critical",
						},
						Samples: []model.SamplePair{
							{
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"alertstate": "resolved",
							"severity":   "critical",
						},
						Samples: []model.SamplePair{
							{
								Timestamp: model.Now().Add(-19 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert2",
							"namespace":  "bar",
							"alertstate": "resolved",
							"severity":   "critical",
						},
						Samples: []model.SamplePair{
							{
								Timestamp: model.Now().Add(-19 * time.Minute),
							},
						},
					},
				}, nil)
				return mocked
			}(),
			amLoader: func() alertmanager.Loader {
				silencedAlerts := []models.Alert{}
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(silencedAlerts, nil)
				return mocked
			}(),
			incidentsMap: map[string]Incident{
				"1": {
					GroupId: "1",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo", "severity": "critical"},
						{"alertname": "Alert1", "namespace": "bar", "severity": "critical"},
					},
				},
				"2": {
					GroupId: "2",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo", "severity": "critical"},
						{"alertname": "Alert2", "namespace": "bar", "severity": "critical"},
					},
				},
			},
			expectedIncidents: []Incident{
				{
					GroupId: "1",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"severity":   "critical",
							"status":     "resolved",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
							"severity":   "critical",
							"status":     "resolved",
							"start_time": model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
				{
					GroupId: "2",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"severity":   "critical",
							"status":     "resolved",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert2",
							"namespace":  "bar",
							"severity":   "critical",
							"status":     "resolved",
							"start_time": model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
			},
		},
		{
			name: "Alerts are correctly marked as silenced",
			// three alerts with the same name
			// A. Alert1, namespace=foo, pod=red
			// B. Alert1, namespace=foo, pod=blue (same alertname and namespace with A. but differend pod name)
			// C. Alert1, namespace=bar, pod=red (same alertname and pod name with A. but different namespace)
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"pod":        "red",
							"alertstate": "firing",
							"severity":   "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"pod":        "blue",
							"alertstate": "firing",
							"severity":   "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"pod":        "red",
							"alertstate": "firing",
							"severity":   "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"pod":        "green",
							"alertstate": "firing",
							"severity":   "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
				}, nil)
				return mocked
			}(),
			amLoader: func() alertmanager.Loader {
				silencedAlerts := []models.Alert{
					{
						Labels: map[string]string{
							"alertname": "Alert1",
							"namespace": "foo",
							"severity":  "warning",
							"pod":       "red",
						},
					},
					{
						Labels: map[string]string{
							"alertname": "Alert1",
							"namespace": "bar",
							"severity":  "warning",
							"pod":       "red",
						},
					},
					{
						Labels: map[string]string{
							"alertname": "Alert1",
							"namespace": "bar",
							"severity":  "warning",
							"pod":       "green",
						},
					},
				}
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(silencedAlerts, nil)
				mocked.EXPECT().GetSilencesByLabels([]string{"alertname=Alert1", "namespace=foo", "severity=warning"}).Return([]models.Silence{
					{
						StartsAt: func(t time.Time) *strfmt.DateTime {
							dt := strfmt.DateTime(t)
							return &dt
						}(fooSilencedOn),
					},
				}, nil).AnyTimes()
				mocked.EXPECT().GetSilencesByLabels([]string{"alertname=Alert1", "namespace=bar", "severity=warning"}).Return([]models.Silence{
					{
						StartsAt: func(t time.Time) *strfmt.DateTime {
							dt := strfmt.DateTime(t)
							return &dt
						}(barSilencedOn),
					},
					// adding other silences because we want to check the lowest one is matching the expectation
					{
						StartsAt: func(t time.Time) *strfmt.DateTime {
							dt := strfmt.DateTime(t)
							return &dt
						}(barSilencedOn.Add(5 * time.Minute)),
					},
					{
						StartsAt: func(t time.Time) *strfmt.DateTime {
							dt := strfmt.DateTime(t)
							return &dt
						}(barSilencedOn.Add(time.Hour)),
					},
				}, nil).AnyTimes()
				return mocked
			}(),
			incidentsMap: map[string]Incident{
				"1": {
					GroupId: "1",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo", "severity": "warning"},
						{"alertname": "Alert1", "namespace": "bar", "severity": "warning"},
					},
				},
			},
			expectedIncidents: []Incident{
				{
					GroupId: "1",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"status":     "firing",
							"severity":   "warning",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":        "Alert1",
							"namespace":   "bar",
							"status":      "firing",
							"severity":    "warning",
							"start_time":  model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
							"silenced":    "true",
							"silenced_on": model.LabelValue(barSilencedOn.Format(time.RFC3339)),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			tool := IncidentTool{}

			incidents, err := tool.getAlertDataForIncidents(ctx, tt.incidentsMap, tt.promLoader, tt.amLoader, v1.Range{
				Start: time.Now().Add(-30 * time.Minute),
				End:   time.Now(),
				Step:  300 * time.Second,
			})

			assert.Equal(t, tt.wantErr, err)

			// Sort the actual and expected alerts slices before comparing to avoid test flakyness
			for i := range incidents {
				sortAlerts(incidents[i].Alerts)
			}

			for i := range tt.expectedIncidents {
				sortAlerts(tt.expectedIncidents[i].Alerts)
			}

			assert.ElementsMatch(t, tt.expectedIncidents, incidents)
		})
	}
}

func sortAlerts(alerts []model.LabelSet) {
	sort.Slice(alerts, func(i, j int) bool {
		a := alerts[i]
		b := alerts[j]

		// First, sort by 'name'
		if a["name"] != b["name"] {
			return a["name"] < b["name"]
		}

		// Then, sort by 'namespace' if names are the same
		if a["namespace"] != b["namespace"] {
			return a["namespace"] < b["namespace"]
		}

		// Finally, sort by 'pod' or another unique label to guarantee stability
		return a["pod"] < b["pod"]
	})
}

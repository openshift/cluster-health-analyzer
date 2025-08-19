package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func TestTransformPromValueToIncident(t *testing.T) {
	tests := []struct {
		name              string
		testInput         model.Value
		expectedIncidents map[string]Incident
	}{
		{
			name: "Two alerts with same group_id are one incident",
			testInput: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Values: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Values: []model.SamplePair{
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
			testInput: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Values: []model.SamplePair{
						{
							Value:     1,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Values: []model.SamplePair{
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
			testInput: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Values: []model.SamplePair{
						{
							Value:     1,
							Timestamp: model.Now().Add(-25 * time.Minute),
						},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{
						"src_alertname": "Alert3",
						"group_id":      "2",
						"src_severity":  "none",
						"component":     "none",
					},
					Values: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "critical",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Values: []model.SamplePair{
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
				&model.SampleStream{
					Metric: model.Metric{
						"src_alertname": "Alert4",
						"group_id":      "2",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Values: []model.SamplePair{
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
			incidents, err := transformPromValueToIncident(tt.testInput, v1.Range{
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
	tests := []struct {
		name              string
		activeAlerts      model.Matrix
		incidentsMap      map[string]Incident
		expectedIncidents []Incident
	}{
		{
			name: "Same alerts in different namespace are matched correctly",
			activeAlerts: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"alertname":  "Alert1",
						"namespace":  "foo",
						"alertstate": "firing",
					},
					Values: []model.SamplePair{
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
				&model.SampleStream{
					Metric: model.Metric{
						"alertname":  "Alert1",
						"namespace":  "bar",
						"alertstate": "firing",
					},
					Values: []model.SamplePair{
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
			},
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
							"status":     "firing",
							"start_time": model.LabelValue(model.Now().Add(-25 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
							"status":     "firing",
							"start_time": model.LabelValue(model.Now().Add(-24 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
			},
		},
		{
			name: "Same alert in more incidents",
			activeAlerts: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"alertname":  "Alert1",
						"namespace":  "foo",
						"alertstate": "resolved",
					},
					Values: []model.SamplePair{
						{
							Timestamp: model.Now().Add(-20 * time.Minute),
						},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{
						"alertname":  "Alert1",
						"namespace":  "bar",
						"alertstate": "resolved",
					},
					Values: []model.SamplePair{
						{
							Timestamp: model.Now().Add(-19 * time.Minute),
						},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{
						"alertname":  "Alert2",
						"namespace":  "bar",
						"alertstate": "resolved",
					},
					Values: []model.SamplePair{
						{
							Timestamp: model.Now().Add(-19 * time.Minute),
						},
					},
				},
			},
			incidentsMap: map[string]Incident{
				"1": {
					GroupId: "1",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo"},
						{"alertname": "Alert1", "namespace": "bar"},
					},
				},
				"2": {
					GroupId: "2",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo"},
						{"alertname": "Alert2", "namespace": "bar"},
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
							"status":     "resolved",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
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
							"status":     "resolved",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert2",
							"namespace":  "bar",
							"status":     "resolved",
							"start_time": model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockPromApi := MockPromAPI{modelValue: tt.activeAlerts}
			incidents := getAlertDataForIncidents(ctx, tt.incidentsMap, &mockPromApi, v1.Range{
				Start: time.Now().Add(-30 * time.Minute),
				End:   time.Now(),
				Step:  300 * time.Second,
			})
			assert.ElementsMatch(t, tt.expectedIncidents, incidents)
		})
	}
}

type MockPromAPI struct {
	modelValue model.Value
}

func (m *MockPromAPI) Query(ctx context.Context, query string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
	return m.modelValue, nil, nil
}

func (m *MockPromAPI) Alerts(ctx context.Context) (v1.AlertsResult, error) {
	// noop
	return v1.AlertsResult{}, nil
}

func (m *MockPromAPI) AlertManagers(ctx context.Context) (v1.AlertManagersResult, error) {
	// noop
	return v1.AlertManagersResult{}, nil
}

func (m *MockPromAPI) Buildinfo(ctx context.Context) (v1.BuildinfoResult, error) {
	//noop
	return v1.BuildinfoResult{}, nil
}

func (m *MockPromAPI) Config(ctx context.Context) (v1.ConfigResult, error) {
	//noop
	return v1.ConfigResult{}, nil
}

func (m *MockPromAPI) Flags(ctx context.Context) (v1.FlagsResult, error) {
	//noop
	return nil, nil
}
func (m *MockPromAPI) CleanTombstones(ctx context.Context) error {
	// noop
	return nil
}

func (m *MockPromAPI) DeleteSeries(ctx context.Context, matches []string, startTime, endTime time.Time) error {
	// noop
	return nil
}

func (m *MockPromAPI) LabelNames(ctx context.Context, matches []string, startTime, endTime time.Time) ([]string, v1.Warnings, error) {
	return nil, nil, nil
}

func (m *MockPromAPI) LabelValues(ctx context.Context, label string, matches []string, startTime, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, nil
}

func (m *MockPromAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, nil
}

func (m *MockPromAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, nil
}

func (m *MockPromAPI) QueryRange(ctx context.Context, query string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
	return m.modelValue, nil, nil
}

func (m *MockPromAPI) Rules(ctx context.Context) (v1.RulesResult, error) {
	return v1.RulesResult{}, nil
}

func (m *MockPromAPI) Runtimeinfo(ctx context.Context) (v1.RuntimeinfoResult, error) {
	return v1.RuntimeinfoResult{}, nil
}

func (m *MockPromAPI) Series(ctx context.Context, matches []string, startTime, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, nil
}

func (m *MockPromAPI) Snapshot(ctx context.Context, skipHead bool) (v1.SnapshotResult, error) {
	return v1.SnapshotResult{}, nil
}

func (m *MockPromAPI) TSDB(ctx context.Context) (v1.TSDBResult, error) {
	return v1.TSDBResult{}, nil
}
func (m *MockPromAPI) Targets(ctx context.Context) (v1.TargetsResult, error) {
	return v1.TargetsResult{}, nil
}

func (m *MockPromAPI) TargetsMetadata(ctx context.Context, matchTarget, metric, limit string) ([]v1.MetricMetadata, error) {
	return nil, nil
}

func (m *MockPromAPI) WalReplay(ctx context.Context) (v1.WalReplayStatus, error) {
	return v1.WalReplayStatus{}, nil
}

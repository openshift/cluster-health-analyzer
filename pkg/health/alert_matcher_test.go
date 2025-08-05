package health

import (
	"testing"

	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func TestEvaluateAlerts(t *testing.T) {
	tests := []struct {
		name                 string
		alerts               AlertsSelectors
		expectedActiveAlerts []model.LabelSet
	}{
		{
			name: "Multiple label values (OR) and one matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"FooAlert", "BazAlert"},
						},
					},
				},
			},
			expectedActiveAlerts: []model.LabelSet{
				{
					srcAlertname: "FooAlert",
					srcSeverity:  "warning",
					srcNamespace: "foo-ns",
				},
				{
					srcAlertname: "FooAlert",
					srcSeverity:  "warning",
					srcNamespace: "second-foo-ns",
				},
			},
		},
		{
			name: "Multiple label values (OR) and none matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"BazAlert", "QuxAlert"},
						},
					},
				},
			},
			expectedActiveAlerts: nil,
		},
		{
			name: "Multiple label values (OR) and all matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"FooAlert", "BarAlert"},
						},
					},
				},
			},
			expectedActiveAlerts: []model.LabelSet{
				{
					srcAlertname: "FooAlert",
					srcSeverity:  "warning",
					srcNamespace: "foo-ns",
				},
				{
					srcAlertname: "FooAlert",
					srcSeverity:  "warning",
					srcNamespace: "second-foo-ns",
				},
				{
					srcAlertname: "BarAlert",
					srcSeverity:  "critical",
					srcNamespace: "bar-ns",
				},
			},
		},
		{
			name: "Multiple labels (AND) but only one matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"part_of":     {"testing"},
							"alertname":   {"FooAlert"},
							"nonexisting": {"value"},
						},
					},
				},
			},
			expectedActiveAlerts: nil,
		},
		{
			name: "Multiple labels (AND) and all matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"FooAlert"},
							"part_of":   {"foos"},
						},
					},
				},
			},
			expectedActiveAlerts: []model.LabelSet{
				{
					srcAlertname: "FooAlert",
					"part_of":    "foos",
					srcSeverity:  "warning",
					srcNamespace: "foo-ns",
				},
				{
					srcAlertname: "FooAlert",
					"part_of":    "foos",
					srcSeverity:  "warning",
					srcNamespace: "second-foo-ns",
				},
			},
		},
		{
			name: "Multiple labels (AND), multiple values but only one matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"Alert", "Blah"},
							"part_of":   {"foos"},
						},
					},
				},
			},
			expectedActiveAlerts: nil,
		},
		{
			name: "Multiple labels, multiple values and all matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"Alert", "FooAlert"},
							"part_of":   {"foos"},
						},
					},
				},
			},
			expectedActiveAlerts: []model.LabelSet{
				{
					srcAlertname: "FooAlert",
					"part_of":    "foos",
					srcSeverity:  "warning",
					srcNamespace: "foo-ns",
				},
				{
					srcAlertname: "FooAlert",
					srcSeverity:  "warning",
					srcNamespace: "second-foo-ns",
					"part_of":    "foos",
				},
			},
		},
		{
			name: "Multiple labels (AND) and all matches one alert",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"namespace": {"foo-ns"},
							"part_of":   {"bars", "shits", "foos"},
							"alertname": {"FooAlert"},
						},
					},
				},
			},
			expectedActiveAlerts: []model.LabelSet{
				{
					srcAlertname: "FooAlert",
					"part_of":    "foos",
					srcSeverity:  "warning",
					srcNamespace: "foo-ns",
				},
			},
		},
		{
			name: "Multiple matchlabels attributes and none matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"FooAlert"},
							"part_of":   {"testing"},
						},
					},
					{
						MatchLabels: map[string][]string{
							"alertname": {"BarAlert"},
							"part_of":   {"testing"},
						},
					},
				},
			},
			expectedActiveAlerts: nil,
		},
		{
			name: "Multiple matchlabels attributes and one matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"FooAlert"},
							"part_of":   {"foos"},
						},
					},
					{
						MatchLabels: map[string][]string{
							"alertname": {"BarAlert"},
							"part_of":   {"testing"},
						},
					},
				},
			},
			expectedActiveAlerts: []model.LabelSet{
				{
					srcAlertname: "FooAlert",
					"part_of":    "foos",
					srcSeverity:  "warning",
					srcNamespace: "foo-ns",
				},
				{
					srcAlertname: "FooAlert",
					srcSeverity:  "warning",
					srcNamespace: "second-foo-ns",
					"part_of":    "foos",
				},
			},
		},
		{
			name: "Multiple matchlabels attributes and all matches",
			alerts: AlertsSelectors{
				Selectors: []Selector{
					{
						MatchLabels: map[string][]string{
							"alertname": {"FooAlert"},
							"part_of":   {"foos"},
						},
					},
					{
						MatchLabels: map[string][]string{
							"alertname": {"BarAlert"},
							"part_of":   {"bars"},
						},
					},
				},
			},
			expectedActiveAlerts: []model.LabelSet{
				{
					srcAlertname: "FooAlert",
					"part_of":    "foos",
					srcSeverity:  "warning",
					srcNamespace: "foo-ns",
				},
				{
					srcAlertname: "FooAlert",
					srcSeverity:  "warning",
					srcNamespace: "second-foo-ns",
					"part_of":    "foos",
				},
				{
					srcAlertname: "BarAlert",
					"part_of":    "bars",
					srcSeverity:  "critical",
					srcNamespace: "bar-ns",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAlertLoader := MockAlertLoader{
				alerts: []models.Alert{
					{
						Labels: models.LabelSet{
							"alertname": "FooAlert",
							"part_of":   "foos",
							"namespace": "foo-ns",
							"severity":  "warning",
						},
					},
					{
						Labels: models.LabelSet{
							"alertname": "BarAlert",
							"part_of":   "bars",
							"namespace": "bar-ns",
							"severity":  "critical",
						},
					},
					{
						Labels: models.LabelSet{
							"alertname": "FooAlert",
							"part_of":   "foos",
							"namespace": "second-foo-ns",
							"severity":  "warning",
						},
					},
				},
			}
			testAlertMatcher := NewAlertMatcher(mockAlertLoader)
			alerts, err := testAlertMatcher.evaluateAlerts(tt.alerts)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedActiveAlerts, alerts)
		})
	}

}

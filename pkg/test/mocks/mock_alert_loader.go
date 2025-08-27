package mocks

import (
	"strings"

	"github.com/prometheus/alertmanager/api/v2/models"
)

type MockAlertLoader struct {
	alerts   []models.Alert
	silenced []models.Alert
	err      error
}

func NewMockAlertLoader(alerts, silenced []models.Alert, err error) MockAlertLoader {
	return MockAlertLoader{alerts: alerts, silenced: silenced, err: err}
}

func (m MockAlertLoader) ActiveAlerts() ([]models.Alert, error) {
	return m.alerts, m.err
}

func (m MockAlertLoader) SilencedAlerts() ([]models.Alert, error) {
	return m.silenced, m.err
}

// ActiveAlertsWithLabels returns only the alerts matching all the provided labels
func (m MockAlertLoader) ActiveAlertsWithLabels(labels []string) ([]models.Alert, error) {
	var res []models.Alert
	labelsToMatch := labelSliceToMap(labels)
	for _, a := range m.alerts {
		allMatch := true
		for k, v := range labelsToMatch {
			val, ok := a.Labels[k]
			if !ok || val != v {
				allMatch = false
			}
		}
		if allMatch {
			res = append(res, a)
		}
	}
	return res, m.err
}

func labelSliceToMap(labels []string) map[string]string {
	m := make(map[string]string, len(labels))
	for _, l := range labels {
		pairAsSlice := strings.Split(l, "=")
		m[pairAsSlice[0]] = pairAsSlice[1]
	}
	return m
}

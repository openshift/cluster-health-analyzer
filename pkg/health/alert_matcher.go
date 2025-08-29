package health

import (
	"fmt"

	"github.com/openshift/cluster-health-analyzer/pkg/alertmanager"
	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/common/model"
)

var (
	srcAlertname = model.LabelName(fmt.Sprintf("%salertname", processor.SrcLabelPrefix))
	srcNamespace = model.LabelName(fmt.Sprintf("%snamespace", processor.SrcLabelPrefix))
	srcSeverity  = model.LabelName(fmt.Sprintf("%sseverity", processor.SrcLabelPrefix))
)

// alertMatcher is a helper to match
// active alerts based on the provided configuration
type alertMatcher struct {
	loader alertmanager.AlertLoader
}

func NewAlertMatcher(alertLoader alertmanager.AlertLoader) alertMatcher {
	return alertMatcher{
		loader: alertLoader,
	}
}

// The evaluateAlerts evaluates the health of the 'alerts' attribute of the component.
// It returns slice of matching alerts
func (p *alertMatcher) evaluateAlerts(alerts AlertsSelectors) ([]model.LabelSet, error) {
	var allMatchingAlerts []model.LabelSet
	for _, s := range alerts.Selectors {
		alerts, matchedLabels, err := p.matchingAlertFound(s.MatchLabels)
		if err != nil {
			return nil, err
		}
		cleanedAlerts := cleanupLabels(alerts, matchedLabels)
		allMatchingAlerts = append(allMatchingAlerts, cleanedAlerts...)
	}
	return allMatchingAlerts, nil
}

// matchingAlertFound iterates over the provided map of labels and tries
// to find matching alerts following the rules:
// - mutliple values in one label (under one key) are joined by logical OR
// - multiple labels (key-value pairs) are joined by logical AND
func (a *alertMatcher) matchingAlertFound(matchLabels map[string][]string) ([]models.Alert, map[string]string, error) {
	var matchingAlerts []models.Alert
	labelsMatched := make(map[string]string)

	for key, values := range matchLabels {
		// when there are already some matching alerts then
		// we need to check if any of them matches the label pair
		if len(matchingAlerts) > 0 {
			filteredMatchingAlerts := make([]models.Alert, 0, len(matchingAlerts))
			for _, ma := range matchingAlerts {
				matchFound := false
				for _, v := range values {
					if existingLV, ok := ma.Labels[key]; ok && existingLV == v {
						labelsMatched[key] = v
						matchFound = true
					}
				}
				if matchFound {
					filteredMatchingAlerts = append(filteredMatchingAlerts, ma)
				}
			}
			if len(filteredMatchingAlerts) == 0 {
				return nil, nil, nil
			}
			matchingAlerts = filteredMatchingAlerts
		} else {
			matchFound := false
			for _, v := range values {
				labelPair := fmt.Sprintf("%s=%s", key, v)
				alerts, err := a.loader.ActiveAlertsWithLabels([]string{labelPair})
				if err != nil {
					return nil, nil, err
				}
				if len(alerts) > 0 {
					matchFound = true
					labelsMatched[key] = v
				}
				matchingAlerts = append(matchingAlerts, alerts...)
			}
			// if there's no match found, we can return, because the
			// "matchLabels" requirement is not satisfied
			if !matchFound {
				return nil, nil, nil
			}
		}
	}

	return matchingAlerts, labelsMatched, nil
}

// cleanupLabels transforms the slice of models.Alert to simplified Alert type
// keeping only the required labels
func cleanupLabels(alerts []models.Alert, matchedLabels map[string]string) []model.LabelSet {
	var cleanedAlerts []model.LabelSet
	for _, a := range alerts {
		cleanAlert := model.LabelSet{
			srcAlertname: model.LabelValue(a.Labels["alertname"]),
			srcSeverity:  model.LabelValue(a.Labels["severity"]),
			srcNamespace: model.LabelValue(a.Labels["namespace"]),
		}
		for key, value := range matchedLabels {
			if key != "alertname" && key != "severity" && key != "namespace" {
				cleanAlert[model.LabelName(key)] = model.LabelValue(value)
			}
		}
		cleanedAlerts = append(cleanedAlerts, cleanAlert)
	}
	return cleanedAlerts
}

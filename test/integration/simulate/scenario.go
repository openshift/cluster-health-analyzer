// Package simulate provides utilities for running stress tests via the simulate command.
package simulate

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Alert represents one alert in a scenario.
type Alert struct {
	Start       int
	End         int
	Name        string
	Namespace   string
	Severity    string
	Silenced    bool
	ExtraLabels map[string]string
}

// ScenarioBuilder creates scenario CSV files for the simulate command.
type ScenarioBuilder struct {
	alerts []Alert
}

// NewScenarioBuilder creates a new scenario builder.
func NewScenarioBuilder() *ScenarioBuilder {
	return &ScenarioBuilder{}
}

// AddAlert adds a single alert to the scenario.
func (s *ScenarioBuilder) AddAlert(alert Alert) *ScenarioBuilder {
	s.alerts = append(s.alerts, alert)
	return s
}

// AddWatchdog adds a background Watchdog alert (commonly needed).
func (s *ScenarioBuilder) AddWatchdog(start, end int) *ScenarioBuilder {
	return s.AddAlert(Alert{
		Start:     start,
		End:       end,
		Name:      "Watchdog",
		Namespace: "openshift-monitoring",
		Severity:  "none",
		Silenced:  true,
	})
}

// AddStressAlerts adds multiple alerts with sequential names.
// The unique prefix (e.g., "StressSim1234567890") prevents grouping with other test runs.
func (s *ScenarioBuilder) AddStressAlerts(count int, prefix, namespace string, startTime, endTime int) *ScenarioBuilder {
	for i := 1; i <= count; i++ {
		start := startTime + (i % 100) // Slight variation in start times
		s.AddAlert(Alert{
			Start:       start,
			End:         endTime,
			Name:        fmt.Sprintf("%s%04d", prefix, i),
			Namespace:   namespace,
			Severity:    "warning",
			Silenced:    false,
			ExtraLabels: map[string]string{"component": "monitoring"},
		})
	}
	return s
}

// WriteCSV writes the scenario to a CSV file.
func (s *ScenarioBuilder) WriteCSV(path string) (retErr error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create scenario file: %w", err)
	}
	w := csv.NewWriter(f)

	defer func() {
		w.Flush()
		if flushErr := w.Error(); flushErr != nil && retErr == nil {
			retErr = flushErr
		}
		if closeErr := f.Close(); retErr == nil {
			retErr = closeErr
		}
	}()

	// Header
	if err := w.Write([]string{"start", "end", "alertname", "namespace", "severity", "silenced", "labels"}); err != nil {
		return err
	}

	// Alerts
	for _, a := range s.alerts {
		silenced := "false"
		if a.Silenced {
			silenced = "true"
		}

		labels := "{}"
		if len(a.ExtraLabels) > 0 {
			b, err := json.Marshal(a.ExtraLabels)
			if err != nil {
				return fmt.Errorf("failed to marshal labels for alert %s: %w", a.Name, err)
			}
			labels = string(b)
		}

		if err := w.Write([]string{
			strconv.Itoa(a.Start), strconv.Itoa(a.End),
			a.Name, a.Namespace, a.Severity, silenced, labels,
		}); err != nil {
			return err
		}
	}

	return nil
}

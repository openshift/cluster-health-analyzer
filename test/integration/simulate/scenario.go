// Package simulate provides utilities for running stress tests via the simulate command.
package simulate

import (
	"fmt"
	"os"
	"strings"
)

// Alert represents one alert in a scenario.
type Alert struct {
	Start      int
	End        int
	Name       string
	Namespace  string
	Severity   string
	Silenced   bool
	ExtraLabel string // Optional extra label key=value
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
			Start:      start,
			End:        endTime,
			Name:       fmt.Sprintf("%s%04d", prefix, i),
			Namespace:  namespace,
			Severity:   "warning",
			Silenced:   false,
			ExtraLabel: "component=monitoring",
		})
	}
	return s
}

// WriteCSV writes the scenario to a CSV file.
func (s *ScenarioBuilder) WriteCSV(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create scenario file: %w", err)
	}
	defer f.Close()

	// Header
	if _, err := f.WriteString("start,end,alertname,namespace,severity,silenced,labels\n"); err != nil {
		return err
	}

	// Alerts
	for _, a := range s.alerts {
		silenced := "false"
		if a.Silenced {
			silenced = "true"
		}

		labels := "{}"
		if a.ExtraLabel != "" {
			// ExtraLabel is in "key=value" format, convert to JSON {"key": "value"}
			if parts := strings.SplitN(a.ExtraLabel, "=", 2); len(parts) == 2 {
				labels = fmt.Sprintf("{\"%s\": \"%s\"}", parts[0], parts[1])
			}
		}

		line := fmt.Sprintf("%d,%d,%s,%s,%s,%s,%s\n",
			a.Start, a.End, a.Name, a.Namespace, a.Severity, silenced, labels)
		if _, err := f.WriteString(line); err != nil {
			return err
		}
	}

	return nil
}

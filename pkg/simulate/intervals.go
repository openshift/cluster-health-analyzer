package simulate

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/common/model"

	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/utils"
)

var defaultRelativeIntervals = []utils.RelativeInterval{
	{
		Labels: model.LabelSet{
			"alertname": "Watchdog",
			"namespace": "openshift-monitoring",
			"severity":  "none",
			"silenced":  "true",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "AlertmanagerReceiversNotConfigured",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"silenced":  "true",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "ClusterNotUpgradeable",
			"namespace": "openshift-cluster-version",
			"severity":  "info",
			"silenced":  "true",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "TargetDown",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3000,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "KubeNodeNotReady",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"node":      "ip-10-0-58-248.us-east-2.compute.internal",
			"condition": "Ready",
			"silenced":  "false",
		},
		Start: 3010,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "KubeNodeUnreachable",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"node":      "ip-10-0-58-248.us-east-2.compute.internal",
			"silenced":  "false",
		},
		Start: 3010,
		End:   4000,
	},
	// Simulate the ClusterOperatorDegraded alert to be flapping
	{
		Labels: model.LabelSet{
			"alertname": "ClusterOperatorDegraded",
			"namespace": "openshift-cluster-version",
			"severity":  "warning",
			"name":      "machine-config",
			"silenced":  "false",
		},
		Start: 3005,
		End:   3050,
	},
	{
		Labels: model.LabelSet{
			"alertname": "ClusterOperatorDegraded",
			"namespace": "openshift-cluster-version",
			"severity":  "warning",
			"name":      "machine-config",
			"silenced":  "false",
		},
		Start: 3100,
		End:   3200,
	},
	{
		Labels: model.LabelSet{
			"alertname": "ClusterOperatorDegraded",
			"namespace": "openshift-cluster-version",
			"severity":  "warning",
			"name":      "machine-config",
			"silenced":  "false",
		},
		Start: 3300,
		End:   3600,
	},
	{
		Labels: model.LabelSet{
			"alertname": "TargetDown",
			"namespace": "openshift-machine-config-operator",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3000,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "ClusterOperatorDown",
			"namespace": "openshift-cluster-version",
			"severity":  "warning",
			"name":      "monitoring",
			"silenced":  "false",
		},
		Start: 3000,
		End:   3200,
	},
	{
		Labels: model.LabelSet{
			"alertname": "ClusterOperatorDown",
			"namespace": "openshift-cluster-version",
			"severity":  "critical",
			"name":      "monitoring",
			"silenced":  "false",
		},
		Start: 3200,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "KubeDaemonSetRolloutStuck",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3000,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "PodDisruptionBudgetAtLimit",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "TargetDown",
			"namespace": "openshift-dns",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "KubeDaemonSetMisScheduled",
			"namespace": "openshift-dns",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "KubeDaemonSetMisScheduled",
			"namespace": "openshift-ingress-canary",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "KubeDaemonSetMisScheduled",
			"namespace": "openshift-network-operator",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: model.LabelSet{
			"alertname": "TargetDown",
			"namespace": "openshift-network-operator",
			"severity":  "warning",
			"silenced":  "false",
		},
		Start: 3020,
		End:   4000,
	},
}

func RelativeIntervalToAbsoluteInterval(ri utils.RelativeInterval, origin model.Time) processor.Interval {
	labels := make(model.LabelSet)
	for k, v := range ri.Labels {
		labels[k] = v
	}
	labels["alertstate"] = "firing"

	return processor.Interval{
		Start:  origin.Add(time.Duration(float64(ri.Start) * float64(time.Minute))),
		End:    origin.Add(time.Duration(float64(ri.End) * float64(time.Minute))),
		Metric: labels,
	}
}

func RelativeToAbsoluteIntervals(relIntervals []utils.RelativeInterval, end model.Time) []processor.Interval {
	maxEnd := 0
	for _, relInterval := range relIntervals {
		if relInterval.End > maxEnd {
			maxEnd = relInterval.End
		}
	}

	absStart := end.Add(time.Duration((float64)(-maxEnd) * float64(time.Minute)))

	ret := make([]processor.Interval, len(relIntervals))
	for i, ri := range relIntervals {
		ret[i] = RelativeIntervalToAbsoluteInterval(ri, absStart)
	}
	return ret
}

func readIntervalsFromCSV(scenarioFile string) ([]utils.RelativeInterval, error) {
	file, err := os.Open(scenarioFile)
	if err != nil {
		slog.Error("Failed to open CSV file", "error", err)
		return nil, err
	}
	defer file.Close() // nolint:errcheck

	return parseIntervalsFromCSV(file)
}

func parseIntervalsFromCSV(file io.Reader) ([]utils.RelativeInterval, error) {
	var intervals []utils.RelativeInterval
	csvReader := csv.NewReader(file)
	csvReader.LazyQuotes = true
	line := 0
	for {
		line++

		fields, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Error("Invalid CSV format", "line", line, "error", err)
			return nil, err
		} else if len(fields) != 7 {
			slog.Error("Invalid number of fields", "line", line, "expected", 7, "got", len(fields))
			return nil, errors.New("CSV parsing error")
		}

		// Skip the header
		if line == 1 {
			continue
		}

		start, err := strconv.Atoi(fields[0])
		if err != nil {
			slog.Error("Invalid start time", "line", line, "error", err)
			return nil, err
		}

		end, err := strconv.Atoi(fields[1])
		if err != nil {
			slog.Error("Invalid end time", "line", line, "error", err)
			return nil, err
		}

		labels := model.LabelSet{
			"alertname": model.LabelValue(fields[2]),
			"namespace": model.LabelValue(fields[3]),
			"severity":  model.LabelValue(fields[4]),
			"silenced":  model.LabelValue(fields[5]),
		}

		// Parse additional labels, if present
		if fields[6] != "" {
			var additionalLabels model.LabelSet
			err := json.Unmarshal([]byte(fields[6]), &additionalLabels)
			if err != nil {
				slog.Error("Invalid additional labels JSON", "line", line, "error", err)
				return nil, err
			}
			for k, v := range additionalLabels {
				labels[k] = v
			}
		}

		intervals = append(intervals, utils.RelativeInterval{
			Labels: labels,
			Start:  start,
			End:    end,
		})
	}

	return intervals, nil
}

// endTimeBuffer adds extra time to ensure alerts are still "firing" when queried.
// Without this buffer, alerts end exactly at time.Now() and immediately start
// becoming stale in Prometheus (5-minute staleness window). This caused integration
// tests to only see ~80% of alerts because the processor runs after a delay and
// queries at a later time when some alerts have already gone stale.
const endTimeBuffer = 10 * time.Minute

func buildAlertIntervals(scenarioFile string) ([]processor.Interval, error) {
	// Add buffer to end time to ensure alerts are still active when the processor runs.
	// This compensates for delays in test setup and processor polling intervals.
	end := model.TimeFromUnixNano(time.Now().Add(endTimeBuffer).UnixNano())
	intervals := defaultRelativeIntervals
	if scenarioFile != "" {
		csvIntervals, err := readIntervalsFromCSV(scenarioFile)
		if err != nil {
			return nil, err
		}
		intervals = csvIntervals
	}
	return RelativeToAbsoluteIntervals(intervals, end), nil
}

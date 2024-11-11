package simulate

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"

	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/utils"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

var outputFile = "cluster-health-analyzer-openmetrics.txt"
var scenarioFile string

var SimulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Generate simulated data in openmetrics format",
	Run: func(cmd *cobra.Command, args []string) {
		simulate(outputFile, scenarioFile)
	},
}

func init() {
	SimulateCmd.Flags().StringVarP(&outputFile, "output", "o", outputFile, "output file")
	SimulateCmd.Flags().StringVarP(&scenarioFile, "scenario", "s", "", "CSV file with the scenario to simulate")
}

var defaultRelativeIntervals = []utils.RelativeInterval{
	{
		Labels: map[string]string{
			"alertname": "Watchdog",
			"namespace": "openshift-monitoring",
			"severity":  "none",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "AlertmanagerReceiversNotConfigured",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "ClusterNotUpgradeable",
			"namespace": "openshift-cluster-version",
			"severity":  "info",
		},
		Start: 0,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "TargetDown",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
		},
		Start: 3000,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "KubeNodeNotReady",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"node":      "ip-10-0-58-248.us-east-2.compute.internal",
			"condition": "Ready",
		},
		Start: 3010,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "KubeNodeUnreachable",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
			"node":      "ip-10-0-58-248.us-east-2.compute.internal",
		},
		Start: 3010,
		End:   4000,
	},
	// Simulate the ClusterOperatorDegraded alert to be flapping
	{
		Labels: map[string]string{
			"alertname": "ClusterOperatorDegraded",
			"namespace": "openshift-cluster-version",
			"severity":  "warning",
			"name":      "machine-config",
		},
		Start: 3005,
		End:   3050,
	},
	{
		Labels: map[string]string{
			"alertname": "ClusterOperatorDegraded",
			"namespace": "openshift-cluster-version",
			"severity":  "warning",
			"name":      "machine-config",
		},
		Start: 3100,
		End:   3200,
	},
	{
		Labels: map[string]string{
			"alertname": "ClusterOperatorDegraded",
			"namespace": "openshift-cluster-version",
			"severity":  "warning",
			"name":      "machine-config",
		},
		Start: 3300,
		End:   3600,
	},
	{
		Labels: map[string]string{
			"alertname": "TargetDown",
			"namespace": "openshift-machine-config-operator",
			"severity":  "warning",
		},
		Start: 3000,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "ClusterOperatorDown",
			"namespace": "openshift-cluster-version",
			"severity":  "warning",
			"name":      "monitoring",
		},
		Start: 3000,
		End:   3200,
	},
	{
		Labels: map[string]string{
			"alertname": "ClusterOperatorDown",
			"namespace": "openshift-cluster-version",
			"severity":  "critical",
			"name":      "monitoring",
		},
		Start: 3200,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "KubeDaemonSetRolloutStuck",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
		},
		Start: 3000,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "PodDisruptionBudgetAtLimit",
			"namespace": "openshift-monitoring",
			"severity":  "warning",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "TargetDown",
			"namespace": "openshift-dns",
			"severity":  "warning",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "KubeDaemonSetMisScheduled",
			"namespace": "openshift-dns",
			"severity":  "warning",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "KubeDaemonSetMisScheduled",
			"namespace": "openshift-ingress-canary",
			"severity":  "warning",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "KubeDaemonSetMisScheduled",
			"namespace": "openshift-network-operator",
			"severity":  "warning",
		},
		Start: 3020,
		End:   4000,
	},
	{
		Labels: map[string]string{
			"alertname": "TargetDown",
			"namespace": "openshift-network-operator",
			"severity":  "warning",
		},
		Start: 3020,
		End:   4000,
	},
}

func RelativeIntervalToAbsoluteInterval(ri utils.RelativeInterval, origin model.Time) processor.Interval {
	labels := make(map[string]string)
	for k, v := range ri.Labels {
		labels[k] = v
	}
	labels["alertstate"] = "firing"

	return processor.Interval{
		Start:  origin.Add(time.Duration(float64(ri.Start) * float64(time.Minute))),
		End:    origin.Add(time.Duration(float64(ri.End) * float64(time.Minute))),
		Metric: prom.LabelSet{Labels: labels},
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
	defer file.Close()

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
		} else if len(fields) != 6 {
			slog.Error("Invalid number of fields", "line", line, "expected", 6, "got", len(fields))
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

		labels := map[string]string{
			"alertname": fields[2],
			"namespace": fields[3],
			"severity":  fields[4],
		}

		// Parse additional labels, if present
		if fields[5] != "" {
			var additionalLabels map[string]string
			err := json.Unmarshal([]byte(fields[5]), &additionalLabels)
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

func buildAlertIntervals(scenarioFile string) ([]processor.Interval, error) {
	end := model.TimeFromUnixNano(time.Now().UnixNano())
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

// fmtInterval writes the interval to the writer in OpenMetrics format.
func fmtInterval(
	w io.Writer,
	metricName string,
	labels map[string]string,
	start,
	end model.Time,
	step time.Duration,
	value float64,
) error {
	sb := new(strings.Builder)
	fmt.Fprintf(sb, "%s{", metricName)
	first := true
	for k, v := range labels {
		// if k == "alertname" {
		// 	continue
		// }
		if first {
			first = false
		} else {
			fmt.Fprint(sb, ",")
		}

		fmt.Fprintf(sb, "%s=\"%s\"", k, v)
	}
	fmt.Fprint(sb, "}")
	labelsStr := sb.String()

	for s := start; s <= end; s = s.Add(step) {
		_, err := fmt.Fprintf(w, "%s %f %d\n", labelsStr, value, s.Unix())
		if err != nil {
			return err
		}
	}
	return nil
}

func simulate(outputFile, scenarioFile string) {
	// Build sample intervals.
	intervals, err := buildAlertIntervals(scenarioFile)
	must(err)
	slog.Info("Generated intervals", "num", len(intervals))

	step := 5 * time.Minute
	start := intervals[0].Start
	end := intervals[0].End
	for _, i := range intervals {
		if i.Start.Before(start) {
			start = i.Start
		}
		if i.End.After(end) {
			end = i.End
		}
	}

	startToIntervals := make(map[model.Time][]processor.Interval)

	// Group them by time.
	for _, i := range intervals {
		startToIntervals[i.Start] = append(startToIntervals[i.Start], i)
	}

	// Prepare the changeset
	changes := make(processor.ChangeSet, len(startToIntervals))
	for t, intervals := range startToIntervals {
		changes = append(changes, processor.Change{
			Timestamp: t,
			Intervals: intervals,
		})
	}
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Timestamp.Before(changes[j].Timestamp)
	})

	f, err := os.Create(outputFile)
	must(err)
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	// Output ALERTS
	fmt.Fprintln(w, "# HELP ALERTS Alert status")
	fmt.Fprintln(w, "# TYPE ALERTS gauge")
	for _, i := range intervals {
		err := fmtInterval(w, "ALERTS", i.Metric.MLabels(), i.Start, i.End, step, 1)
		must(err)
	}

	// Output cluster:health:components
	fmt.Fprintln(w, "# HELP cluster:health:components Cluster health components ranking")
	fmt.Fprintln(w, "# TYPE cluster:health:components gauge")
	ranks := processor.BuildComponentRanks()
	for _, rank := range ranks {
		err := fmtInterval(w, "cluster:health:components", map[string]string{
			"layer":     rank.Layer,
			"component": rank.Component,
		}, start, end, step, float64(rank.Rank))
		must(err)
	}

	gc := &processor.GroupsCollection{}
	var groupedIntervalsSet []processor.GroupedInterval

	for _, change := range changes {
		groupedIntervals := gc.ProcessIntervalsBatch(change.Intervals)
		groupedIntervalsSet = append(groupedIntervalsSet, groupedIntervals...)
	}

	// Output cluster;health;components:map
	fmt.Fprintln(w, "# HELP cluster:health:components:map Cluster health components mapping")
	fmt.Fprintln(w, "# TYPE cluster:health:components:map gauge")

	for _, gi := range groupedIntervalsSet {
		labels := gi.Metric.MLabels()
		labels["group_id"] = gi.GroupMatcher.RootGroupID
		alert := prom.Alert{
			Name:   labels["alertname"],
			Labels: labels,
		}

		// Map alert to component
		healthMap := processor.MapAlerts([]prom.Alert{alert})[0]
		err := fmtInterval(w, "cluster:health:components:map", healthMap.Labels(), gi.Start, gi.End, step, float64(healthMap.Health))
		must(err)
	}
	fmt.Fprint(w, "# EOF")

	groups := make(map[string][]processor.GroupedInterval)
	for _, gi := range groupedIntervalsSet {
		groups[gi.GroupMatcher.RootGroupID] = append(groups[gi.GroupMatcher.RootGroupID], gi)
	}

	for _, intervals := range groups {
		start := intervals[0].Start
		end := intervals[0].End
		alerts := make(map[string]struct{})

		for _, interval := range intervals {
			if interval.Start.Before(start) {
				start = interval.Start
			}
			if interval.End.After(end) {
				end = interval.End
			}
			alertname := interval.Interval.Metric.MLabels()["alertname"]
			alerts[alertname] = struct{}{}
		}
	}

	slog.Info("Generated incidents", "num", len(groups))

	slog.Info("Openmetrics file saved", "output", outputFile)
}

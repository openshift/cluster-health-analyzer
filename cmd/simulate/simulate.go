package simulate

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
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

var SimulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Generate simulated data in openmetrics format",
	Run: func(cmd *cobra.Command, args []string) {
		simulate(outputFile)
	},
}

func init() {
	SimulateCmd.Flags().StringVarP(&outputFile, "output", "o", outputFile, "output file")
}

var relIntervals = []utils.RelativeInterval{
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

func buildAlertIntervals() []processor.Interval {
	end := model.TimeFromUnixNano(time.Now().UnixNano())
	return RelativeToAbsoluteIntervals(relIntervals, end)
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

func simulate(outputFile string) {
	// Build sample intervals.
	intervals := buildAlertIntervals()

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

	slog.Info("Generating alerts", "changes", len(changes))

	f, err := os.Create(outputFile)
	must(err)
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	// Output ALERTS
	fmt.Fprintln(w, "# HELP ALERTS Alert status")
	fmt.Fprintln(w, "# TYPE ALERTS gauge")
	for _, i := range intervals {
		fmtInterval(w, "ALERTS", i.Metric.MLabels(), i.Start, i.End, step, 1)
	}

	// Output cluster:health:components
	fmt.Fprintln(w, "# HELP cluster:health:components Cluster health components ranking")
	fmt.Fprintln(w, "# TYPE cluster:health:components gauge")
	ranks := processor.BuildComponentRanks()
	for _, rank := range ranks {
		fmtInterval(w, "cluster:health:components", map[string]string{
			"layer":     rank.Layer,
			"component": rank.Component,
		}, start, end, step, float64(rank.Rank))
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

	for groupID, intervals := range groups {
		slog.Info("Start generating group", "group_id", groupID)
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

		slog.Info("End generating group", "alerts", alerts, "start", start, "end", end)
	}
}

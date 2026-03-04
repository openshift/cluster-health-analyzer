package simulate

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/common/model"

	"github.com/openshift/cluster-health-analyzer/pkg/processor"
)

// Simulate generates simulated Prometheus metrics in OpenMetrics format.
// When alertsOnly is true, only ALERTS metrics are written — the processed
// cluster_health_components metrics are skipped so the analyzer under test
// can compute them itself.
func Simulate(ctx context.Context, outputFile, scenarioFile string, alertsOnly bool) error {
	intervals, err := buildAlertIntervals(scenarioFile)
	if err != nil {
		return fmt.Errorf("building alert intervals: %w", err)
	}
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

	f, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close() // nolint:errcheck

	w := bufio.NewWriter(f)
	defer w.Flush() // nolint:errcheck

	// Output ALERTS
	if _, err := fmt.Fprintln(w, "# HELP ALERTS Alert status"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE ALERTS gauge"); err != nil {
		return err
	}
	for _, i := range intervals {
		if err := fmtInterval(ctx, w, "ALERTS", i.Metric, i.Start, i.End, step, 1); err != nil {
			return err
		}
	}

	// When alertsOnly is set, skip the processed metrics - they should be
	// computed by the cluster-health-analyzer being tested.
	if alertsOnly {
		if _, err = fmt.Fprint(w, "# EOF"); err != nil {
			return err
		}
		slog.Info("Openmetrics file saved (alerts only)", "output", outputFile)
		return nil
	}

	// Output cluster_health_components
	if _, err := fmt.Fprintln(w, "# HELP cluster_health_components Cluster health components ranking"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE cluster_health_components gauge"); err != nil {
		return err
	}
	ranks := processor.BuildComponentRanks()
	for _, rank := range ranks {
		if err := fmtInterval(ctx, w, "cluster_health_components", model.LabelSet{
			"layer":     model.LabelValue(rank.Layer),
			"component": model.LabelValue(rank.Component),
		}, start, end, step, float64(rank.Rank)); err != nil {
			return err
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

	gc := &processor.GroupsCollection{}
	var groupedIntervalsSet []processor.GroupedInterval

	for _, change := range changes {
		groupedIntervals := gc.ProcessIntervalsBatch(change.Intervals)
		groupedIntervalsSet = append(groupedIntervalsSet, groupedIntervals...)
	}

	// Output cluster_health_components_map
	if _, err := fmt.Fprintln(w, "# HELP cluster_health_components_map Cluster health components mapping"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE cluster_health_components_map gauge"); err != nil {
		return err
	}

	for _, gi := range groupedIntervalsSet {
		alert := gi.Metric
		alert["group_id"] = model.LabelValue(gi.GroupMatcher.RootGroupID)

		healthMap := processor.MapAlerts([]model.LabelSet{alert})[0]
		if err := fmtInterval(ctx, w, "cluster_health_components_map", healthMap.Labels(), gi.Start, gi.End, step, float64(healthMap.Health)); err != nil {
			return err
		}
	}

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
			alertname := string(interval.Metric["alertname"])
			alerts[alertname] = struct{}{}
		}
	}

	slog.Info("Generated incidents", "num", len(groups))

	if _, err = fmt.Fprint(w, "# EOF"); err != nil {
		return err
	}

	slog.Info("Openmetrics file saved", "output", outputFile)
	return nil
}

// fmtInterval writes the interval to the writer in OpenMetrics format.
func fmtInterval(
	ctx context.Context,
	w io.Writer,
	metricName string,
	labels model.LabelSet,
	start,
	end model.Time,
	step time.Duration,
	value float64,
) error {
	sb := new(strings.Builder)
	fmt.Fprintf(sb, "%s{", metricName)
	first := true
	for k, v := range labels {
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
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, "%s %f %d\n", labelsStr, value, s.Unix())
		if err != nil {
			return err
		}
	}
	return nil
}

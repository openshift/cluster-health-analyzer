// Package processor contains the core logic for processing alerts and
// updating the health map metrics.
package processor

import (
	"context"
	"log/slog"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"k8s.io/apimachinery/pkg/util/wait"
)

// processor is the component responsible for continuously loading alerts from source
// and coordinates updating the exported metrics.
type processor struct {
	// healthMapMetrics maps input signal (alerts) to components, incidents
	// and normalized severity.
	healthMapMetrics prom.MetricSet

	// componentsMetrics maps components to their ranking via the metric value.
	componentsMetrics prom.MetricSet

	// interval is the time interval between processing iterations.
	interval time.Duration

	loader           *prom.Loader
	groupsCollection *GroupsCollection
}

func NewProcessor(healthMapMetrics, componentsMetrics prom.MetricSet, interval time.Duration) (*processor, error) {
	promLoader, err := prom.NewLoader()
	if err != nil {
		return nil, err
	}
	return &processor{
		healthMapMetrics:  healthMapMetrics,
		componentsMetrics: componentsMetrics,
		interval:          interval,
		loader:            promLoader,
	}, nil
}

// Start starts the processor in a goroutine and returns immediately.
func (p *processor) Start(ctx context.Context) {
	go p.Run(ctx)
}

// initGroupsCollection initializes the groups collection by loading the alerts.
//
// The alerts are loaded for the given time range and step and prepares the structure
// for assigning group-ids to the alerts.
func (p *processor) InitGroupsCollection(ctx context.Context, start, end time.Time, step time.Duration) error {
	slog.Info("Initializing groups collection", "start", start, "end", end, "step", step)
	p.groupsCollection = &GroupsCollection{}

	// TODO(falox): log the length of the results
	slog.Info("Loading alerts range")
	alertsRange, err := p.loader.LoadAlertsRange(ctx, start, end, step)
	if err != nil {
		return err
	}

	// Warm up the groups collection with historical alerts.
	slog.Info("Processing historical alerts")
	p.groupsCollection.processHistoricalAlerts(alertsRange)

	slog.Info("Loading health map range")
	healthMapRV, err := p.loader.LoadVectorRange(ctx, "cluster:health:components:map", start, end, step)
	if err != nil {
		return err
	}

	slog.Info("Updating group-ids")
	p.groupsCollection.UpdateGroupUUIDs(healthMapRV)

	return nil
}

// Run runs the processor and blocks until canceled via the ctx.
func (p *processor) Run(ctx context.Context) {
	// wait.Until provides the core for the repeated execution of the Process method
	wait.Until(func() {
		// wait.ExponentialBackoffWithContext provides a backoff mechanism
		// in case of errors during the Process method execution.
		wait.ExponentialBackoffWithContext(
			ctx,
			wait.Backoff{Duration: time.Second, Steps: 4, Factor: 1.5},
			func(ctx context.Context) (bool, error) {
				slog.Info("Begin processing")

				err := p.Process(ctx)
				if err != nil {
					slog.Error("Error processing", "err", err)
					// We don't return an error here because we want to keep retrying.
					return false, nil
				}

				slog.Info("End processing")
				return true, nil
			})
	}, p.interval, ctx.Done())
}

// dedupHealthMaps deduplicates the health maps by combining the health values.
//
// The deduplication is done by hashing the label values of the health maps.
// For duplicates, the health value is combined by taking the maximum of the two.
func dedupHealthMaps(healthMaps []ComponentHealthMap) []ComponentHealthMap {
	hashMap := make(map[uint64]ComponentHealthMap, len(healthMaps))

	for _, healthMap := range healthMaps {
		hash := healthMap.hashLabelValues()
		existing, ok := hashMap[hash]
		if ok {
			existing.Health = max(existing.Health, healthMap.Health)
		} else {
			hashMap[hash] = healthMap
		}
	}

	deduped := make([]ComponentHealthMap, 0, len(hashMap))
	for _, healthMap := range hashMap {
		deduped = append(deduped, healthMap)
	}
	return deduped
}

func (p *processor) assignAlertsToGroups(alerts []prom.Alert, t time.Time) []prom.Alert {
	processedAlerts := p.groupsCollection.ProcessAlertsBatch(alerts, t)

	// Prune the groups collection to remove old groups.
	p.groupsCollection.PruneGroups(t)
	return processedAlerts
}

// Process performs a single iteration of the processor.
func (p *processor) Process(ctx context.Context) error {
	err := p.updateHealthMap(ctx)
	if err != nil {
		return err
	}

	p.updateComponentsMetrics()

	return nil
}

func (p *processor) updateHealthMap(ctx context.Context) error {
	t := time.Now()
	alerts, err := p.loader.LoadAlerts(ctx, t)
	if err != nil {
		return err
	}

	if p.groupsCollection != nil {
		alerts = p.assignAlertsToGroups(alerts, t)
	}

	alertsHealthMap := MapAlerts(alerts)
	alertsHealthMap = dedupHealthMaps(alertsHealthMap)

	metrics := make([]prom.Metric, 0, len(alertsHealthMap))
	for _, healthMap := range alertsHealthMap {
		metrics = append(metrics, prom.Metric{
			Labels: healthMap.Labels(),
			Value:  float64(healthMap.Health),
		})
	}
	p.healthMapMetrics.Update(metrics)

	return nil
}

func (p *processor) updateComponentsMetrics() {
	ranks := BuildComponentRanks()

	metrics := make([]prom.Metric, 0)
	for _, r := range ranks {
		metrics = append(metrics, prom.Metric{
			Labels: map[string]string{
				"layer":     r.Layer,
				"component": r.Component,
			},
			Value: float64(r.Rank),
		})
	}
	p.componentsMetrics.Update(metrics)
}

type ComponentRank struct {
	Layer     string
	Component string
	Rank      int
}

func BuildComponentRanks() []ComponentRank {
	components := make(map[string]ComponentRank)
	components["compute"] = ComponentRank{Layer: "compute", Component: "compute", Rank: 1}

	for i, m := range coreMatchers {
		components[m.component] = ComponentRank{Layer: "core", Component: m.component, Rank: 10 + i*5}
	}

	for i, m := range workloadMatchers {
		components[m.component] = ComponentRank{Layer: "workload", Component: m.component, Rank: 1000 + i*5}
	}

	ret := make([]ComponentRank, 0, len(components))
	for _, c := range components {
		ret = append(ret, c)
	}
	return ret
}

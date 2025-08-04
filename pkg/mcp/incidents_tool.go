package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/openshift/cluster-health-analyzer/pkg/common"
	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// IncidentsTool create a new MCP tool for the incidents
func IncidentsTool() mcp.Tool {
	readOnly := true
	return mcp.Tool{
		Name: "get_incidents",
		Description: `List the current firing incidents in the cluster. 
		One incident is a group of related alerts that are likely triggered by the same root cause.
		Use this tool to analyze the cluster health status and determine why a component is failing or degraded.`,
		Annotations: mcp.ToolAnnotation{
			Title:        "Provides information about Incidents in the cluster",
			ReadOnlyHint: &readOnly,
		},
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: make(map[string]interface{}),
		},
	}
}

// IncidentsHandler is the main handler for the Incidents. It connects to the
// in-cluster Prometheus and queries the Incidents metrics.
func IncidentsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slog.Info("Incidents tool received request with ", "params", request.Params, "and arguments ", request.Params.Arguments)
	token, err := getTokenFromCtx(ctx)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}

	promURL := os.Getenv("PROM_URL")
	promClient, err := prom.NewPrometheusClientWithToken(promURL, token)
	if err != nil {
		slog.Error("Failed to initialize Prometheus client", "error", err)
		return nil, err
	}

	promAPI := v1.NewAPI(promClient)
	timeNow := time.Now().UTC()
	queryTimeRange := v1.Range{
		Start: timeNow.Add(-15 * 24 * time.Hour),
		End:   timeNow,
		Step:  300 * time.Second,
	}
	val, warning, err := promAPI.QueryRange(ctx, processor.ClusterHealthComponentsMap, queryTimeRange)
	if err != nil {
		slog.Error("Recieved error response from Prometheus", "error", err)
		return nil, err
	}
	if warning != nil {
		slog.Warn("Prometheus query response", "warning", warning)
	}

	incidentsMap, err := transformPromValueToIncident(val, queryTimeRange)
	if err != nil {
		slog.Error("Failed to transform metric data", "error", err)
		return nil, err
	}

	incidents := getAlertDataForIncidents(ctx, incidentsMap, promAPI, queryTimeRange)
	r := Response{
		Incidents: Incidents{
			Total:     len(incidents),
			Incidents: incidents,
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		slog.Error("Failed to marshal the Incident data", "error", err)
		return nil, err
	}
	return mcp.NewToolResultText(string(data)), nil
}

// formatToRFC3339 formats a time to RFC3339 string, returns empty string for zero time
func formatToRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// processSampleTimeAndStatus calculates the delta between the two samples and if it's greater
// than the range step then the return status is resolved and endTime is set, otherwise it returns
// firing status and endTime is an empty string.
func processSampleTimeAndStatus(firstSample, lastSample model.SamplePair, qRange v1.Range) (string, string, string) {
	status := "firing"
	startTime := firstSample.Timestamp.Time().UTC()
	var endTime time.Time

	if qRange.End.Sub(lastSample.Timestamp.Time().UTC()).Seconds() > qRange.Step.Seconds() {
		status = "resolved"
		endTime = lastSample.Timestamp.Time().UTC()
	}
	return formatToRFC3339(startTime),
		formatToRFC3339(endTime),
		status
}

// transformPromValueToIncident transforms the metrics data to map of incidents
func transformPromValueToIncident(data model.Value, qRange v1.Range) (map[string]Incident, error) {
	dataVec, ok := data.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("cannot convert data to Prometheus model.Vector type")
	}

	incidents := make(map[string]Incident, len(dataVec))
	for _, v := range dataVec {
		alertSeverity := v.Metric["src_severity"]
		alertName := v.Metric["src_alertname"]
		if alertSeverity == "none" {
			slog.Debug("Skipping unknown severity ", "alert", alertName, "severity", alertSeverity)
			continue
		}

		lastSample := v.Values[len(v.Values)-1]
		firstSample := v.Values[0]
		startTime, endTime, status := processSampleTimeAndStatus(firstSample, lastSample, qRange)

		labels := common.SrcLabels(v.Metric)
		healthyVal := processor.HealthValue(lastSample.Value)
		groupId := string(v.Metric["group_id"])
		component := string(v.Metric["component"])

		if existingInc, ok := incidents[groupId]; ok {
			existingInc.ComponentsSet[component] = struct{}{}
			existingInc.AffectedComponents = slices.Collect(maps.Keys(existingInc.ComponentsSet))
			sort.Strings(existingInc.AffectedComponents)
			existingInc.Alerts = append(existingInc.Alerts, model.LabelSet(labels))
			if healthyVal > processor.ParseHealthValue(existingInc.Severity) {
				existingInc.Severity = healthyVal.String()
			}
			existingInc.EndTime = endTime
			existingInc.Status = status
			incidents[existingInc.GroupId] = existingInc
		} else {
			incident := Incident{
				GroupId:   string(groupId),
				Severity:  healthyVal.String(),
				StartTime: startTime,
				EndTime:   endTime,
				Status:    status,
				ComponentsSet: map[string]struct{}{
					component: {},
				},
				AffectedComponents: []string{component},
				Alerts:             []model.LabelSet{labels},
			}
			incidents[groupId] = incident
		}
	}
	return incidents, nil
}

// getTokenFromCtx gets the authorization header from the
// provided context
func getTokenFromCtx(ctx context.Context) (string, error) {
	k8sToken := ctx.Value(authHeaderStr)
	k8TokenStr, ok := k8sToken.(string)
	if !ok {
		return "", fmt.Errorf("failed to convert the authorization token to string")
	}
	return k8TokenStr, nil
}

// getAlertDataForIncidents queries Prometheus for firing alerts from the last 15 days (to have
// some starting time) and then maps (the alert identifier is composed by name and namespace)
// the active alerts to the provided map of incidents. It returns slice of the incidents.
func getAlertDataForIncidents(ctx context.Context, incidents map[string]Incident, promAPI v1.API, qRange v1.Range) []Incident {
	v, _, err := promAPI.QueryRange(ctx, `ALERTS{alertstate!="pending"}`, qRange)
	if err != nil {
		slog.Error("Failed to query firing alerts", "error", err)
		return nil
	}
	alertData, ok := v.(model.Matrix)
	if !ok {
		slog.Error("Failed to convert alert data")
		return nil
	}

	var alerts []model.LabelSet
	for i := range alertData {
		sample := alertData[i]
		metric := sample.Metric
		firstSample := sample.Values[0]
		lastSample := sample.Values[len(sample.Values)-1]
		startTime, endTime, status := processSampleTimeAndStatus(firstSample, lastSample, qRange)

		metric[model.LabelName("start_time")] = model.LabelValue(startTime)
		if endTime != "" {
			metric[model.LabelName("end_time")] = model.LabelValue(endTime)
		}
		metric[model.LabelName("alertstate")] = model.LabelValue(status)
		alerts = append(alerts, model.LabelSet(metric))
	}

	var incidentsSlice []Incident
	for _, inc := range incidents {
		var updatedAlerts []model.LabelSet
		for _, alertInIncident := range inc.Alerts {
			subsetMatcher := common.LabelsSubsetMatcher{Labels: alertInIncident}
			for _, firingAlert := range alerts {
				match, _ := subsetMatcher.Matches(firingAlert)
				if match {
					updatedAlerts = append(updatedAlerts, firingAlert)
				}
			}
		}
		inc.Alerts = updatedAlerts
		incidentsSlice = append(incidentsSlice, inc)
	}
	return incidentsSlice
}

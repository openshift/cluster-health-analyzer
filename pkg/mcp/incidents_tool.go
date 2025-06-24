package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
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
	token, err := tokenFromCtx(ctx)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}

	promURL := os.Getenv("PROM_URL")
	promCli, err := prom.NewPrometheusClientWithToken(promURL, token)
	if err != nil {
		slog.Error("Failed to initialize Prometheus client", "error", err)
		return nil, err
	}

	promAPI := v1.NewAPI(promCli)
	val, warning, err := promAPI.Query(ctx, "cluster:health:components:map{}", time.Now())
	if err != nil {
		slog.Error("Recieved error response from Prometheus", "error", err)
		return nil, err
	}
	if warning != nil {
		slog.Warn("Prometheus query response", "warning", warning)
	}

	incidentsMap, err := transformPromValueToIncident(val)
	if err != nil {
		slog.Error("Failed to transform metric data", "error", err)
		return nil, err
	}

	incidents := getAlertDataForIncidents(ctx, incidentsMap, promAPI)
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

// transformPromValueToIncident transforms the metrics data to map of incidents
func transformPromValueToIncident(data model.Value) (map[string]Incident, error) {
	dataVec, ok := data.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("cannot convert data to Prometheus model.Vector type")
	}

	incidents := make(map[string]Incident, len(dataVec))
	for _, v := range dataVec {
		alertSeverity := v.Metric["src_severity"]
		alertName := v.Metric["src_alertname"]
		if alertSeverity == "none" || alertSeverity == "info" {
			slog.Debug("Skipping low severity ", "alert", alertName, "severity", alertSeverity)
			continue
		}
		healthyVal := processor.HealthValue(v.Value)
		severity := healthyVal.String()
		groupId := string(v.Metric["group_id"])
		component := string(v.Metric["component"])
		namespace := v.Metric["src_namespace"]

		if existingInc, ok := incidents[groupId]; ok {
			existingInc.AffectedComponents = append(existingInc.AffectedComponents, component)
			existingInc.Alerts = append(existingInc.Alerts, model.Metric{"alertname": alertName, "namespace": namespace})
			incidents[existingInc.GroupId] = existingInc
		} else {
			incidents[groupId] = Incident{
				GroupId:            string(groupId),
				Severity:           severity,
				Status:             "firing",
				AffectedComponents: []string{component},
				Alerts:             []model.Metric{{"alertname": alertName, "namespace": namespace}},
			}
		}
	}
	return incidents, nil
}

// tokenFromCtx gets the authorization header from the
// provide context
func tokenFromCtx(ctx context.Context) (string, error) {
	k8sToken := ctx.Value(authHeaderStr)
	k8TokenStr, ok := k8sToken.(string)
	if !ok {
		return "", fmt.Errorf("failed to convert the authorization token to string")
	}
	return k8TokenStr, nil
}

// alertIdentifier helps to identify particular alert,
// but it currently does not distinguish alert with the same
// name and in the same namespace
type alertIdentifier struct {
	name      string
	namespace string
}

func alertIdentifierFromSample(s model.Sample) alertIdentifier {
	return alertIdentifier{
		name:      string(s.Metric["alertname"]),
		namespace: string(s.Metric["namespace"]),
	}
}

// getAlertDataForIncidents queries Prometheus for firing alerts from the last 15 days (to have
// some starting time) and then maps (the alert identifier is composed by name and namespace)
// the active alerts to the provided map of incidents. It returns slice of the incidents.
func getAlertDataForIncidents(ctx context.Context, incidents map[string]Incident, promAPI v1.API) []Incident {
	v, _, err := promAPI.Query(ctx, `min_over_time(timestamp(ALERTS{alertstate="firing"})[15d:1m])`, time.Now())
	if err != nil {
		slog.Error("Failed to query firing alerts", "error", err)
		return nil
	}

	alertData, ok := v.(model.Vector)
	if !ok {
		slog.Error("Failed to convert alert data")
		return nil
	}

	var incidentsSlice []Incident

	alertsByName := make(map[alertIdentifier]model.Metric)
	for i := range alertData {
		sample := alertData[i]
		alertID := alertIdentifierFromSample(*sample)

		metric := sample.Metric
		alertStartTime := time.Unix(int64(sample.Value), 0).UTC().Format(time.RFC3339)
		metric[model.LabelName("start_time")] = model.LabelValue(alertStartTime)
		alertsByName[alertID] = metric
	}

	for _, inc := range incidents {
		updatedAlerts := make([]model.Metric, 0, len(inc.Alerts))

		incidentStartTime := time.Now().UTC()
		for _, alert := range inc.Alerts {
			alertName := string(alert["alertname"])
			alertNamespace := string(alert["namespace"])
			alertLabels := alertsByName[alertIdentifier{name: alertName, namespace: alertNamespace}]
			alertStartTime, err := time.Parse(time.RFC3339, string(alertLabels["start_time"]))
			if err != nil {
				slog.Error("Failed to convert string to time", "string", string(alertLabels["start_time"]), "error", err)
				continue
			}
			if alertStartTime.Before(incidentStartTime) {
				incidentStartTime = alertStartTime.UTC()
			}
			updatedAlerts = append(updatedAlerts, alertLabels)
		}
		inc.Alerts = updatedAlerts
		inc.StartTime = incidentStartTime.UTC().Format(time.RFC3339)
		incidentsSlice = append(incidentsSlice, inc)
	}

	return incidentsSlice
}

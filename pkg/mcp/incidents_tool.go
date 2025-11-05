package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openshift/cluster-health-analyzer/pkg/alertmanager"
	"github.com/openshift/cluster-health-analyzer/pkg/common"
	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/utils"
	"github.com/prometheus/alertmanager/api/v2/models"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var (
	// Format response with instructions for better LLM interpretation
	getIncidentsResponseTemplate = `<DATA>
%s
</DATA>
<INSTRUCTIONS>
- An incident is a group of related alerts. Base your analysis on the alerts to understand the incident. 
- Don't confuse or mix the concepts of incident and alert during your explanation.
- For each incident, analyze its alerts to identify the affected components and the core problem. 
- Whenever you print an incident ID, add also a short one-sentence summary of the incident (e.g. "etcd degradation", "ingress failure")
- If the user asks about a problem you cannot find in the data, do not guess. State that you cannot find the cause and simply list the incidents.
</INSTRUCTIONS>`
)

type IncidentTool struct {
	Tool mcp.Tool
	cfg  incidentToolCfg
	// the followings allow to use mocked instance of needed clients for testing
	getPrometheusLoaderFn   func(string, string) (prom.Loader, error)
	getAlertManagerLoaderFn func(string, string) (alertmanager.Loader, error)
}

type incidentToolCfg struct {
	promURL         string
	alertManagerURL string
	consoleURL      string
}

type GetIncidentsParams struct {
	MaxAgeHours uint `json:"max_age_hours"`
}

var (
	defaultMcpGetIncidentsTool = mcp.Tool{
		Name: "get_incidents",
		Description: `List the current firing incidents in the cluster. 
		One incident is a group of related alerts that are likely triggered by the same root cause.
		Use this tool to analyze the cluster health status and determine why a component is failing or degraded.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Provides information about Incidents in the cluster",
			ReadOnlyHint: true,
		},
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"max_age_hours": {
					Type:        "number",
					Description: "Maximum age of incidents to include in hours (max 360 for 15 days). Default: 360",
					Minimum:     utils.Ptr(float64(1)),
					Maximum:     utils.Ptr(float64(360)),
				},
			},
		},
	}
)

// NewIncidentsTool creates a new MCP tool for the incidents
func NewIncidentsTool(promURL, alertmanagerURL string) IncidentTool {
	var err error

	consoleURL, err := getConsoleURL()
	if err != nil {
		slog.Error("Failed to obtain cluster console URL", "error", err)
	}

	return IncidentTool{
		Tool: defaultMcpGetIncidentsTool,
		cfg: incidentToolCfg{
			promURL:         promURL,
			alertManagerURL: alertmanagerURL,
			consoleURL:      consoleURL,
		},
		getPrometheusLoaderFn:   defaultPrometheusLoader,
		getAlertManagerLoaderFn: defaultAlertManagerLoader,
	}
}

// IncidentsHandler is the main handler for the Incidents. It connects to the
// in-cluster Prometheus and queries the Incidents metrics.
func (i *IncidentTool) IncidentsHandler(ctx context.Context, request *mcp.CallToolRequest, params GetIncidentsParams) (*mcp.CallToolResult, any, error) {
	slog.Info("Incidents tool received request with ", "params", params)
	token, err := getTokenFromCtx(ctx)
	if err != nil {
		slog.Error(err.Error())
		return nil, nil, err
	}

	amLoader, err := i.getAlertManagerLoaderFn(i.cfg.alertManagerURL, token)
	if err != nil {
		slog.Error("Failed to initialize AlertManager client", "error", err)
		return nil, nil, err
	}

	promLoader, err := i.getPrometheusLoaderFn(i.cfg.promURL, token)
	if err != nil {
		slog.Error("Failed to initialize Prometheus client", "error", err)
		return nil, nil, err
	}

	maxAgeHours := 360 // 15 days default
	if params.MaxAgeHours > 0 {
		maxAgeHours = int(params.MaxAgeHours)
	}

	timeNow := time.Now()
	queryTimeRange := v1.Range{
		Start: timeNow.Add(-time.Duration(maxAgeHours) * time.Hour),
		End:   timeNow,
		Step:  300 * time.Second,
	}

	val, err := promLoader.LoadVectorRange(ctx, processor.ClusterHealthComponentsMap, queryTimeRange.Start, queryTimeRange.End, queryTimeRange.Step)
	if err != nil {
		slog.Error("Received error response from Prometheus", "error", err)
		return nil, nil, err
	}

	silences, err := amLoader.SilencedAlerts()
	if err != nil {
		slog.Error("Failed retrieving silenced alerts from AlertManager", "error", err)
		return nil, nil, err
	}

	incidentsMap, err := i.transformPromValueToIncident(val, queryTimeRange)
	if err != nil {
		slog.Error("Failed to transform metric data", "error", err)
		return nil, nil, err
	}

	incidents := getAlertDataForIncidents(ctx, incidentsMap, silences, promLoader, queryTimeRange)
	r := Response{
		Incidents: Incidents{
			Total:     len(incidents),
			Incidents: incidents,
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		slog.Error("Failed to marshal the Incident data", "error", err)
		return nil, nil, err
	}

	response := fmt.Sprintf(getIncidentsResponseTemplate, string(data))
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: response},
		},
	}, nil, nil
}

// formatToRFC3339 formats a time to RFC3339 string, returns empty string for zero time
func formatToRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// processSampleTime calculates the delta between the two samples and if it's greater
// than the range step then the endTime is set, otherwise it returns zero endTime
func processSampleTime(firstSample, lastSample model.SamplePair, qRange v1.Range) (time.Time, time.Time) {
	startTime := firstSample.Timestamp.Time()
	var endTime time.Time

	if qRange.End.Sub(lastSample.Timestamp.Time()).Seconds() > qRange.Step.Seconds() {
		endTime = lastSample.Timestamp.Time()
	}
	return startTime, endTime
}

// transformPromValueToIncident transforms the metrics data to map of incidents
func (i *IncidentTool) transformPromValueToIncident(dataVec prom.RangeVector, qRange v1.Range) (map[string]Incident, error) {

	incidents := make(map[string]Incident, len(dataVec))
	for _, v := range dataVec {

		alertSeverity := v.Metric["src_severity"]
		alertName := v.Metric["src_alertname"]

		if alertSeverity == "none" {
			slog.Debug("Skipping unknown severity ", "alert", alertName, "severity", alertSeverity)
			continue
		}

		lastSample := v.Samples[len(v.Samples)-1]
		firstSample := v.Samples[0]
		startTime, endTime := processSampleTime(firstSample, lastSample, qRange)

		labels := common.SrcLabels(model.Metric(v.Metric))

		healthyVal := processor.HealthValue(lastSample.Value)
		groupId := string(v.Metric["group_id"])
		component := string(v.Metric["component"])

		clusterName := v.Metric["cluster"]
		clusterID := v.Metric["clusterID"]

		if existingInc, ok := incidents[groupId]; ok {
			existingInc.ComponentsSet[component] = struct{}{}
			existingInc.AffectedComponents = slices.Collect(maps.Keys(existingInc.ComponentsSet))
			sort.Strings(existingInc.AffectedComponents)

			if _, ok := existingInc.AlertsSet[labels.String()]; !ok {
				existingInc.AlertsSet[labels.String()] = struct{}{}
				existingInc.Alerts = append(existingInc.Alerts, labels)
			}

			if healthyVal > processor.ParseHealthValue(existingInc.Severity) {
				existingInc.Severity = healthyVal.String()
			}
			err := existingInc.UpdateStartTime(startTime)
			if err != nil {
				slog.Error("Failed to parse the start time of an incident ", "error", err)
				continue
			}
			err = existingInc.UpdateEndTime(endTime)
			if err != nil {
				slog.Error("Failed to parse the end time of an incident ", "error", err)
				continue
			}
			existingInc.UpdateStatus()
			incidents[existingInc.GroupId] = existingInc
		} else {
			incident := Incident{
				Cluster:   string(clusterName),
				ClusterID: string(clusterID),
				GroupId:   string(groupId),
				Severity:  healthyVal.String(),
				StartTime: formatToRFC3339(startTime),
				EndTime:   formatToRFC3339(endTime),
				ComponentsSet: map[string]struct{}{
					component: {},
				},
				AffectedComponents: []string{component},
				Alerts:             []model.LabelSet{labels},
				AlertsSet: map[string]struct{}{
					labels.String(): {},
				},
			}
			if i.cfg.consoleURL != "" {
				incident.URL = fmt.Sprintf("%s/monitoring/incidents?groupId=%s", i.cfg.consoleURL, groupId)
			}
			incident.UpdateStatus()
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
func getAlertDataForIncidents(ctx context.Context, incidents map[string]Incident, silences []models.Alert, promAPI prom.Loader, qRange v1.Range) []Incident {
	alertData, err := promAPI.LoadVectorRange(ctx, `ALERTS{alertstate!="pending"}`, qRange.Start, qRange.End, qRange.Step)
	if err != nil {
		slog.Error("Failed to query firing alerts", "error", err)
		return nil
	}

	silencedAlertsMap := make(map[string][]models.Alert, len(silences))
	for _, silencedAlert := range silences {
		if alertname, ok := silencedAlert.Labels["alertname"]; ok {
			silencedAlertsMap[alertname] = append(silencedAlertsMap[alertname], silencedAlert)
		}
	}

	var alerts []model.LabelSet
	for i := range alertData {
		sample := alertData[i]
		metric := model.LabelSet(sample.Metric)
		firstSample := sample.Samples[0]
		lastSample := sample.Samples[len(sample.Samples)-1]
		startTime, endTime := processSampleTime(firstSample, lastSample, qRange)

		metric["start_time"] = model.LabelValue(formatToRFC3339(startTime))
		if !endTime.IsZero() {
			metric["end_time"] = model.LabelValue(formatToRFC3339(endTime))
			metric["alertstate"] = "resolved"
		} else {
			metric["alertstate"] = "firing"
		}
		alerts = append(alerts, metric)
	}

	var incidentsSlice []Incident
	for _, inc := range incidents {
		updatedAlertsMap := make(map[string]model.LabelSet, len(inc.Alerts))

		for _, alertInIncident := range inc.Alerts {
			subsetMatcher := common.LabelsSubsetMatcher{Labels: alertInIncident}
			for _, firingAlert := range alerts {
				// check for multicluster/ACM environment
				if inc.ClusterID != "" {
					clusterIDMatch := string(firingAlert["clusterID"]) == inc.ClusterID
					// if the alert cluster ID does not match incident cluster ID, skip
					if !clusterIDMatch {
						continue
					}
				}
				match, _ := subsetMatcher.Matches(firingAlert)
				if match {

					// the silencedAlertsMap is precomputed in order to contain all the silences grouped by alertname
					// [Alert1] = [{alertname="Alert1", namespace="foo"}, alertname="Alert1", namespace="bar"]
					alertname := string(firingAlert["alertname"])
					namespace := string(firingAlert["namespace"])
					severity := string(firingAlert["severity"])

					key := fmt.Sprintf("%s|%s|%s", alertname, namespace, severity)

					silenced := false
					if isAlertSilenced(firingAlert, silencedAlertsMap[alertname]) {
						silenced = true
					}

					updatedAlert := cleanupLabels(firingAlert)

					// If multiple alerts shares the same triple (alertname, namespace, severity) within
					// the same incident, these should be collapsed in a unique row. (same logic applied on server command)
					// The desired behaviour is to attach `silenced="true"` only if all colliding alerts are silenced, otherwise false.
					if _, f := updatedAlertsMap[key]; f {

						// if an alert, already labels cleaned, was already registered in the map
						// we should verify if it was marked as silenced
						lastSilenced, err := strconv.ParseBool(string(updatedAlertsMap[key]["silenced"]))
						if err != nil {
							slog.Error("failed to parse bool", "error", err)
							return nil
						}
						// the && operator allow us to get the following behaviour
						// if all are silenced the ending property will be true
						// if even just one is not silenced the ending property will be false
						updatedAlert["silenced"] = model.LabelValue(fmt.Sprintf("%t", lastSilenced && silenced))
						updatedAlertsMap[key] = updatedAlert
					} else {
						updatedAlert["silenced"] = model.LabelValue(fmt.Sprintf("%t", silenced))
						updatedAlertsMap[key] = updatedAlert
					}
				}
			}
		}

		inc.Alerts = slices.Collect(maps.Values(updatedAlertsMap))
		incidentsSlice = append(incidentsSlice, inc)
	}
	return incidentsSlice
}

// cleanupLabels removes and renames some of the
// labels from the set and returns new LabelSet
func cleanupLabels(m model.LabelSet) model.LabelSet {
	updatedLS := m.Clone()
	updatedLS["status"] = updatedLS["alertstate"]
	updatedLS["name"] = updatedLS["alertname"]
	if clusterID := updatedLS["clusterID"]; clusterID != "" {
		updatedLS["cluster_id"] = clusterID
	}
	delete(updatedLS, "__name__")
	delete(updatedLS, "prometheus")
	delete(updatedLS, "alertstate")
	delete(updatedLS, "alertname")
	delete(updatedLS, "pod")
	delete(updatedLS, "clusterID")
	return updatedLS
}

// getConsoleURL tries to read consoleURL from the "cluster" consoles.config.openshift.io
// resource
func getConsoleURL() (string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", err
	}
	cli, err := dynamic.NewForConfig(config)
	if err != nil {
		return "", err
	}

	unstConsole, err := cli.Resource(
		schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "consoles"}).
		Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	consoleURL, ok, err := unstructured.NestedString(unstConsole.Object, "status", "consoleURL")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("cannot find consoleURL attribute in the 'cluster' console.config.openshift.io resource")
	}

	return consoleURL, nil
}

func defaultPrometheusLoader(promURL, token string) (prom.Loader, error) {
	return prom.NewLoaderWithToken(promURL, token)
}

func defaultAlertManagerLoader(alertManagerURL, token string) (alertmanager.Loader, error) {
	return alertmanager.NewLoader(alertmanager.LoaderConfig{
		AlertManagerURL: alertManagerURL,
		Token:           token,
	})
}

func isAlertSilenced(alert model.LabelSet, silences []models.Alert) bool {
	// The labels defined in an Alertmanager silence are an intersection of the labels on an alert
	// If all the common labels by alert and the silence matches we can assume that alert is silenced

	for _, silence := range silences {
		if silence.Labels == nil {
			continue
		}

		// Convert silence labels to model.LabelSet
		silenceLabels := make(model.LabelSet)
		for silenceLabel, silenceValue := range silence.Labels {
			silenceLabels[model.LabelName(silenceLabel)] = model.LabelValue(silenceValue)
		}

		// Use LabelsIntersectionMatcher to check if silence labels match the alert
		matcher := common.LabelsIntersectionMatcher{Labels: silenceLabels}
		match, _ := matcher.Matches(alert)
		if match {
			return true
		}
	}

	return false
}

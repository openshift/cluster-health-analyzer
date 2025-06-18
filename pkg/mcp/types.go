package mcp

import "github.com/prometheus/common/model"

type Response struct {
	Incidents Incidents `json:"incidents"`
}

type Incidents struct {
	Total     int        `json:"total"`
	Incidents []Incident `json:"items"`
}

type Incident struct {
	GroupId   string `json:"id"`
	Severity  string `json:"severity"`
	StartTime string `json:"start_time"`
	Status    string `json:"status"`
	EndTime   string `json:"end_time"`

	// TODO
	URL                string         `json:"-"`
	Alerts             []model.Metric `json:"alerts"`
	AffectedComponents []string       `json:"affected_components"`
}

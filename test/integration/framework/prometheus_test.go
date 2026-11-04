package framework

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/common/model"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
)

type groupingCleanupLoader struct {
	prom.Loader
	query  string
	result []model.LabelSet
	err    error
}

func (l *groupingCleanupLoader) LoadQuery(_ context.Context, query string, _ time.Time) ([]model.LabelSet, error) {
	l.query = query
	return l.result, l.err
}

func TestHasGroupingData(t *testing.T) {
	queryErr := errors.New("query failed")
	for _, tc := range []struct {
		name   string
		result []model.LabelSet
		err    error
		want   bool
	}{
		{name: "empty"},
		{name: "pending alert", result: []model.LabelSet{{"__name__": "ALERTS", "alertstate": "pending"}}, want: true},
		{name: "firing alert", result: []model.LabelSet{{"__name__": "ALERTS", "alertstate": "firing"}}, want: true},
		{name: "scenario clock", result: []model.LabelSet{{"__name__": "cha_integration_start_seconds"}}, want: true},
		{name: "CHA metric", result: []model.LabelSet{{"__name__": "cluster_health_components_map"}}, want: true},
		{name: "query failure", err: queryErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loader := &groupingCleanupLoader{result: tc.result, err: tc.err}
			client := PrometheusClient{loader: loader}
			got, err := client.HasGroupingData(context.Background(), "^AlertTest123$", "123")
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("got present=%t error=%v, want present=%t error=%v", got, err, tc.want, tc.err)
			}
			wantQuery := `ALERTS{alertname=~"^AlertTest123$",test_run="123"}` +
				` or cluster_health_components_map{src_alertname=~"^AlertTest123$"}` +
				` or cha_integration_start_seconds{test_run="123"}`
			if loader.query != wantQuery {
				t.Fatalf("cleanup query does not cover all run-scoped data:\n%s", loader.query)
			}
		})
	}
}

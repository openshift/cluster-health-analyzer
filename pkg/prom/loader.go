package prom

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type loader struct {
	api v1.API
}

type Loader struct {
	*loader
}

func NewLoader(prometheusURL string) (*Loader, error) {
	promClient, err := NewPrometheusClient(prometheusURL)
	if err != nil {
		return nil, err
	}

	return &Loader{
		&loader{
			api: v1.NewAPI(promClient),
		},
	}, nil
}

func (c *loader) LoadAlerts(ctx context.Context, t time.Time) ([]model.LabelSet, error) {
	result, _, err := c.api.Query(ctx, `ALERTS{alertstate="firing"}`, t)
	if err != nil {
		return nil, err
	}
	vect := result.(model.Vector)
	var ret = make([]model.LabelSet, len(vect))
	for i, sample := range vect {
		alert := make(model.LabelSet, len(sample.Metric))
		for k, v := range sample.Metric {
			alert[k] = v
		}
		ret[i] = alert
	}
	return ret, nil

}

func (c *loader) LoadAlertsRange(ctx context.Context, start, end time.Time, step time.Duration) (RangeVector, error) {
	result, _, err := c.api.QueryRange(ctx, `ALERTS{alertstate="firing"}`, v1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})
	if err != nil {
		return nil, err
	}
	matrix := result.(model.Matrix)
	ret := make(RangeVector, len(matrix))
	for i, samples := range matrix {
		alert := make(model.LabelSet, len(samples.Metric))
		for k, v := range samples.Metric {
			alert[k] = v
		}
		ret[i] = Range{
			Metric:  alert,
			Samples: samples.Values,
			Step:    step,
		}
	}
	return ret, nil
}

func (c *loader) LoadVectorRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (RangeVector, error) {
	result, _, err := c.api.QueryRange(ctx, query, v1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})
	if err != nil {
		return nil, err
	}
	matrix := result.(model.Matrix)
	ret := make(RangeVector, len(matrix))
	for i, samples := range matrix {
		labels := make(model.LabelSet, len(samples.Metric))
		for k, v := range samples.Metric {
			labels[k] = v
		}
		ret[i] = Range{
			Metric:  labels,
			Samples: samples.Values,
			Step:    step,
		}
	}
	return ret, nil
}

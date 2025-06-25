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

func (c *loader) LoadAlerts(ctx context.Context, t time.Time) ([]Alert, error) {
	result, _, err := c.api.Query(ctx, `ALERTS{alertstate="firing"}`, t)
	if err != nil {
		return nil, err
	}
	vect := result.(model.Vector)
	var ret = make([]Alert, len(vect))
	for i, sample := range vect {
		labels := make(map[string]string, len(sample.Metric))
		for k, v := range sample.Metric {
			labels[string(k)] = string(v)
		}
		alert := Alert{
			Name:   string(sample.Metric["alertname"]),
			Labels: labels,
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
		labels := make(map[string]string, len(samples.Metric))
		for k, v := range samples.Metric {
			labels[string(k)] = string(v)
		}
		alert := Alert{
			Name:   string(samples.Metric["alertname"]),
			Labels: labels,
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
		labels := make(map[string]string, len(samples.Metric))
		for k, v := range samples.Metric {
			labels[string(k)] = string(v)
		}
		labelSet := LabelSet{
			Labels: labels,
		}
		ret[i] = Range{
			Metric:  labelSet,
			Samples: samples.Values,
			Step:    step,
		}
	}
	return ret, nil
}

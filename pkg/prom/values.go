package prom

import (
	"fmt"
	"math"
	"time"

	"github.com/prometheus/common/model"
)

// TODO: Replace with LabelSet and get rid of Alerts struct
//   - it's unnecessary complex.
type PromMetric interface {
	MLabels() map[string]string
}

type LabelSet struct {
	Labels map[string]string
}

func (ls LabelSet) MLabels() map[string]string {
	return ls.Labels
}

func PromMetricToString(m PromMetric) string {
	ret := fmt.Sprintf("%s{", m.MLabels()["__name__"])
	first := true
	for k, v := range m.MLabels() {
		if k == "__name__" {
			continue
		}
		if first {
			first = false
		} else {
			ret += ", "
		}

		ret += k + `="` + v + `"`
	}
	ret += "}"
	return ret
}

type Alert struct {
	Name   string
	Labels map[string]string
}

func (a Alert) MLabels() map[string]string {
	return a.Labels
}

func (a Alert) String() string {
	return PromMetricToString(a)
}

type Range struct {
	Metric  PromMetric
	Samples []model.SamplePair
	Step    time.Duration
}

type RangeVector []Range

// Matrix is a dense matrix of time series aligned by time.
type Matrix struct {
	Metrics []PromMetric
	Values  [][]model.SampleValue
	Start   model.Time
	End     model.Time
	Step    time.Duration
}

func (v RangeVector) MinTime() model.Time {
	if len(v) == 0 {
		return model.Time(0)
	}
	min := v[0].Samples[0].Timestamp
	for _, r := range v {
		if r.Samples[0].Timestamp < min {
			min = r.Samples[0].Timestamp
		}
	}
	return min
}

func (v RangeVector) MaxTime() model.Time {
	if len(v) == 0 {
		return model.Time(0)
	}
	max := v[0].Samples[len(v[0].Samples)-1].Timestamp
	for _, r := range v {
		if r.Samples[len(r.Samples)-1].Timestamp > max {
			max = r.Samples[len(r.Samples)-1].Timestamp
		}
	}
	return max
}

// Expand converts a RangeVector to a dense Matrix.
//
// The dense matrix is quite an expensive structure. We initially used it
// for searching for alerts changes, but we used interval-based approach
// instead. We keep this function for possible future use.
func (v RangeVector) Expand() Matrix {
	if len(v) == 0 {
		return Matrix{}
	}
	start := v.MinTime()
	end := v.MaxTime()
	step := v[0].Step // Expecting all steps to be the same.

	stepMs := int64(step / time.Millisecond)
	nSteps := (int64(end) - int64(start)) / stepMs

	ret := Matrix{
		Metrics: make([]PromMetric, len(v)),
		Values:  make([][]model.SampleValue, len(v)),
		Start:   start,
		End:     end,
		Step:    step,
	}

	for i, r := range v {
		ret.Metrics[i] = r.Metric
		ret.Values[i] = make([]model.SampleValue, nSteps)
	}

	// Iterate over individual ranges and fill in the values.
	for i, r := range v {
		time := start
		cur := 0

		values := ret.Values[i]

		// The matrix is dense, so we can just iterate over the values and fill in.
		for s := range values {
			values[s] = model.SampleValue(math.NaN())

			// If we have a sample at this time, fill it in. Otherwise, keep the NaN.
			for cur < len(r.Samples) && r.Samples[cur].Timestamp <= time {
				values[s] = r.Samples[cur].Value
				cur++
			}

			// Move to the next time step.
			time = time.Add(step)
		}
	}
	return ret
}

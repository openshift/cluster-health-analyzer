package common

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func TestLabelsMatcher_Matches(t *testing.T) {
	type args struct {
		labels model.LabelSet
	}

	tests := []struct {
		name          string
		matcher       LabelsMatcher
		args          args
		expectedMatch bool
	}{
		{
			name: "LabelsSubsetMatcher matches",
			matcher: LabelsSubsetMatcher{
				Labels: model.LabelSet{
					"first": "foo",
				},
			},
			args: args{
				labels: model.LabelSet{
					"first":  "foo",
					"second": "bar",
				},
			},
			expectedMatch: true,
		},
		{
			name: "LabelsSubsetMatcher do not match",
			matcher: &LabelsSubsetMatcher{
				Labels: model.LabelSet{
					"first": "foo",
					"third": "oof",
				},
			},
			args: args{
				labels: model.LabelSet{
					"first":  "foo",
					"second": "bar",
				},
			},
			// this shouldn't match because third is not contained in the superset
			expectedMatch: false,
		},
		{
			name: "LabelsIntersectionMatcher matches",
			matcher: &LabelsIntersectionMatcher{
				Labels: model.LabelSet{
					"first": "foo",
					"third": "oof",
				},
			},
			args: args{
				labels: model.LabelSet{
					"first":  "foo",
					"second": "bar",
				},
			},
			expectedMatch: true,
		},
		{
			name: "LabelsIntersectionMatcher do not match",
			matcher: &LabelsIntersectionMatcher{
				Labels: model.LabelSet{
					"first": "oof",
				},
			},
			args: args{
				labels: model.LabelSet{
					"first":  "foo",
					"second": "bar",
				},
			},
			// this shouldn't match because oof != foo
			expectedMatch: false,
		},
		{
			name: "LabelsIntersectionMatcher do not match - no intersection",
			matcher: &LabelsIntersectionMatcher{
				Labels: model.LabelSet{
					"first": "foo",
				},
			},
			args: args{
				labels: model.LabelSet{
					"second": "bar",
				},
			},
			expectedMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, _ := tt.matcher.Matches(tt.args.labels)
			assert.Equal(t, tt.expectedMatch, match)
		})
	}

}

func TestLabelsMatcher_Equals(t *testing.T) {
	type args struct {
		otherMatcher LabelsMatcher
	}

	tests := []struct {
		name           string
		matcher        LabelsMatcher
		args           args
		expectedEquals bool
	}{
		{
			name: "LabelsSubsetMatcher Equals ok",
			matcher: LabelsSubsetMatcher{
				Labels: model.LabelSet{
					"first": "foo",
				},
			},
			args: args{
				otherMatcher: LabelsSubsetMatcher{
					Labels: model.LabelSet{
						"first": "foo",
					},
				},
			},
			expectedEquals: true,
		},
		{
			name: "LabelsSubsetMatcher Equals ko - different length",
			matcher: LabelsSubsetMatcher{
				Labels: model.LabelSet{
					"first": "foo",
				},
			},
			args: args{
				otherMatcher: LabelsSubsetMatcher{
					Labels: model.LabelSet{
						"first":  "foo",
						"second": "bar",
					},
				},
			},
			expectedEquals: false,
		},
		{
			name: "LabelsSubsetMatcher Equals ko - same length",
			matcher: LabelsSubsetMatcher{
				Labels: model.LabelSet{
					"first":  "foo",
					"second": "foo",
				},
			},
			args: args{
				otherMatcher: LabelsSubsetMatcher{
					Labels: model.LabelSet{
						"first":  "foo",
						"second": "bar",
					},
				},
			},
			expectedEquals: false,
		},
		{
			name: "LabelsIntersectionMatcher Equals ok",
			matcher: LabelsIntersectionMatcher{
				Labels: model.LabelSet{
					"first":  "foo",
					"second": "bar",
				},
			},
			args: args{
				otherMatcher: LabelsIntersectionMatcher{
					Labels: model.LabelSet{
						"first":  "foo",
						"second": "bar",
					},
				},
			},
			expectedEquals: true,
		},
		{
			name: "LabelsIntersectionMatcher Equals ko - different length",
			matcher: LabelsIntersectionMatcher{
				Labels: model.LabelSet{
					"first": "foo",
				},
			},
			args: args{
				otherMatcher: LabelsIntersectionMatcher{
					Labels: model.LabelSet{
						"first":  "foo",
						"second": "bar",
					},
				},
			},
			expectedEquals: false,
		},
		{
			name: "LabelsIntersectionMatcher Equals ko - same length",
			matcher: LabelsIntersectionMatcher{
				Labels: model.LabelSet{
					"first":  "foo",
					"second": "foo",
				},
			},
			args: args{
				otherMatcher: LabelsIntersectionMatcher{
					Labels: model.LabelSet{
						"first":  "foo",
						"second": "bar",
					},
				},
			},
			expectedEquals: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := tt.matcher.Equals(tt.args.otherMatcher)
			assert.Equal(t, tt.expectedEquals, match)
		})
	}

}

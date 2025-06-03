package health

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildLabelExpressions(t *testing.T) {
	tests := []struct {
		name                string
		selectors           []Selector
		expectedExpressions []string
	}{
		{
			name: "One matchLabels attribute with two simple labels",
			selectors: []Selector{
				{
					MatchLabels: map[string][]string{
						"app": {"test-1"},
						"env": {"testing"},
					},
				},
			},
			expectedExpressions: []string{"app=test-1,env=testing"},
		},
		{
			name: "One matchLabels attribute with two key labels",
			selectors: []Selector{
				{
					MatchLabels: map[string][]string{
						"app": {},
						"env": {},
					},
				},
			},
			expectedExpressions: []string{"app,env"},
		},
		{
			name: "One matchLabels attribute with two labels",
			selectors: []Selector{
				{
					MatchLabels: map[string][]string{
						"app": {"test-1"},
						"env": {"qe", "staging", "prod"},
					},
				},
			},
			expectedExpressions: []string{"app=test-1,env in (qe,staging,prod)"},
		},
		{
			name: "Two matchLabels with only key labels",
			selectors: []Selector{
				{
					MatchLabels: map[string][]string{
						"app": {},
					},
				},
				{
					MatchLabels: map[string][]string{
						"env": {},
					},
				},
			},
			expectedExpressions: []string{"app", "env"},
		},
		{
			name: "Two matchLabels with only key labels",
			selectors: []Selector{
				{
					MatchLabels: map[string][]string{
						"app": {"test-1"},
					},
				},
				{
					MatchLabels: map[string][]string{
						"env": {"qe", "staging", "prod"},
					},
				},
			},
			expectedExpressions: []string{"app=test-1", "env in (qe,staging,prod)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expressions := buildLabelExpressions(tt.selectors)
			assert.Equal(t, tt.expectedExpressions, expressions)
		})
	}
}

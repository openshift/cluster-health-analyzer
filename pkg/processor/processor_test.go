package processor

import (
	"errors"
	"testing"

	"github.com/openshift/cluster-health-analyzer/pkg/test/mocks"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func Test_evaluateSilences(t *testing.T) {
	type args struct {
		alerts []model.LabelSet
	}

	tests := []struct {
		name            string
		args            args
		silenced        []models.Alert
		alertManagerErr error
		expected        []model.LabelSet
		wantErr         error
	}{
		{
			name: "happy path",
			args: args{
				alerts: []model.LabelSet{
					{
						"alertname": "KubeNodeNotReady",
						"namespace": "openshift-monitoring",
					},
					{
						"alertname": "KubePodCrashLooping",
						"namespace": "openshift-etcd",
					},
				},
			},
			silenced: []models.Alert{
				{
					Labels: map[string]string{
						"alertname": "KubePodCrashLooping",
					},
				},
			},
			expected: []model.LabelSet{
				{
					"alertname": "KubeNodeNotReady",
					"namespace": "openshift-monitoring",
					"silenced":  "0",
				},
				{
					"alertname": "KubePodCrashLooping",
					"namespace": "openshift-etcd",
					"silenced":  "1",
				},
			},
			wantErr: nil,
		},
		{
			name: "unhappy path - alert manager client gets an error",
			args: args{
				alerts: []model.LabelSet{
					{
						"alertname": "KubeNodeNotReady",
						"namespace": "openshift-monitoring",
					},
					{
						"alertname": "KubePodCrashLooping",
						"namespace": "openshift-etcd",
					},
				},
			},
			alertManagerErr: errors.New("alertmanager error"),
			wantErr:         errors.New("alertmanager error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testProcessor := processor{
				amLoader: mocks.NewMockAlertLoader(nil, tt.silenced, tt.alertManagerErr),
			}
			got, err := testProcessor.evaluateSilences(tt.args.alerts)
			assert.Equal(t, tt.expected, got)
			assert.Equal(t, tt.wantErr, err)
		})
	}

}

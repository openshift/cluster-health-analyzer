package mcp_test

import (
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	analyzer "github.com/openshift/cluster-health-analyzer/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestNewIncidentLoadersRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name            string
		config          *rest.Config
		prometheusURL   string
		alertmanagerURL string
		wantError       string
	}{
		{
			name:      "missing REST config",
			wantError: "no REST config available",
		},
		{
			name:            "HTTP Prometheus",
			config:          &rest.Config{},
			prometheusURL:   "http://prometheus.example.test",
			alertmanagerURL: "https://alertmanager.example.test",
			wantError:       "prometheus transport: backend URL",
		},
		{
			name:            "HTTP Alertmanager",
			config:          &rest.Config{},
			prometheusURL:   "https://prometheus.example.test",
			alertmanagerURL: "http://alertmanager.example.test",
			wantError:       "alertmanager transport: backend URL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			promLoader, alertLoader, err := analyzer.NewIncidentLoaders(
				tt.config, tt.prometheusURL, tt.alertmanagerURL, "",
			)
			require.ErrorContains(t, err, tt.wantError)
			assert.Nil(t, promLoader)
			assert.Nil(t, alertLoader)
		})
	}
}

func TestNewIncidentLoadersUsesRESTCredentialsAndBackendCA(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer per-call-token", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Impersonate-User"))
		w.Header().Set("Content-Type", "application/json")
		var body string
		switch r.URL.Path {
		case "/api/v1/query":
			body = `{"status":"success","data":{"resultType":"vector","result":[]}}`
		case "/api/v2/alerts":
			body = `[]`
		default:
			http.NotFound(w, r)
			return
		}
		_, err := fmt.Fprint(w, body)
		assert.NoError(t, err)
	}))
	defer backend.Close()

	caFile := filepath.Join(t.TempDir(), "service-ca.crt")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: backend.Certificate().Raw,
	}), 0o600))

	config := &rest.Config{
		Host:        "https://kubernetes.example.test",
		BearerToken: "per-call-token",
		TLSClientConfig: rest.TLSClientConfig{
			CAData: []byte("invalid Kubernetes API CA"),
		},
		Impersonate: rest.ImpersonationConfig{UserName: "test-user"},
	}
	original := rest.CopyConfig(config)
	promLoader, alertLoader, err := analyzer.NewIncidentLoaders(
		config, backend.URL, backend.URL, caFile,
	)
	require.NoError(t, err)

	_, err = promLoader.LoadQuery(t.Context(), "up", time.Now())
	require.NoError(t, err)
	_, err = alertLoader.SilencedAlerts()
	require.NoError(t, err)
	assert.Equal(t, original, config, "the caller's REST config must remain unchanged")
}

package incidents

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/config"
	"k8s.io/client-go/rest"
)

type testBackendConfig struct {
	PrometheusURL   string `toml:"prometheus_url"`
	AlertmanagerURL string `toml:"alertmanager_url"`
}

func (*testBackendConfig) Validate() error {
	return nil
}

func init() {
	config.RegisterToolsetConfig(metricsToolsetName, func(_ context.Context, primitive toml.Primitive, md toml.MetaData) (api.ExtendedConfig, error) {
		var cfg testBackendConfig
		if err := md.PrimitiveDecode(primitive, &cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	})
}

type testRequest struct {
	api.ToolCallRequest
	arguments map[string]any
}

func (r testRequest) GetArguments() map[string]any {
	return r.arguments
}

type testKubernetesClient struct {
	api.KubernetesClient
	caData []byte
}

func (c testKubernetesClient) RESTConfig() *rest.Config {
	return &rest.Config{
		BearerToken: "per-call-token",
		TLSClientConfig: rest.TLSClientConfig{
			CAData: c.caData,
		},
	}
}

func TestGetIncidentsRequiresBackendURLs(t *testing.T) {
	result, err := handleGetIncidents(api.ToolHandlerParams{
		Context:    context.Background(),
		BaseConfig: config.BaseDefault(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == nil {
		t.Fatal("expected an error when metrics backend URLs are not configured")
	}
}

func TestGetIncidentsBackendConfigSelection(t *testing.T) {
	tests := []struct {
		name       string
		configTOML string
		wantError  string
	}{
		{
			name: "incidents config when metrics is absent",
			configTOML: `
				[toolset_configs."observability/incidents"]
				prometheus_url = "http://incidents.example.test"
				alertmanager_url = "https://incidents.example.test"
			`,
			wantError: "http://incidents.example.test",
		},
		{
			name: "metrics config takes precedence",
			configTOML: `
				[toolset_configs."observability/metrics"]
				prometheus_url = "http://metrics.example.test"
				alertmanager_url = "https://metrics.example.test"
				[toolset_configs."observability/incidents"]
				prometheus_url = "http://incidents.example.test"
				alertmanager_url = "https://incidents.example.test"
			`,
			wantError: "http://metrics.example.test",
		},
		{
			name: "incomplete metrics config falls back to incidents",
			configTOML: `
				[toolset_configs."observability/metrics"]
				prometheus_url = "http://metrics.example.test"
				[toolset_configs."observability/incidents"]
				prometheus_url = "http://incidents.example.test"
				alertmanager_url = "https://incidents.example.test"
			`,
			wantError: "http://incidents.example.test",
		},
		{
			name: "empty incidents config is an error",
			configTOML: `
				[toolset_configs."observability/incidents"]
				prometheus_url = ""
				alertmanager_url = ""
			`,
			wantError: "requires prometheus_url and alertmanager_url",
		},
		{
			name: "partial incidents config is an error",
			configTOML: `
				[toolset_configs."observability/incidents"]
				prometheus_url = "https://incidents.example.test"
			`,
			wantError: "requires prometheus_url and alertmanager_url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.ReadToml([]byte(tt.configTOML))
			if err != nil {
				t.Fatal(err)
			}
			result, err := handleGetIncidents(api.ToolHandlerParams{
				Context:          context.Background(),
				BaseConfig:       cfg,
				KubernetesClient: testKubernetesClient{},
				ToolCallRequest:  testRequest{},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Error == nil || !strings.Contains(result.Error.Error(), tt.wantError) {
				t.Fatalf("error = %v, want it to contain %q", result.Error, tt.wantError)
			}
		})
	}
}

func TestGetIncidentsUsesServiceCAAndRESTConfigToken(t *testing.T) {
	requests := make(chan string, 1)
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	caFile := filepath.Join(t.TempDir(), "service-ca.crt")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: backend.Certificate().Raw,
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ReadToml([]byte(fmt.Sprintf(`
		[toolset_configs."observability/metrics"]
		prometheus_url = %q
		alertmanager_url = %q
	`, backend.URL, backend.URL)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := handleGetIncidentsWithCAFile(api.ToolHandlerParams{
		Context:          context.Background(),
		BaseConfig:       cfg,
		KubernetesClient: testKubernetesClient{caData: []byte("invalid Kubernetes CA")},
		ToolCallRequest:  testRequest{},
	}, caFile)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == nil {
		t.Fatal("expected the mock backend to return an error")
	}
	select {
	case authorization := <-requests:
		if authorization != "Bearer per-call-token" {
			t.Fatalf("authorization = %q, want %q", authorization, "Bearer per-call-token")
		}
	default:
		t.Fatal("the Prometheus backend received no request")
	}
}

func TestBackendTransportRequiresHTTPS(t *testing.T) {
	_, err := backendTransport(&rest.Config{}, "http://example.com", serviceCAFile)
	if err == nil {
		t.Fatal("expected HTTP backend URL to be rejected")
	}
}

func TestParseGetIncidentsArgs(t *testing.T) {
	tests := []struct {
		name         string
		arguments    map[string]any
		wantHours    float64
		wantSeverity string
		wantError    string
	}{
		{name: "defaults", wantHours: 360, wantSeverity: "warning"},
		{name: "fractional hours", arguments: map[string]any{"time_range": 1.5}, wantHours: 1.5, wantSeverity: "warning"},
		{name: "case-insensitive severity", arguments: map[string]any{"min_severity": "CRITICAL"}, wantHours: 360, wantSeverity: "CRITICAL"},
		{name: "below minimum", arguments: map[string]any{"time_range": 0.5}, wantError: "time_range must be between"},
		{name: "above maximum", arguments: map[string]any{"time_range": 360.5}, wantError: "time_range must be between"},
		{name: "severity suffix", arguments: map[string]any{"min_severity": "notwarning"}, wantError: "min_severity must be"},
		{name: "empty severity", arguments: map[string]any{"min_severity": ""}, wantError: "min_severity must be"},
		{name: "wrong time type", arguments: map[string]any{"time_range": "1.5"}, wantError: "time_range must be a number"},
		{name: "wrong severity type", arguments: map[string]any{"min_severity": 1.0}, wantError: "min_severity must be a string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGetIncidentsArgs(api.ToolHandlerParams{
				ToolCallRequest: testRequest{arguments: tt.arguments},
			})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.TimeRange != tt.wantHours || got.MinSeverity != tt.wantSeverity {
				t.Fatalf("params = %+v, want hours %v and severity %q", got, tt.wantHours, tt.wantSeverity)
			}
		})
	}
}

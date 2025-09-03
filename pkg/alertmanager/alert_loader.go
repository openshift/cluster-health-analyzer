package alertmanager

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	runtimeclient "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/prometheus/alertmanager/api/v2/client"
	"github.com/prometheus/alertmanager/api/v2/client/alert"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/client_golang/api"
	prom_config "github.com/prometheus/common/config"
)

// Loader reads alerts from the Alertmanager API
type Loader interface {
	// ActiveAlert reads the active alerts from the Alertmanager
	// and returns them as a slice.
	ActiveAlerts() ([]models.Alert, error)
	// ActiveAlert reads the active alerts with the provided labels from the Alertmanager
	// and returns them as a slice.
	ActiveAlertsWithLabels(labels []string) ([]models.Alert, error)
	// SilencedAlerts reads silenced alerts from the Alertmanager
	// and returns them as a slice
	SilencedAlerts() ([]models.Alert, error)
}

type loader struct {
	cli *client.AlertmanagerAPI
}

// NewLoader creates a new client Alermanager API
func NewLoader(alertManagerURL string) (Loader, error) {
	amURL, err := url.Parse(alertManagerURL)
	if err != nil {
		return nil, err
	}
	useTls := strings.HasPrefix(alertManagerURL, "https://")

	runtime := runtimeclient.New(amURL.Host, path.Join(amURL.Path, "/api/v2"), []string{amURL.Scheme})
	if useTls {
		token, err := readTokenFromFile()
		if err != nil {
			return nil, err
		}

		certs, err := createCertPool()
		if err != nil {
			return nil, err
		}
		defaultRt := api.DefaultRoundTripper.(*http.Transport)
		defaultRt.TLSClientConfig = &tls.Config{RootCAs: certs}

		runtime.Transport = prom_config.NewAuthorizationCredentialsRoundTripper(
			"Bearer", prom_config.NewInlineSecret(string(token)), defaultRt)
		return &loader{
			cli: client.New(runtime, strfmt.Default),
		}, nil
	}
	return &loader{cli: client.New(runtime, strfmt.Default)}, nil
}

// ActiveAlert reads the active alerts from the Alertmanager
// and returns them as a slice.
func (l *loader) ActiveAlerts() ([]models.Alert, error) {
	return l.loadAlerts(true, false, nil)
}

// ActiveAlert reads the active alerts with the provided labels from the Alertmanager
// and returns them as a slice.
func (l *loader) ActiveAlertsWithLabels(labels []string) ([]models.Alert, error) {
	return l.loadAlerts(true, false, labels)
}

// SilencedAlerts reads silenced alerts from the Alertmanager
// and returns them as a slice
func (l *loader) SilencedAlerts() ([]models.Alert, error) {
	return l.loadAlerts(false, true, nil)
}

// loadAlerts queries the alertmanager with the provided parameters
func (l *loader) loadAlerts(active, silenced bool, labels []string) ([]models.Alert, error) {
	params := alert.NewGetAlertsParams().
		WithActive(&active).
		WithSilenced(&silenced).
		WithFilter(labels)

	alertsOK, err := l.cli.Alert.GetAlerts(params)
	if err != nil {
		return nil, err
	}
	var alerts []models.Alert
	for _, gettableAlert := range alertsOK.Payload {
		alerts = append(alerts, gettableAlert.Alert)
	}

	return alerts, nil
}

func createCertPool() (*x509.CertPool, error) {
	certs := x509.NewCertPool()

	pemData, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt")
	if err != nil {
		slog.Error("Failed to read the CA certificate", "err", err)
		return nil, err
	}
	certs.AppendCertsFromPEM(pemData)
	return certs, nil
}

func readTokenFromFile() ([]byte, error) {
	return os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
}

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
)

type startupTestServer struct {
	started bool
	handled bool
}

func (s *startupTestServer) Handle(_ string, _ http.Handler) { s.handled = true }
func (s *startupTestServer) Start(_ context.Context) error {
	s.started = true
	return nil
}

func TestStartServerFailsBeforeServingWithoutRules(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("watch") == "true" {
			w.WriteHeader(http.StatusOK)
			if r.URL.Query().Get("sendInitialEvents") == "true" {
				_, err := fmt.Fprintln(w, `{"type":"BOOKMARK","object":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"resourceVersion":"1","name":"cluster-health-analyzer-config","annotations":{"k8s.io/initial-events-end":"true"}}}}`)
				if err != nil {
					t.Error(err)
				}
			}
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		_, err := fmt.Fprint(w, `{"apiVersion":"v1","kind":"ConfigMapList","metadata":{"resourceVersion":"1"},"items":[]}`)
		if err != nil {
			t.Error(err)
		}
	}))
	defer api.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"test": {Server: api.URL}},
		Contexts:       map[string]*clientcmdapi.Context{"test": {Cluster: "test", AuthInfo: "test"}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"test": {}},
		CurrentContext: "test",
	}
	require.NoError(t, clientcmd.WriteToFile(config, path))
	server := &startupTestServer{}
	err := StartServer(time.Minute, server, common.Options{Kubeconfig: path, DisableComponentsHealth: true})
	require.ErrorContains(t, err, "no valid IncidentRules")
	require.False(t, server.started)
	require.False(t, server.handled)
}

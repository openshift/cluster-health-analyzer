package serve

import (
	"context"
	"crypto/tls"
	"net/http"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/config/configdefaults"
	"github.com/openshift/library-go/pkg/config/serving"
	genericapiserver "k8s.io/apiserver/pkg/server"
	utilversion "k8s.io/apiserver/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
	"github.com/openshift/cluster-health-analyzer/pkg/server"
)

// APIServer is an implementation of server.Server interfac
// using genericapiserver.GenericAPIServer.
//
// It leverages the kube-apiserver library to provide a generic API server
// with built-in authentication, authorization and secure serving.
type APIServer struct {
	*genericapiserver.GenericAPIServer
}

func (s APIServer) Handle(pattern string, handler http.Handler) {
	s.Handler.NonGoRestfulMux.Handle(pattern, handler)
}

func (s APIServer) Start(ctx context.Context) error {
	return s.PrepareRun().RunWithContext(ctx)
}

func buildServer(o common.Options) (server.Server, error) {
	config, err := buildServerConfig(o)
	if err != nil {
		return nil, err
	}

	genericServer, err := config.Complete(nil).New("cluster-health-analyzer",
		genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	return APIServer{genericServer}, nil
}

// buildServerConfig creates a minimal genericapiserver.Config object
// for the provided options.
//
// Its main responsibility is to configure secure serving and
// authentication/authorization.  and fulfill the minimum requirements for a
// generic API server.
func buildServerConfig(o common.Options) (*genericapiserver.Config, error) {
	// We need kubeClient only when authentication/authorization is enabled.
	var kubeClient *kubernetes.Clientset

	if !o.DisableAuthForTesting {
		kubeConfig, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
		if err != nil {
			return nil, err
		}

		kubeClient, err = kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			return nil, err
		}
	}

	servingInfo := configv1.HTTPServingInfo{}
	configdefaults.SetRecommendedHTTPServingInfoDefaults(&servingInfo)

	servingInfo.CertFile = o.CertFile
	servingInfo.KeyFile = o.CertKey
	// Don't set a CA file for client certificates because the CA is read from
	// the kube-system/extension-apiserver-authentication ConfigMap.
	servingInfo.ClientCA = ""

	serverConfig, err := serving.ToServerConfig(
		context.Background(),
		servingInfo,
		operatorv1alpha1.DelegatedAuthentication{Disabled: o.DisableAuthForTesting},
		operatorv1alpha1.DelegatedAuthorization{Disabled: o.DisableAuthForTesting},
		o.Kubeconfig,
		kubeClient,
		nil,   // disable leader election
		false, // disable http2
	)
	if err != nil {
		return nil, err
	}

	// Set the effective version to avoid panics in the API server.
	serverConfig.EffectiveVersion = utilversion.DefaultKubeEffectiveVersion()

	// We will be serving out own `/metrics` endpoint.
	serverConfig.EnableMetrics = false
	// use only the secured cipher suites
	serverConfig.SecureServing.CipherSuites = getCipherSuites()

	return serverConfig, nil
}

func getCipherSuites() []uint16 {
	secureCiphers := tls.CipherSuites()
	cipherSuites := make([]uint16, len(secureCiphers))
	for i, cipher := range secureCiphers {
		cipherSuites[i] = cipher.ID
	}
	return cipherSuites
}

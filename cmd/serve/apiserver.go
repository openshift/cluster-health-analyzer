package serve

import (
	"context"
	"fmt"

	"net"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilversion "k8s.io/apiserver/pkg/util/version"

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

func buildServer(o options) (server.Server, error) {
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
// It's main responsibility is to configure secure serving and
// authentication/authorization.  and fulfill the minimum requirements for a
// generic API server.
func buildServerConfig(o options) (*genericapiserver.Config, error) {
	err := o.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil,
		[]net.IP{net.ParseIP("127.0.0.1")})
	if err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	serverConfig := genericapiserver.NewConfig(codecs)
	if err := o.SecureServing.ApplyTo(&serverConfig.SecureServing, &serverConfig.LoopbackClientConfig); err != nil {
		return nil, err
	}

	if !o.DisableAuthForTesting {
		authNOpts := genericoptions.NewDelegatingAuthenticationOptions()
		authNOpts.RemoteKubeConfigFile = o.Kubeconfig

		authZOpts := genericoptions.NewDelegatingAuthorizationOptions()
		authZOpts.RemoteKubeConfigFile = o.Kubeconfig

		if err := authNOpts.ApplyTo(&serverConfig.Authentication, serverConfig.SecureServing, nil); err != nil {
			return nil, err
		}
		if err := authZOpts.ApplyTo(&serverConfig.Authorization); err != nil {
			return nil, err
		}
	}

	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		GetOpenAPIDefinitions, openapinamer.NewDefinitionNamer(scheme))
	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(
		GetOpenAPIDefinitions, openapinamer.NewDefinitionNamer(scheme))
	serverConfig.OpenAPIConfig.Info.Title = "cluster health analyzer"
	serverConfig.OpenAPIV3Config.Info.Title = "cluster health analyzer"
	serverConfig.EffectiveVersion = utilversion.DefaultKubeEffectiveVersion()

	// We will be serving out own `/metrics` endpoint.
	serverConfig.EnableMetrics = false

	return serverConfig, nil
}

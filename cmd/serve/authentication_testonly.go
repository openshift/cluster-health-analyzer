//go:build testonly

package serve

import (
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/spf13/pflag"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
)

var disableAuthForTesting bool

// addTestOnlyFlags registers the authentication bypass flag in test-only builds.
func addTestOnlyFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&disableAuthForTesting, "disable-auth-for-testing", false,
		"Disable authentication and authorization for local testing only")
}

// buildAuthenticationConfig disables delegated authentication and authorization
// only when the test-only bypass flag is enabled.
func buildAuthenticationConfig(o common.Options) (authenticationConfig, error) {
	if disableAuthForTesting {
		return authenticationConfig{
			delegatedAuthentication: operatorv1alpha1.DelegatedAuthentication{Disabled: true},
			delegatedAuthorization:  operatorv1alpha1.DelegatedAuthorization{Disabled: true},
		}, nil
	}

	return buildEnabledAuthenticationConfig(o)
}

//go:build !testonly

package serve

import (
	"github.com/spf13/pflag"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
)

// addTestOnlyFlags intentionally does not register authentication-bypass flags
// in production builds.
func addTestOnlyFlags(_ *pflag.FlagSet) {}

// buildAuthenticationConfig always enables delegated authentication and
// authorization in production builds.
func buildAuthenticationConfig(o common.Options) (authenticationConfig, error) {
	return buildEnabledAuthenticationConfig(o)
}

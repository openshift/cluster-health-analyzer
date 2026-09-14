//go:build testonly

package serve

import (
	"testing"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
)

// TestServeFlagsExposeDisableAuthForTestingInTestOnlyBuild verifies that
// test-only builds register and apply the authentication bypass flag.
func TestServeFlagsExposeDisableAuthForTestingInTestOnlyBuild(t *testing.T) {
	previousDisableAuthForTesting := disableAuthForTesting
	disableAuthForTesting = false
	t.Cleanup(func() { disableAuthForTesting = previousDisableAuthForTesting })

	flags := newServeCmd().Flags()

	if flag := flags.Lookup("disable-auth-for-testing"); flag == nil {
		t.Fatal("test-only authentication bypass must be available in a testonly build")
	}

	if err := flags.Parse([]string{"--disable-auth-for-testing"}); err != nil {
		t.Fatalf("failed to parse test-only authentication bypass flag: %v", err)
	}
	if !disableAuthForTesting {
		t.Fatal("test-only authentication bypass must be enabled after parsing the flag")
	}

	authenticationConfig, err := buildAuthenticationConfig(common.Options{})
	if err != nil {
		t.Fatalf("build authentication configuration: %v", err)
	}
	if !authenticationConfig.delegatedAuthentication.Disabled {
		t.Fatal("test-only bypass must disable delegated authentication")
	}
	if !authenticationConfig.delegatedAuthorization.Disabled {
		t.Fatal("test-only bypass must disable delegated authorization")
	}
}

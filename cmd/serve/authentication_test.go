//go:build !testonly

package serve

import "testing"

// TestServeFlagsDoNotExposeDisableAuthForTesting verifies that production
// builds do not register the authentication bypass flag.
func TestServeFlagsDoNotExposeDisableAuthForTesting(t *testing.T) {
	flags := newServeCmd().Flags()

	if flag := flags.Lookup("disable-auth-for-testing"); flag != nil {
		t.Fatalf("testing-only authentication bypass must not be exposed: %q", flag.Name)
	}
}

//go:build testonly

package mcp

import "github.com/spf13/pflag"

var disableAuthForTesting bool

// addTestOnlyFlags registers the authentication-bypass flag in test-only builds.
func addTestOnlyFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&disableAuthForTesting, "disable-auth-for-testing", false,
		"Disable token authentication for local testing only")
}

// disableAuthForTestingEnabled reports whether the test-only bypass is enabled.
func disableAuthForTestingEnabled() bool { return disableAuthForTesting }

//go:build !testonly

package mcp

import "github.com/spf13/pflag"

// addTestOnlyFlags intentionally does not register authentication-bypass flags
// in production builds.
func addTestOnlyFlags(_ *pflag.FlagSet) {}

// disableAuthForTestingEnabled always returns false in production builds.
func disableAuthForTestingEnabled() bool { return false }

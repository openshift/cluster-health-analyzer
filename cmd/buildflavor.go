//go:build !testonly

package cmd

// buildFlavor reports the compilation flavor of the binary. Production
// builds (make build) compile this file; dev builds (testonly tag, e.g.
// make run and make build-dev) compile buildflavor_testonly.go instead and
// additionally include the /debug/incident-state endpoint.
const buildFlavor = "production"

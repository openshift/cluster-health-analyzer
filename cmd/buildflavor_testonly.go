//go:build testonly

package cmd

// buildFlavor reports the compilation flavor of the binary. Dev builds
// (testonly tag: make run, make build-dev) include the
// --disable-auth-for-testing flag and the /debug/incident-state endpoint.
const buildFlavor = "dev"

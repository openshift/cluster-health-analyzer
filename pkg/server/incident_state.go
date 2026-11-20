//go:build !testonly

package server

import "github.com/openshift/cluster-health-analyzer/pkg/processor"

// registerIncidentStateEndpoint is a no-op outside dev builds: the
// /debug/incident-state endpoint must not ship in production binaries.
func registerIncidentStateEndpoint(_ Server, _ processor.MatcherStateSnapshotter) {}

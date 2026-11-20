//go:build testonly

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/openshift/cluster-health-analyzer/pkg/processor"
)

// registerIncidentStateEndpoint exposes the processor's in-memory matcher
// state as JSON on /debug/incident-state. It is compiled only into dev
// builds (make run, -tags testonly) for debugging the matcher lifecycle.
func registerIncidentStateEndpoint(server Server, p processor.MatcherStateSnapshotter) {
	server.Handle("/debug/incident-state", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(p.MatcherStateSnapshot()); err != nil {
			slog.Error("Failed to encode incident state", "err", err)
		}
	}))
}

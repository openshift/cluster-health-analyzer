//go:build testonly

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/stretchr/testify/require"
)

type stubServer struct {
	pattern  string
	handler  http.Handler
	snapshot processor.MatcherState
}

func (s *stubServer) Handle(pattern string, handler http.Handler) {
	s.pattern = pattern
	s.handler = handler
}

func (s *stubServer) Start(_ context.Context) error { return nil }

func (s *stubServer) MatcherStateSnapshot() processor.MatcherState { return s.snapshot }

func TestIncidentStateEndpointRegisteredInDevBuilds(t *testing.T) {
	srv := &stubServer{snapshot: processor.MatcherState{
		Groups: []processor.MatcherGroupState{{GroupID: "ID-1", RootGroupID: "ID-1"}},
	}}
	registerIncidentStateEndpoint(srv, srv)
	require.Equal(t, "/debug/incident-state", srv.pattern)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/incident-state", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got processor.MatcherState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "ID-1", got.Groups[0].GroupID)
}

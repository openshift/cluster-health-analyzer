package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// newFakeReviewer returns a TokenReviewer backed by a fake clientset that
// always rejects tokens.
func newFakeReviewer() *TokenReviewer {
	client := fake.NewClientset()
	client.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{
			Authenticated: false,
			Error:         "fake: always reject",
		}
		return true, review, nil
	})
	return NewTokenReviewer(client)
}

// TestHealthzBypassesAuth verifies that /healthz is reachable without
// authentication, while the MCP endpoint (catch-all) correctly rejects
// unauthenticated requests.
func TestHealthzBypassesAuth(t *testing.T) {
	cfg := MCPHealthServerCfg{
		Name:                  "test-mcp",
		Version:               "0.0.1-test",
		Url:                   ":0", // unused; we use httptest
		PrometheusURL:         "http://localhost:9090",
		AlertManagerURL:       "http://localhost:9093",
		DisableAuthForTesting: true, // skip in-cluster config
	}

	srv, err := NewMCPHealthServer(cfg)
	if err != nil {
		t.Fatalf("failed to create MCP server: %v", err)
	}

	// Attach a reviewer that always rejects tokens so we can verify
	// that healthz paths are truly exempt from auth.
	srv.tokenReviewer = newFakeReviewer()

	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	baseURL := ts.URL

	// /healthz must be reachable without a token.
	t.Run("unauthenticated /healthz", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /healthz failed: %v", err)
		}

		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /healthz: expected 200, got %d", resp.StatusCode)
		}
	})

	// The MCP catch-all must reject unauthenticated requests.
	t.Run("unauthenticated /mcp", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/mcp", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /mcp failed: %v", err)
		}

		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("POST /mcp: expected 401, got %d", resp.StatusCode)
		}
	})
}

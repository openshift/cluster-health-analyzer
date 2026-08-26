package mcp

import (
	"context"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestValidateToken_Authenticated(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{
			Authenticated: true,
			User: authenticationv1.UserInfo{
				Username: "test-user",
			},
		}
		return true, review, nil
	})

	reviewer := NewTokenReviewer(client)
	if err := reviewer.ValidateToken(context.Background(), "valid-token"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateToken_Unauthenticated(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{
			Authenticated: false,
			Error:         "token expired",
		}
		return true, review, nil
	})

	reviewer := NewTokenReviewer(client)
	if err := reviewer.ValidateToken(context.Background(), "expired-token"); err == nil {
		t.Fatal("expected error for unauthenticated token, got nil")
	}
}

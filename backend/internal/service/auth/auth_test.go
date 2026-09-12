// Copyright 2026 Contextual, Inc. https://agentrq.com

package auth

import (
	"context"
	"testing"
)

func TestGoogleAuthService(t *testing.T) {
	s := NewGoogle("client-id", "client-secret", "http://localhost/callback")

	t.Run("GetAuthURL", func(t *testing.T) {
		state := "some-state"
		url := s.GetAuthURL(state)
		if url == "" {
			t.Fatalf("expected auth URL, got empty")
		}
		if !contains(url, "client_id=client-id") {
			t.Errorf("URL missing client_id")
		}
		if !contains(url, "state="+state) {
			t.Errorf("URL missing state")
		}
		if !contains(url, "googleapis.com") {
			t.Errorf("URL should point to googleapis.com")
		}
	})

	t.Run("ExchangeError", func(t *testing.T) {
		ctx := context.Background()
		user, err := s.Exchange(ctx, "invalid-code")
		if err == nil {
			t.Error("expected error for invalid code, got nil")
		}
		if user != nil {
			t.Error("expected nil user for invalid code, got object")
		}
	})
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

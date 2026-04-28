package auth

import (
    "context"
    "testing"
)

func TestAuthService(t *testing.T) {
    clientID := "client-id"
    clientSecret := "client-secret"
    redirectURL := "http://localhost/callback"
    s := New(clientID, clientSecret, redirectURL)

    t.Run("GetAuthURL", func(t *testing.T) {
        state := "some-state"
        url := s.GetAuthURL(state)
        if url == "" {
            t.Fatalf("expected auth URL, got empty")
        }
        // contains expected substring
        if !contains(url, "client_id="+clientID) {
            t.Errorf("URL missing client_id")
        }
        if !contains(url, "state="+state) {
            t.Errorf("URL missing state")
        }
    })

    t.Run("ExchangeError", func(t *testing.T) {
        // This will fail because it'll try to reach Google, but at least we cover the error path
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

// Minimal helper to avoid depending on strings package in test if not needed (already there)
func contains(s, substr string) bool {
    for i := 0; i < len(s)-len(substr)+1; i++ {
        if s[i:i+len(substr)] == substr {
            return true
        }
    }
    return false
}

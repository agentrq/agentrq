package slack

import (
	"context"
	"testing"
	"fmt"

	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
)

func TestHandleOAuthCallback_CSRFProtection(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	tokenKey := "0123456789abcdef0123456789abcdef"
	c := &controller{
		tokenKey: tokenKey,
		repo: mockRepo,
	}

	workspaceID62 := "test-workspace"
	validSignature := security.Sign(workspaceID62, tokenKey)
	validState := workspaceID62 + "." + validSignature

	t.Run("ValidSignature", func(t *testing.T) {
		// Valid signature reaches the repo call
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), gomock.Any()).Return(model.Workspace{}, fmt.Errorf("next step")).AnyTimes()
		err := c.HandleOAuthCallback(context.Background(), validState, "code", "redirect")
		if err != nil && err.Error() == "slack: invalid state signature (CSRF suspected)" {
			t.Errorf("expected signature to be valid, but got CSRF error")
		}
	})

	t.Run("InvalidSignature", func(t *testing.T) {
		invalidState := workspaceID62 + ".invalid-sig"
		err := c.HandleOAuthCallback(context.Background(), invalidState, "code", "redirect")
		if err == nil || err.Error() != "slack: invalid state signature (CSRF suspected)" {
			t.Errorf("expected CSRF error for invalid signature, got %v", err)
		}
	})

	t.Run("MissingSignature", func(t *testing.T) {
		invalidState := workspaceID62
		err := c.HandleOAuthCallback(context.Background(), invalidState, "code", "redirect")
		if err == nil || err.Error() != "slack: invalid state parameter format" {
			t.Errorf("expected format error for missing signature, got %v", err)
		}
	})

	t.Run("TamperedMessage", func(t *testing.T) {
		tamperedState := "other-workspace." + validSignature
		err := c.HandleOAuthCallback(context.Background(), tamperedState, "code", "redirect")
		if err == nil || err.Error() != "slack: invalid state signature (CSRF suspected)" {
			t.Errorf("expected CSRF error for tampered message, got %v", err)
		}
	})
}

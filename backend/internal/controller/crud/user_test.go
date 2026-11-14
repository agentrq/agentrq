// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"fmt"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	mock_avatar "github.com/agentrq/agentrq/backend/internal/service/mocks/avatar"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

func TestFindOrCreateUser_FoundByEmail_NoUpdate(t *testing.T) {
	e := newTestController(t)

	cached := model.User{
		ID:        testUserID,
		Email:     "test@example.com",
		Name:      "Test User",
		Picture:   "http://pic",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	e.repo.EXPECT().FindUserByEmail(gomock.Any(), "test@example.com").Return(cached, nil)

	resp, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{
		Email:   "test@example.com",
		Name:    "Test User",
		Picture: "http://pic",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.User.ID != testUserID {
		t.Errorf("expected ID %d, got %d", testUserID, resp.User.ID)
	}
}

func TestFindOrCreateUser_FoundByEmail_UpdateNeeded(t *testing.T) {
	e := newTestController(t)

	cached := model.User{
		ID:        testUserID,
		Email:     "test@example.com",
		Name:      "",
		Picture:   "",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	e.repo.EXPECT().FindUserByEmail(gomock.Any(), "test@example.com").Return(cached, nil)
	e.repo.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, u model.User) (model.User, error) {
		if u.Name != "New Name" || u.Picture != "http://new" {
			return model.User{}, fmt.Errorf("unexpected update values")
		}
		return u, nil
	})

	resp, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{
		Email:   "test@example.com",
		Name:    "New Name",
		Picture: "http://new",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.User.Name != "New Name" {
		t.Errorf("expected Name 'New Name', got %s", resp.User.Name)
	}
}

func TestFindOrCreateUser_NotFound_CreateNew(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().FindUserByEmail(gomock.Any(), "new@example.com").Return(model.User{}, base.ErrNotFound)
	e.idgen.EXPECT().NextID().Return(int64(999))
	e.repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, u model.User) (model.User, error) {
		if u.ID != 999 || u.Email != "new@example.com" {
			return model.User{}, fmt.Errorf("unexpected creation values")
		}
		return u, nil
	})
	e.idgen.EXPECT().NextID().Return(int64(1000))
	e.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, w model.Workspace) (model.Workspace, error) {
		if w.ID != 1000 || w.UserID != 999 || w.Name != "supervisor" {
			return model.Workspace{}, fmt.Errorf("unexpected supervisor workspace values")
		}
		if w.Description != defaultSupervisorWorkspaceDescription {
			return model.Workspace{}, fmt.Errorf("supervisor workspace description should be the default mission text")
		}
		if w.SelfLearningLoopNote != defaultSupervisorWorkspaceSelfLearningLoopNote {
			return model.Workspace{}, fmt.Errorf("supervisor workspace self-learning-loop note should be the default")
		}
		if w.InputSendDelaySeconds != 5 {
			return model.Workspace{}, fmt.Errorf("supervisor workspace send delay should default to 5s, got %d", w.InputSendDelaySeconds)
		}
		if !w.ClearContextDefault {
			return model.Workspace{}, fmt.Errorf("supervisor workspace should clear context by default")
		}
		return w, nil
	})

	resp, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{
		Email: "new@example.com",
		Name:  "New Person",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.User.ID != 999 {
		t.Errorf("expected ID 999, got %d", resp.User.ID)
	}
}

func TestFindOrCreateUser_NotFound_SupervisorWorkspaceError(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().FindUserByEmail(gomock.Any(), "new@example.com").Return(model.User{}, base.ErrNotFound)
	e.idgen.EXPECT().NextID().Return(int64(999))
	e.repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).Return(model.User{ID: 999, Email: "new@example.com"}, nil)
	e.idgen.EXPECT().NextID().Return(int64(1000))
	e.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).Return(model.Workspace{}, fmt.Errorf("database unavailable"))

	_, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{
		Email: "new@example.com",
		Name:  "New Person",
	})
	if err == nil {
		t.Fatal("expected error when the supervisor workspace fails to create")
	}
}

func TestFindOrCreateUser_RepoError(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().FindUserByEmail(gomock.Any(), gomock.Any()).Return(model.User{}, fmt.Errorf("database unavailable"))

	_, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{Email: "e@e.com"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFindOrCreateUser_EmptyEmail(t *testing.T) {
	e := newTestController(t)

	resp, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{Email: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response for empty email")
	}
}

func TestEnsureSupervisorWorkspace_ReturnsTheExistingOne(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().ListWorkspaces(gomock.Any(), int64(999), false).Return([]model.Workspace{
		{ID: 1, UserID: 999, Name: "website"},
		{ID: 2, UserID: 999, Name: "supervisor"},
	}, nil)

	resp, err := e.controller.EnsureSupervisorWorkspace(context.Background(), entity.EnsureSupervisorWorkspaceRequest{UserID: monoflake.ID(999).String()})
	if err != nil {
		t.Fatalf("EnsureSupervisorWorkspace failed: %v", err)
	}
	if resp.Workspace.ID != 2 {
		t.Errorf("EnsureSupervisorWorkspace returned workspace %d, want the existing supervisor 2", resp.Workspace.ID)
	}
}

func TestEnsureSupervisorWorkspace_MakesOneWithSignupDefaultsWhenThereIsNone(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().ListWorkspaces(gomock.Any(), int64(999), false).Return([]model.Workspace{{ID: 1, UserID: 999, Name: "website"}}, nil)
	e.idgen.EXPECT().NextID().Return(int64(1000))
	e.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, w model.Workspace) (model.Workspace, error) {
		if w.ID != 1000 || w.UserID != 999 || w.Name != "supervisor" || w.Description != defaultSupervisorWorkspaceDescription ||
			w.SelfLearningLoopNote != defaultSupervisorWorkspaceSelfLearningLoopNote || w.InputSendDelaySeconds != defaultInputSendDelaySeconds ||
			!w.ClearContextDefault {
			return model.Workspace{}, fmt.Errorf("the supervisor was made with %+v, want signup's defaults", w)
		}
		return w, nil
	})

	resp, err := e.controller.EnsureSupervisorWorkspace(context.Background(), entity.EnsureSupervisorWorkspaceRequest{UserID: monoflake.ID(999).String()})
	if err != nil {
		t.Fatalf("EnsureSupervisorWorkspace failed: %v", err)
	}
	if resp.Workspace.ID != 1000 || resp.Workspace.Name != "supervisor" {
		t.Errorf("EnsureSupervisorWorkspace returned %d %q, want the new supervisor 1000", resp.Workspace.ID, resp.Workspace.Name)
	}
}

func TestEnsureSupervisorWorkspace_FailsWhenTheWorkspacesCannotBeListed(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().ListWorkspaces(gomock.Any(), int64(999), false).Return(nil, fmt.Errorf("database unavailable"))

	if _, err := e.controller.EnsureSupervisorWorkspace(context.Background(), entity.EnsureSupervisorWorkspaceRequest{UserID: monoflake.ID(999).String()}); err == nil {
		t.Fatal("EnsureSupervisorWorkspace returned no error, want the list failure")
	}
}

func TestEnsureSupervisorWorkspace_FailsWhenTheSupervisorCannotBeMade(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().ListWorkspaces(gomock.Any(), int64(999), false).Return(nil, nil)
	e.idgen.EXPECT().NextID().Return(int64(1000))
	e.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).Return(model.Workspace{}, fmt.Errorf("database unavailable"))

	if _, err := e.controller.EnsureSupervisorWorkspace(context.Background(), entity.EnsureSupervisorWorkspaceRequest{UserID: monoflake.ID(999).String()}); err == nil {
		t.Fatal("EnsureSupervisorWorkspace returned no error, want the create failure")
	}
}

func newAvatarTestController(t *testing.T) (*testEnv, *mock_avatar.MockService) {
	t.Helper()
	e := newTestController(t)
	avatars := mock_avatar.NewMockService(gomock.NewController(t))
	e.controller.(*controller).avatars = avatars
	return e, avatars
}

func TestFindOrCreateUser_Avatars(t *testing.T) {
	const kept = "https://cdn.example/avatars/u-1/abc.png"
	req := entity.FindOrCreateUserRequest{Email: "test@example.com", Name: "Test User", Picture: "https://provider/pic"}

	t.Run("an existing user's picture becomes the kept copy", func(t *testing.T) {
		e, avatars := newAvatarTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), req.Email).Return(model.User{ID: testUserID, Email: req.Email, Name: req.Name, Picture: req.Picture}, nil)
		avatars.EXPECT().Mirror(gomock.Any(), testUserID, req.Picture, req.Picture).Return(kept, nil)
		e.repo.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u model.User) (model.User, error) {
			return u, nil
		})

		resp, err := e.controller.FindOrCreateUser(context.Background(), req)
		if err != nil || resp.User.Picture != kept {
			t.Fatalf("FindOrCreateUser returned %+v, %v, want the kept copy as the picture", resp, err)
		}
	})

	t.Run("a picture already kept is not saved again", func(t *testing.T) {
		e, avatars := newAvatarTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), req.Email).Return(model.User{ID: testUserID, Email: req.Email, Name: req.Name, Picture: kept}, nil)
		avatars.EXPECT().Mirror(gomock.Any(), testUserID, req.Picture, kept).Return(kept, nil)

		resp, err := e.controller.FindOrCreateUser(context.Background(), req)
		if err != nil || resp.User.Picture != kept {
			t.Fatalf("FindOrCreateUser returned %+v, %v, want the kept copy as the picture", resp, err)
		}
	})

	t.Run("a copy that fails keeps the picture the user has", func(t *testing.T) {
		e, avatars := newAvatarTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), req.Email).Return(model.User{ID: testUserID, Email: req.Email, Name: req.Name, Picture: kept}, nil)
		avatars.EXPECT().Mirror(gomock.Any(), testUserID, req.Picture, kept).Return("", fmt.Errorf("provider down"))

		resp, err := e.controller.FindOrCreateUser(context.Background(), req)
		if err != nil || resp.User.Picture != kept {
			t.Fatalf("FindOrCreateUser returned %+v, %v, want the picture the user had", resp, err)
		}
	})

	t.Run("a copy that fails for a user with no picture keeps the provider's", func(t *testing.T) {
		e, avatars := newAvatarTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), req.Email).Return(model.User{ID: testUserID, Email: req.Email, Name: req.Name}, nil)
		avatars.EXPECT().Mirror(gomock.Any(), testUserID, req.Picture, "").Return("", fmt.Errorf("provider down"))
		e.repo.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u model.User) (model.User, error) {
			return u, nil
		})

		resp, err := e.controller.FindOrCreateUser(context.Background(), req)
		if err != nil || resp.User.Picture != req.Picture {
			t.Fatalf("FindOrCreateUser returned %+v, %v, want the provider's picture", resp, err)
		}
	})

	t.Run("a new user is created with the kept copy", func(t *testing.T) {
		e, avatars := newAvatarTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), req.Email).Return(model.User{}, base.ErrNotFound)
		e.idgen.EXPECT().NextID().Return(testUserID)
		avatars.EXPECT().Mirror(gomock.Any(), testUserID, req.Picture, "").Return(kept, nil)
		e.repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u model.User) (model.User, error) {
			return u, nil
		})
		e.idgen.EXPECT().NextID().Return(int64(99))
		e.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, w model.Workspace) (model.Workspace, error) {
			return w, nil
		})

		resp, err := e.controller.FindOrCreateUser(context.Background(), req)
		if err != nil || resp.User.Picture != kept {
			t.Fatalf("FindOrCreateUser returned %+v, %v, want the kept copy as the picture", resp, err)
		}
	})

	t.Run("a new user whose copy fails is created with the provider's picture", func(t *testing.T) {
		e, avatars := newAvatarTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), req.Email).Return(model.User{}, base.ErrNotFound)
		e.idgen.EXPECT().NextID().Return(testUserID)
		avatars.EXPECT().Mirror(gomock.Any(), testUserID, req.Picture, "").Return("", fmt.Errorf("provider down"))
		e.repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u model.User) (model.User, error) {
			return u, nil
		})
		e.idgen.EXPECT().NextID().Return(int64(99))
		e.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, w model.Workspace) (model.Workspace, error) {
			return w, nil
		})

		resp, err := e.controller.FindOrCreateUser(context.Background(), req)
		if err != nil || resp.User.Picture != req.Picture {
			t.Fatalf("FindOrCreateUser returned %+v, %v, want the provider's picture", resp, err)
		}
	})

	t.Run("a sign-in with no picture copies nothing", func(t *testing.T) {
		e, _ := newAvatarTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), req.Email).Return(model.User{ID: testUserID, Email: req.Email, Name: req.Name}, nil)

		if _, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{Email: req.Email, Name: req.Name}); err != nil {
			t.Fatalf("FindOrCreateUser returned %v", err)
		}
	})
}

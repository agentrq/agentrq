package crud

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	"github.com/hasmcp/agentrq/backend/internal/repository/base"
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

func TestFindOrCreateUser_RepoError(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().FindUserByEmail(gomock.Any(), gomock.Any()).Return(model.User{}, fmt.Errorf("db error"))

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

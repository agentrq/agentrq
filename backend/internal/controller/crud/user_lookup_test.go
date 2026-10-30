// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

// A stored user with no email takes the one it was found by.
func TestFindOrCreateUser_FillsAMissingEmail(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().FindUserByEmail(gomock.Any(), "a@example.com").Return(model.User{ID: testUserID, Name: "A", Picture: "p"}, nil)
	var saved model.User
	e.repo.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u model.User) (model.User, error) { saved = u; return u, nil })
	resp, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if saved.Email != "a@example.com" || resp.User.Email != "a@example.com" {
		t.Errorf("saved %q, answered %q; want the email", saved.Email, resp.User.Email)
	}
}

func TestFindOrCreateUser_PassesOnWriteFailures(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), "a@example.com").Return(model.User{ID: testUserID, Email: "a@example.com"}, nil)
		e.repo.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).Return(model.User{}, errDBDown)
		_, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{Email: "a@example.com", Name: "A"})
		wantErrIs(t, "FindOrCreateUser", err, errDBDown)
	})
	t.Run("create", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().FindUserByEmail(gomock.Any(), "a@example.com").Return(model.User{}, base.ErrNotFound)
		e.idgen.EXPECT().NextID().Return(int64(7))
		e.repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).Return(model.User{}, errDBDown)
		_, err := e.controller.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{Email: "a@example.com"})
		wantErrIs(t, "FindOrCreateUser", err, errDBDown)
	})
}

func TestCreateUser(t *testing.T) {
	e := newTestController(t)
	e.idgen.EXPECT().NextID().Return(int64(7))
	var saved model.User
	e.repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u model.User) (model.User, error) { saved = u; return u, nil })
	got, err := e.controller.CreateUser(context.Background(), entity.User{Email: "a@example.com", Name: "A", Picture: "p"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if saved.ID != 7 || saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Errorf("saved %+v, want id 7 and both timestamps", saved)
	}
	if got.ID != 7 || got.Email != "a@example.com" || got.Name != "A" || got.Picture != "p" || !got.CreatedAt.Equal(saved.CreatedAt) {
		t.Errorf("CreateUser = %+v", got)
	}

	e = newTestController(t)
	e.idgen.EXPECT().NextID().Return(int64(8))
	e.repo.EXPECT().CreateUser(gomock.Any(), gomock.Any()).Return(model.User{}, errDBDown)
	_, err = e.controller.CreateUser(context.Background(), entity.User{Email: "b@example.com"})
	wantErrIs(t, "CreateUser", err, errDBDown)
}

func TestFindUserByIDAndEmail(t *testing.T) {
	now := time.Now()
	stored := model.User{ID: testUserID, CreatedAt: now, UpdatedAt: now, Email: "a@example.com", Name: "A", Picture: "p"}
	want := entity.User{ID: testUserID, CreatedAt: now, UpdatedAt: now, Email: "a@example.com", Name: "A", Picture: "p"}

	e := newTestController(t)
	e.repo.EXPECT().SystemGetUser(gomock.Any(), testUserID).Return(stored, nil)
	if got, err := e.controller.FindUserByID(context.Background(), testUserID); err != nil || got != want {
		t.Errorf("FindUserByID = %+v, %v; want %+v", got, err, want)
	}
	e.repo.EXPECT().SystemGetUser(gomock.Any(), int64(9)).Return(model.User{}, base.ErrNotFound)
	_, err := e.controller.FindUserByID(context.Background(), 9)
	wantErrIs(t, "FindUserByID", err, base.ErrNotFound)

	e.repo.EXPECT().FindUserByEmail(gomock.Any(), "a@example.com").Return(stored, nil)
	if got, err := e.controller.FindUserByEmail(context.Background(), "a@example.com"); err != nil || got != want {
		t.Errorf("FindUserByEmail = %+v, %v; want %+v", got, err, want)
	}
	e.repo.EXPECT().FindUserByEmail(gomock.Any(), "x@example.com").Return(model.User{}, base.ErrNotFound)
	_, err = e.controller.FindUserByEmail(context.Background(), "x@example.com")
	wantErrIs(t, "FindUserByEmail", err, base.ErrNotFound)
}

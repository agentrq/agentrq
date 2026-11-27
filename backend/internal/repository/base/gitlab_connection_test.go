// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestGitLabConnections(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.GitLabConnection{}); err != nil {
		t.Fatal(err)
	}
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	exp := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)

	if _, err := repo.GetGitLabConnection(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("none yet: %v", err)
	}
	if err := repo.SaveGitLabConnection(ctx, model.GitLabConnection{ID: 1, UserID: 1, Host: "gitlab.com", Username: "ann", AccessToken: "a1", RefreshToken: "r1", ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveGitLabConnection(ctx, model.GitLabConnection{ID: 2, UserID: 2, Host: "gitlab.com", Username: "bob", RefreshToken: "rb"}); err != nil {
		t.Fatal(err)
	}
	// A reconnect replaces the account's connection, keeping its row.
	if err := repo.SaveGitLabConnection(ctx, model.GitLabConnection{ID: 3, UserID: 1, Host: "git.example", Username: "ann2", AccessToken: "a2", AccessNonce: "n", RefreshToken: "r2", RefreshNonce: "m", ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetGitLabConnection(ctx, 1)
	if err != nil || got.ID != 1 || got.Host != "git.example" || got.Username != "ann2" || got.AccessToken != "a2" || got.RefreshToken != "r2" || got.AccessNonce != "n" || got.RefreshNonce != "m" || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("after a reconnect: %+v, %v", got, err)
	}

	// A renewal is stored only over the refresh token it was renewed from.
	renewed := model.GitLabConnection{UserID: 1, AccessToken: "a3", RefreshToken: "r3", ExpiresAt: exp.Add(time.Hour)}
	if ok, err := repo.RenewGitLabConnection(ctx, renewed, "r1"); err != nil || ok {
		t.Fatalf("a stale renewal must not be stored: %v, %v", ok, err)
	}
	if ok, err := repo.RenewGitLabConnection(ctx, renewed, "r2"); err != nil || !ok {
		t.Fatalf("renewal: %v, %v", ok, err)
	}
	if got, _ := repo.GetGitLabConnection(ctx, 1); got.AccessToken != "a3" || got.RefreshToken != "r3" || got.Username != "ann2" {
		t.Fatalf("after a renewal: %+v", got)
	}

	if err := repo.DeleteGitLabConnection(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteGitLabConnection(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a second delete: %v", err)
	}
	if got, err := repo.GetGitLabConnection(ctx, 2); err != nil || got.Username != "bob" {
		t.Fatalf("another account's connection: %+v, %v", got, err)
	}

	// A database that fails says so.
	sqlDB, _ := db.DB()
	sqlDB.Close()
	if _, err := repo.GetGitLabConnection(ctx, 2); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("get on a closed database: %v", err)
	}
	if err := repo.DeleteGitLabConnection(ctx, 2); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("delete on a closed database: %v", err)
	}
}
